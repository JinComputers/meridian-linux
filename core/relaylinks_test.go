package core

import (
	"meridian/engine/params"
	"testing"
)

func TestRelayLinksCacheIsolatesKeyAndDeviceAndClearsOnEmpty(t *testing.T) {
	c := &ParamsCache{Dir: t.TempDir()}
	links := &params.RelayLinks{VK: []string{"hash1"}, Dion: []string{"room1"}}
	saveRelayLinks(c, "key-one", "device-one", links)
	got := cachedRelayLinks(c, "key-one", "device-one")
	if got == nil || len(got.VK) != 1 {
		t.Fatal("saved links missing")
	}
	if cachedRelayLinks(c, "key-two", "device-one") != nil || cachedRelayLinks(c, "key-one", "device-two") != nil {
		t.Fatal("cache shared across key or device")
	}
	saveRelayLinks(c, "key-one", "device-one", &params.RelayLinks{VK: []string{}, Dion: []string{}})
	got = cachedRelayLinks(c, "key-one", "device-one")
	if got == nil || len(got.VK) != 0 || len(got.Dion) != 0 {
		t.Fatal("successful empty pool not saved")
	}
}

func TestAutoLinksPreserveManual(t *testing.T) {
	got := addUniqueStrings([]string{"manual", "shared"}, []string{"shared", "auto"})
	if len(got) != 3 || got[0] != "manual" || got[1] != "shared" || got[2] != "auto" {
		t.Fatal("unexpected room merge")
	}
}

func TestCachedScopeRejectsUnknownAndPartnerDowngrade(t *testing.T) {
	for _, tc := range []struct {
		scope, refusal string
		want           bool
	}{
		{"", "", false}, {"ordinary", "", true}, {"partner", "", true},
		{"ordinary", "partner", false}, {"partner", "partner", true},
	} {
		err := &ParamsError{Kind: params.KindRefused, Code: 503, RoutingScope: tc.refusal}
		if got := cachedScopeAllowed(&ParamsAnswer{RoutingScope: tc.scope}, err); got != tc.want {
			t.Fatalf("scope=%q refusal=%q got %v", tc.scope, tc.refusal, got)
		}
	}
}

func TestTrialPoolRequiresSuccessfulDeviceRequest(t *testing.T) {
	c := &ParamsCache{Dir: t.TempDir()}
	saveRelayLinks(c, "key", "device", &params.RelayLinks{VK: []string{"old"}})
	if relayPoolForSession(startupRelayResult{eligible: true, trial: true}, c, "key", "device") != nil {
		t.Fatal("trial must not reuse cache after failed request")
	}
	if relayPoolForSession(startupRelayResult{eligible: false, trial: true, fetched: true, links: &params.RelayLinks{VK: []string{"new"}}}, c, "key", "device") != nil {
		t.Fatal("expired trial must not use links")
	}
	links := &params.RelayLinks{VK: []string{"new"}, Dion: []string{"room"}}
	if got := relayPoolForSession(startupRelayResult{eligible: true, trial: true, fetched: true, links: links}, c, "key", "device"); got == nil || len(got.VK) != 1 || len(got.Dion) != 1 {
		t.Fatal("valid trial pool missing")
	}
	if got := relayPoolForSession(startupRelayResult{eligible: true}, c, "key", "other-device"); got != nil {
		t.Fatal("foreign device used cache")
	}
	if got := relayPoolForSession(startupRelayResult{eligible: true}, c, "key", "device"); got == nil {
		t.Fatal("paid cache fallback changed")
	}
}

func TestRefusedRelayPoolNeverUsesPaidCache(t *testing.T) {
	c := &ParamsCache{Dir: t.TempDir()}
	saveRelayLinks(c, "key", "device", &params.RelayLinks{VK: []string{"old"}})
	if got := relayPoolForSession(startupRelayResult{eligible: true, refused: true}, c, "key", "device"); got != nil {
		t.Fatal("server refusal must not use cached rooms")
	}
}
