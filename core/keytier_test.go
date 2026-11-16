package core

import (
	"testing"

	"meridian/engine/params"
)

func TestKeyTierOf(t *testing.T) {
	for name, c := range map[string]struct {
		ans  *params.KeyAnswer
		tier KeyTier
		ok   bool
	}{
		"базовый":        {&params.KeyAnswer{State: params.KeyActive, Tier: params.TierBasic}, TierBasic, true},
		"премиум":        {&params.KeyAnswer{State: params.KeyActive}, TierPremium, true},
		"служба молчала": {&params.KeyAnswer{NoVerdict: true}, TierPremium, false},
		"ключ истёк":     {&params.KeyAnswer{State: params.KeyExpired, Tier: params.TierBasic}, TierPremium, false},
		"ответа нет":     {nil, TierPremium, false},
	} {
		tier, ok := KeyTierOf(c.ans)
		if tier != c.tier || ok != c.ok {
			t.Errorf("%s: (%q, %v), want (%q, %v)", name, tier, ok, c.tier, c.ok)
		}
	}
	if !TierBasic.Basic() || TierPremium.Basic() {
		t.Error("Basic() перепутан")
	}
}
