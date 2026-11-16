package core

import (
	"context"
	"math"
	"meridian/engine/params"
	"strings"
)

// PartnerUsage is optional metadata from the trusted /v1/key response.
// A nil value means the response did not identify a managed partner key.
type PartnerUsage struct {
	Managed       bool
	Limit         int64
	Used          int64
	Remaining     *int64
	BlockedReason string
}

func ParsePartnerUsage(body map[string]interface{}) *PartnerUsage {
	raw, ok := body["partner"].(map[string]interface{})
	if !ok || raw["managed"] != true {
		return nil
	}
	limit, ok1 := nonnegativeBytes(raw["traffic_limit_bytes"])
	used, ok2 := nonnegativeBytes(raw["traffic_used_bytes"])
	if !ok1 || !ok2 {
		return nil
	}
	p := &PartnerUsage{Managed: true, Limit: limit, Used: used}
	if v, exists := raw["traffic_remaining_bytes"]; exists && v != nil {
		n, ok := nonnegativeBytes(v)
		if !ok {
			return nil
		}
		p.Remaining = &n
	}
	if reason, ok := raw["blocked_reason"].(string); ok {
		p.BlockedReason = reason
	}
	return p
}

func nonnegativeBytes(v interface{}) (int64, bool) {
	n, ok := v.(float64)
	return int64(n), ok && n >= 0 && n <= math.MaxInt64 && math.Trunc(n) == n
}

// CheckKeyWithPartner parses the verdict and optional partner metadata from one
// authenticated API response. Unknown verdicts remain non-fatal, as in engine/params.
func CheckKeyWithPartner(ctx context.Context, client *params.Client, key string) (*params.KeyAnswer, *PartnerUsage, error) {
	switch r := client.Call(ctx, "/v1/key", map[string]string{"key": key}).(type) {
	case params.Ok:
		a := &params.KeyAnswer{}
		verdict, ok := r.Body["verdict"].(string)
		if !ok {
			a.NoVerdict = true
			return a, nil, nil
		}
		switch strings.ToLower(verdict) {
		case "ok":
			a.State = params.KeyActive
		case "expired":
			a.State = params.KeyExpired
		case "revoked":
			a.State = params.KeyRevoked
		case "deleted":
			a.State = params.KeyDeleted
		case "deactivated":
			a.State = params.KeyDeactivated
		case "rotated":
			a.State = params.KeyRotated
		case "unknown":
			a.State = params.KeyUnknown
		default:
			a.State = params.KeyUnrecognised
		}
		a.Bound, _ = r.Body["bound"].(string)
		if tier, _ := r.Body["tier"].(string); strings.EqualFold(tier, params.TierBasic) {
			a.Tier = params.TierBasic
		}
		if expires, ok := nonnegativeBytes(r.Body["expires_at"]); ok {
			a.ExpiresAt = expires
		}
		return a, ParsePartnerUsage(r.Body), nil
	case params.Refused:
		return nil, nil, &params.Error{Kind: params.KindRefused, Msg: r.Err, RetryAfter: r.RetryAfter, Code: r.Code}
	case params.Incompatible:
		return nil, nil, &params.Error{Kind: params.KindIncompatible, Msg: "несовместимый договор"}
	case params.Unavailable:
		return nil, nil, &params.Error{Kind: params.KindUnavailable, Msg: r.Why}
	}
	return nil, nil, &params.Error{Kind: params.KindUnavailable, Msg: "неизвестный исход обращения"}
}
