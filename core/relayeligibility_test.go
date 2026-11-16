package core

import "testing"

func TestRelayEligibilityBody(t *testing.T) {
	tests := []struct {
		name              string
		b                 map[string]interface{}
		eligible, premium bool
	}{
		{"paid premium", map[string]interface{}{"verdict": "ok", "trial": false}, true, true},
		{"paid basic", map[string]interface{}{"verdict": "ok", "trial": false, "tier": "basic"}, true, false},
		{"trial", map[string]interface{}{"verdict": "ok", "trial": true}, true, true},
		{"trial with basic tier", map[string]interface{}{"verdict": "ok", "trial": true, "tier": "basic"}, true, true},
		{"expired trial", map[string]interface{}{"verdict": "expired", "trial": true}, false, false},
		{"missing trial", map[string]interface{}{"verdict": "ok"}, false, false},
		{"denied", map[string]interface{}{"verdict": "deactivated", "trial": false}, false, false},
		{"unknown tier", map[string]interface{}{"verdict": "ok", "trial": false, "tier": "unknown"}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, b := relayEligibilityBody(tc.b)
			if a != tc.eligible || b != tc.premium {
				t.Fatalf("got %v %v", a, b)
			}
		})
	}
}
