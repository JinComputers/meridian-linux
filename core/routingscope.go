package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"meridian/engine/params"
)

func KeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// OrdinaryRoutingConfirmed requires the additive routing contract. An old
// server which ignores the query cannot classify a new key as ordinary.
func OrdinaryRoutingConfirmed(ctx context.Context, client *params.Client, key string) bool {
	r, ok := client.Call(ctx, "/v1/params", map[string]string{"key": key, "routing_contract": "partner-v1"}).(params.Ok)
	if !ok {
		return false
	}
	scope, _ := r.Body["routing_scope"].(string)
	return scope == "ordinary"
}
