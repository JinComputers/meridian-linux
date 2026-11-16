package main

import (
	"bytes"
	"errors"
	"github.com/JinComputers/meridian-desktop/core"
	"log"
	"meridian/engine/params"
	"strings"
	"testing"
)

func TestKeychainStatusDoesNotExposeErrorText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{{errors.New("secret OSStatus -25244 more"), "-25244"}, {errors.New("secret"), "unknown"}} {
		if got := keychainErrorCode(tc.err); got != tc.want {
			t.Fatalf("got %q", got)
		}
	}
}
func TestRecoveryKeyStaysInMemory(t *testing.T) {
	a := &app{}
	a.setRecoveryKey("synthetic-key")
	k, err := a.currentSubscriberKey()
	if err != nil || k != "synthetic-key" {
		t.Fatal("temporary key not available")
	}
}
func TestRecoveryRequiresActiveConfirmedUnblockedKey(t *testing.T) {
	active := &params.KeyAnswer{State: params.KeyActive}
	if !canUseTemporaryKey(active, nil, nil) {
		t.Fatal("active key refused")
	}
	if canUseTemporaryKey(active, nil, errors.New("network")) || canUseTemporaryKey(nil, nil, nil) || canUseTemporaryKey(active, &core.PartnerUsage{BlockedReason: "expired"}, nil) {
		t.Fatal("unconfirmed or blocked key accepted")
	}
	other := &params.KeyAnswer{State: params.KeyActive, Bound: "other"}
	if canUseTemporaryKey(other, nil, nil) {
		t.Fatal("other device accepted")
	}
	noVerdict := &params.KeyAnswer{State: params.KeyActive, NoVerdict: true}
	if canUseTemporaryKey(noVerdict, nil, nil) {
		t.Fatal("unconfirmed accepted")
	}
}

func TestRecoveredKeyNeedsSuccessfulSaveAndMatchingRead(t *testing.T) {
	value := ""
	save := func(s string) error { value = s; return nil }
	load := func() (string, error) { return value, nil }
	if !persistRecoveredKey("test-only", save, load) {
		t.Fatal("successful write/read rejected")
	}
	if persistRecoveredKey("test-only", func(string) error { return errors.New("denied") }, load) {
		t.Fatal("failed save reported success")
	}
	if persistRecoveredKey("test-only", save, func() (string, error) { return "different", nil }) {
		t.Fatal("mismatched read reported success")
	}
	if persistRecoveredKey("test-only", save, func() (string, error) { return "", errors.New("denied") }) {
		t.Fatal("failed read reported success")
	}
}

func TestRecoveryDiagnosticsDoNotExposeSecrets(t *testing.T) {
	var out bytes.Buffer
	old := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(old)
	secret := "synthetic-private-value"
	persistRecoveredKey(secret, func(string) error {
		return errors.New(secret + " OSStatus -25293: SecItemAdd record=recovery")
	}, func() (string, error) { t.Fatal("must not read after failed write"); return "", nil })
	got := out.String()
	if strings.Contains(got, secret) || !strings.Contains(got, "-25293") || !strings.Contains(got, "SecItemAdd") || !strings.Contains(got, "recovery") {
		t.Fatalf("unexpected diagnostic: %q", got)
	}
	out.Reset()
	persistRecoveredKey(secret, func(string) error { return nil }, func() (string, error) {
		return "", errors.New(secret + " OSStatus -25308: SecItemCopyMatching record=recovery")
	})
	got = out.String()
	if strings.Contains(got, secret) || !strings.Contains(got, "-25308") || !strings.Contains(got, "проверка чтением") {
		t.Fatalf("unexpected read diagnostic: %q", got)
	}
}

func TestKeychainDefaultStatusIsAllowlisted(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errors.New("OSStatus -25293 default-keychain=locked"), "locked"},
		{errors.New("OSStatus -25293 default-keychain=unlocked"), "unlocked"},
		{errors.New("OSStatus -25293 default-keychain=unavailable"), "unavailable"},
		{errors.New("OSStatus -25293 default-keychain=secret"), "unknown"},
		{errors.New("OSStatus -25308 default-keychain=locked"), "unknown"},
		{nil, "unknown"},
	} {
		if got := keychainDefaultStatus(tc.err); got != tc.want {
			t.Errorf("status = %s; want %s", got, tc.want)
		}
	}
}
