package params

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPartnerParamsMustHaveOwnIdentityAndRelay(t *testing.T) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"routing_scope": "partner", "gateway": "203.0.113.20",
		"gateway_public_key":       base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()),
		"gateway_dtls_fingerprint": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"gateway_tls_fingerprints": []string{"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		"entries":                  []any{map[string]any{"host": "203.0.113.20", "port": 443, "transport": "udp", "dtls": true}},
		"relay":                    map[string]any{"enabled": true, "port": 56005, "dtls": true},
	}
	parse := func() (*Answer, error) {
		b, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		return ParseAnswer(b)
	}
	a, err := parse()
	if err != nil {
		t.Fatal(err)
	}
	if a.Gateway != "203.0.113.20" || a.Relay.Port != 56005 || !a.Relay.DTLS {
		t.Fatal("partner route not retained")
	}
	if _, _, err = BuildLadder(a, LadderOptions{}); err != nil {
		t.Fatal(err)
	}
	delete(body, "gateway_public_key")
	if _, err = parse(); err == nil {
		t.Fatal("missing public key accepted")
	}
	body["gateway_public_key"] = base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	delete(body["relay"].(map[string]any), "port")
	if _, err = parse(); err == nil {
		t.Fatal("missing relay port accepted")
	}
	body["relay"].(map[string]any)["port"] = 56005
	delete(body["entries"].([]any)[0].(map[string]any), "dtls")
	if _, err = parse(); err == nil {
		t.Fatal("missing UDP DTLS flag accepted")
	}
}

func TestFetchRequestsScopeAndRejectsLegacyResponse(t *testing.T) {
	var query string
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("routing_contract")
		_, _ = w.Write([]byte(`{"contract":"v1","status":"answered","entries":[{"host":"203.0.113.1","port":443}]}`))
	}))
	defer s.Close()
	c := New(Config{Addresses: []Address{addrOf(t, s, true)}, Pins: []string{SPKIPin(s.Certificate())}})
	_, err := c.Fetch(context.Background(), "test-key")
	if query != "partner-v1" || err == nil {
		t.Fatalf("routing contract %q: legacy response must be rejected", query)
	}
}
