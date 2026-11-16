package params

import "testing"

func TestPartnerKeyMetadata(t *testing.T) {
	cases := []struct {
		name        string
		body        map[string]interface{}
		managed     bool
		blocked     string
		limit, used int64
	}{
		{"ordinary", map[string]interface{}{"verdict": "ok"}, false, "", 0, 0},
		{"limited", map[string]interface{}{"verdict": "ok", "partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(10e9), "traffic_used_bytes": float64(4e9), "traffic_remaining_bytes": float64(6e9)}}, true, "", 10e9, 4e9},
		{"unlimited", map[string]interface{}{"verdict": "ok", "partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(0)}}, true, "", 0, 0},
		{"exhausted", map[string]interface{}{"verdict": "ok", "partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(10e9), "traffic_used_bytes": float64(10e9), "blocked_reason": "traffic_limit"}}, true, "traffic_limit", 10e9, 10e9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := parseKey(c.body)
			if (a.Partner != nil) != c.managed {
				t.Fatalf("managed = %v", a.Partner != nil)
			}
			if c.managed && (a.Partner.BlockedReason != c.blocked || a.Partner.TrafficLimitBytes != c.limit || a.Partner.TrafficUsedBytes != c.used) {
				t.Fatalf("metadata mismatch: %+v", a.Partner)
			}
		})
	}
}
