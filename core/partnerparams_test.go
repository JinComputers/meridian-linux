package core

import "testing"

func TestParsePartnerRouteRejectsIncompleteAndOrdinary(t *testing.T) {
	cases := []string{
		`{"routing_scope":"ordinary","gateway":"46.21.159.203"}`,
		`{"routing_scope":"partner","gateway":"","gateway_public_key":"x","relay":{"port":56004,"dtls":true}}`,
		`{"routing_scope":"partner","gateway":"46.21.159.203","gateway_public_key":"x","relay":{"enabled":true,"port":56004,"dtls":true}}`,
		`{"routing_scope":"partner","gateway":"46.21.159.203","gateway_public_key":"x","relay":{"enabled":true,"target":"46.21.159.203","dtls":true}}`,
	}
	for _, raw := range cases {
		if _, err := ParsePartnerRoute([]byte(raw)); err == nil {
			t.Errorf("accepted incomplete route: %s", raw)
		}
	}
}
func TestParsePartnerRouteAcceptsCompleteShape(t *testing.T) {
	raw := `{"routing_scope":"partner","gateway":"46.21.159.203","gateway_public_key":"x","gateway_dtls_fingerprint":"y","gateway_tls_fingerprints":["z"],"relay":{"enabled":true,"target":"46.21.159.203","port":56004,"dtls":true}}`
	if _, err := ParsePartnerRoute([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}
