package core

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"meridian/engine/params"
)

type capturePartnerEngine struct {
	called bool
	cfg    EngineConfig
}

func (e *capturePartnerEngine) Start(_ context.Context, c EngineConfig, _ PacketFlow) (EngineHandle, <-chan EngineEvent, error) {
	e.called = true
	e.cfg = c
	return nil, nil, errors.New("stop before network")
}

func TestPartnerConnectUsesPartnerGatewayWithoutSharedFallback(t *testing.T) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"routing_scope": "partner", "gateway": "203.0.113.20", "gateway_public_key": base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), "gateway_dtls_fingerprint": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "gateway_tls_fingerprints": []string{}, "entries": []any{map[string]any{"host": "203.0.113.20", "port": 443, "transport": "udp", "dtls": true}}, "relay": map[string]any{"enabled": true, "port": 56005, "dtls": true}}
	raw, _ := json.Marshal(body)
	a, err := params.ParseAnswer(raw)
	if err != nil {
		t.Fatal(err)
	}
	engine := &capturePartnerEngine{}
	_, _ = Connect(context.Background(), &Store{Path: filepath.Join(t.TempDir(), "state.json")}, newFakePlatform(), engine, fakeFetcher{ans: a}, ConnectOptions{KeyMaterial: "test-key", Gateway: "ordinary.example", CachedFallback: "198.51.100.1", PartnerRouting: true})
	if !engine.called || engine.cfg.Gateway != "203.0.113.20" || engine.cfg.CachedFallback != "" {
		t.Fatal("partner session used incorrect gateway or fallback")
	}
}

func TestUnknownKeyCannotUseBuiltinRelayWhenAPIOffline(t *testing.T) {
	engine := &capturePartnerEngine{}
	_, err := Connect(context.Background(), &Store{Path: filepath.Join(t.TempDir(), "state.json")}, newFakePlatform(), engine, fakeFetcher{err: silent()}, ConnectOptions{KeyMaterial: "new-test-key", Gateway: "ordinary.example", Ladder: LadderOptions{Mode: LadderModeRelayOnly, Hashes: []string{"test-hash"}}, ParamsCache: &ParamsCache{Dir: t.TempDir()}})
	if err == nil || engine.called {
		t.Fatal("unclassified key used builtin relay")
	}
}
