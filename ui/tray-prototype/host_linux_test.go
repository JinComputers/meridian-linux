//go:build linux

package main

import (
	"errors"
	"os/exec"
	"testing"
)

func TestClassifyKeyLookup(t *testing.T) {
	for _, tc := range []struct {
		script  string
		want    string
		wantErr error
	}{
		{"printf 'synthetic-key\\n'", "synthetic-key", nil},
		{"exit 1", "", errKeyAbsent},
		{"printf 'private error' >&2; exit 1", "", errKeyStoreUnavailable},
		{"exit 2", "", errKeyStoreUnavailable},
	} {
		out, err := exec.Command("sh", "-c", tc.script).Output()
		got, actualErr := classifyKeyLookup(out, err)
		if got != tc.want || !errors.Is(actualErr, tc.wantErr) {
			t.Fatalf("got %q, %v", got, actualErr)
		}
	}
	if _, err := classifyKeyLookup(nil, errors.New("tool missing")); !errors.Is(err, errKeyStoreUnavailable) {
		t.Fatal("missing executable must be a storage error")
	}
}

func TestNetworkRecoveryRequiresExplicitConsent(t *testing.T) {
	cancel := exec.Command("sh", "-c", "exit 1").Run()
	tests := []struct {
		name                     string
		promptErr, privilegedErr error
		want                     bool
		wantErr                  bool
		wantCalls                int
	}{
		{"cancelled", cancel, nil, false, false, 1},
		{"missing prompt", errors.New("unavailable"), nil, false, true, 1},
		{"authorization failure", nil, errors.New("denied"), false, true, 2},
		{"confirmed and restored", nil, nil, true, false, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			run := func(name string, args ...string) error {
				calls = append(calls, name)
				if name == "zenity" {
					return tc.promptErr
				}
				if name == "pkexec" && len(args) == 1 && args[0] == "/usr/libexec/meridian/meridian-recover" {
					return tc.privilegedErr
				}
				return errors.New("unexpected command")
			}
			got, err := promptAndRecoverNetwork(run)
			if got != tc.want || (err != nil) != tc.wantErr || len(calls) != tc.wantCalls {
				t.Fatalf("got %v %v %v", got, err, calls)
			}
		})
	}
}
