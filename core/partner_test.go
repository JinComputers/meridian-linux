package core

import "testing"

func TestParsePartnerUsage(t *testing.T) {
	cases := []struct {
		name   string
		body   map[string]interface{}
		want   bool
		limit  int64
		reason string
	}{
		{"ordinary", map[string]interface{}{"verdict": "ok"}, false, 0, ""},
		{"partner", map[string]interface{}{"partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(100), "traffic_used_bytes": float64(40), "traffic_remaining_bytes": float64(60)}}, true, 100, ""},
		{"unlimited", map[string]interface{}{"partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(0), "traffic_used_bytes": float64(40), "traffic_remaining_bytes": nil}}, true, 0, ""},
		{"exhausted", map[string]interface{}{"partner": map[string]interface{}{"managed": true, "traffic_limit_bytes": float64(100), "traffic_used_bytes": float64(100), "traffic_remaining_bytes": float64(0), "blocked_reason": "traffic_limit"}}, true, 100, "traffic_limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := ParsePartnerUsage(c.body)
			if (p != nil) != c.want {
				t.Fatalf("partner=%v", p)
			}
			if p != nil && (p.Limit != c.limit || p.BlockedReason != c.reason) {
				t.Fatalf("partner=%+v", p)
			}
		})
	}
}
