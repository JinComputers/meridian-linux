package core

import (
	"context"
	"encoding/json"
	"errors"
	"meridian/engine/params"
)

// PartnerRoute is present only in a complete, trusted partner-v1 response.
type PartnerRoute struct {
	Gateway         string   `json:"gateway"`
	PublicKey       string   `json:"gateway_public_key"`
	DTLSFingerprint string   `json:"gateway_dtls_fingerprint"`
	TLSFingerprints []string `json:"gateway_tls_fingerprints"`
	RoutingScope    string   `json:"routing_scope"`
	Relay           struct {
		Port    int32  `json:"port"`
		DTLS    *bool  `json:"dtls"`
		Enabled bool   `json:"enabled"`
		Target  string `json:"target"`
	} `json:"relay"`
}

func ParsePartnerRoute(raw []byte) (*PartnerRoute, error) {
	var r PartnerRoute
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.RoutingScope != "partner" || r.Gateway == "" || r.PublicKey == "" || r.Relay.DTLS == nil || r.Relay.Port < 1 || r.Relay.Port > 65535 {
		return nil, errors.New("неполный партнёрский маршрут")
	}
	if r.Relay.Enabled && r.Relay.Target == "" {
		return nil, errors.New("у партнёрского релея нет адреса")
	}
	return &r, nil
}

// PartnerParamsFetcher never accepts an ordinary or incomplete answer.
type PartnerParamsFetcher struct {
	Client      *params.Client
	RelayClient *ParamsRace
}

func (p PartnerParamsFetcher) Fetch(ctx context.Context, key string) (*ParamsAnswer, error) {
	result, ok := p.Client.Call(ctx, "/v1/params", map[string]string{"key": key, "routing_contract": "partner-v1", "transports": "udp,tcp,tls"}).(params.Ok)
	if !ok {
		return nil, errors.New("партнёрская выдача недоступна")
	}
	if _, err := ParsePartnerRoute(result.Raw); err != nil {
		return nil, err
	}
	return params.ParseAnswer(result.Raw)
}
