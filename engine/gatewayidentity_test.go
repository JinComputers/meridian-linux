package engine

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

func identityFixture(t *testing.T) (string, string, string, []byte) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := []byte("partner certificate fixture")
	pin := certFingerprint(cert)
	pins, _ := json.Marshal([]string{pin})
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), pin, string(pins), cert
}

func TestGatewayIdentityIsolationAndSnapshot(t *testing.T) {
	pub, dtls, pins, cert := identityFixture(t)
	ordinary := NewLadder()
	before, err := ordinary.gatewayIdentitySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	partner := NewLadder()
	partner.AddDirect("udp", "192.0.2.1", 443, true)
	partner.AddTLS("tls", "192.0.2.1", 443, "partner.example")
	if err = partner.SetGatewayIdentity(pub, dtls, pins); err != nil {
		t.Fatal(err)
	}
	snapshot, err := partner.gatewayIdentitySnapshot()
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(nil)
	s.identity = snapshot
	if err = s.verifyGatewayDTLS([][]byte{cert}); err != nil {
		t.Fatal(err)
	}
	if err = s.verifyGatewayDTLS([][]byte{[]byte("wrong")}); err == nil {
		t.Fatal("DTLS accepted wrong cert")
	}
	cfg := gatewayTLSConfigWithPins("partner.example", s.identity.tlsFingerprints)
	if cfg.VerifyPeerCertificate == nil {
		t.Fatal("TLS verifier missing")
	}
	if err = cfg.VerifyPeerCertificate([][]byte{cert}, nil); err != nil {
		t.Fatal(err)
	}
	if err = cfg.VerifyPeerCertificate([][]byte{[]byte("wrong")}, nil); err == nil {
		t.Fatal("TLS accepted wrong cert")
	}
	if s.identity.publicKey.Equal(before.publicKey) {
		t.Fatal("partner inherited ordinary NK identity")
	}
	// Изменение Ladder после снимка не меняет уже созданную сессию.
	otherPub, _, _, _ := identityFixture(t)
	if err = partner.SetGatewayIdentity(otherPub, dtls, pins); err != nil {
		t.Fatal(err)
	}
	if base64.StdEncoding.EncodeToString(s.identity.publicKey.Bytes()) != pub {
		t.Fatal("snapshot changed")
	}
	snapshot.tlsFingerprints[0] = "mutated snapshot"
	next, _ := partner.gatewayIdentitySnapshot()
	if next.tlsFingerprints[0] != dtls {
		t.Fatal("pin slice shared")
	}
	after, _ := NewLadder().gatewayIdentitySnapshot()
	if !after.publicKey.Equal(before.publicKey) || after.dtlsFingerprint != before.dtlsFingerprint ||
		after.tlsFingerprints[0] != before.tlsFingerprints[0] {
		t.Fatal("ordinary identity changed")
	}
	fresh := newSession(nil)
	if !fresh.identity.publicKey.Equal(before.publicKey) {
		t.Fatal("new session inherited partner")
	}
}

func TestGatewayIdentityInvalidDoesNotFallBack(t *testing.T) {
	pub, dtls, pins, _ := identityFixture(t)
	for _, tc := range [][3]string{
		{"", dtls, pins}, {pub + "\n", dtls, pins},
		{base64.StdEncoding.EncodeToString(make([]byte, 32)), dtls, pins},
		{pub, "not hex", pins}, {pub, dtls, "null"}, {pub, dtls, `"not array"`},
		{pub, dtls, `["not hex"]`},
	} {
		l := NewLadder()
		l.AddDirect("udp", "192.0.2.1", 443, false)
		if l.SetGatewayIdentity(tc[0], tc[1], tc[2]) == nil {
			t.Fatal("invalid accepted")
		}
		if _, err := l.gatewayIdentitySnapshot(); err == nil {
			t.Fatal("invalid fell back")
		}
		if _, err := Connect("invalid.test", "", l, "fixture-password", "fixture-device", nil, nil, nil, nil, nil); err == nil || err.Error() != "некорректная identity шлюза" {
			t.Fatal("Connect must reject invalid identity before resolving or dialing")
		}
	}
	l := NewLadder()
	if err := l.SetGatewayIdentity(pub, "", "[]"); err != nil {
		t.Fatal(err)
	}
	l.AddTLS("tls", "192.0.2.1", 443, "partner.example")
	if _, err := l.gatewayIdentitySnapshot(); err == nil {
		t.Fatal("TLS inherited ordinary pins")
	}
	l = NewLadder()
	_ = l.SetGatewayIdentity(pub, "", "[]")
	l.AddDirect("dtls", "192.0.2.1", 443, true)
	if _, err := l.gatewayIdentitySnapshot(); err == nil {
		t.Fatal("DTLS inherited ordinary pin")
	}
}

func TestGatewayIdentityConcurrentSnapshot(t *testing.T) {
	pub, dtls, pins, _ := identityFixture(t)
	l := NewLadder()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if err := l.SetGatewayIdentity(pub, dtls, pins); err != nil {
					t.Error(err)
				}
				if _, err := l.gatewayIdentitySnapshot(); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
