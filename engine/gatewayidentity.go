package engine

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// gatewayIdentity принадлежит одной сессии; срез пинов всегда копируется.
type gatewayIdentity struct {
	publicKey       *ecdh.PublicKey
	dtlsFingerprint string
	tlsFingerprints []string
}

func defaultGatewayIdentity() gatewayIdentity {
	return gatewayIdentity{gatewayStaticPublicKey, gatewayDTLSFingerprint, append([]string(nil), gwCertFingerprints...)}
}

func (g gatewayIdentity) clone() gatewayIdentity {
	g.tlsFingerprints = append([]string(nil), g.tlsFingerprints...)
	return g
}

func validGatewayFingerprint(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(s) == 64 && len(b) == 32
}

// SetGatewayIdentity задаёт доверенную identity только этой Ladder.
// tlsFingerprints — JSON-массив SHA256 DER (например ["64hex"]), не SPKI.
// Пустые DTLS/TLS значения допустимы только если этих транспортов нет в Ladder.
// Ошибка блокирует Connect этой Ladder, даже если вызывающий проигнорировал error.
// Для обычного шлюза создаётся НОВАЯ Ladder без этого вызова; глобального reset нет.
func (l *Ladder) SetGatewayIdentity(publicKey, dtlsFingerprint, tlsFingerprints string) error {
	l.identityMu.Lock()
	defer l.identityMu.Unlock()
	fail := func() error {
		l.identity = nil
		l.identityErr = errors.New("некорректная identity шлюза")
		return l.identityErr
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(publicKey)
	if err != nil || len(raw) != 32 || base64.StdEncoding.EncodeToString(raw) != publicKey {
		return fail()
	}
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return fail()
	}
	probe, _ := ecdh.X25519().NewPrivateKey(make([]byte, 32))
	if _, err = probe.ECDH(pub); err != nil {
		return fail()
	}
	if dtlsFingerprint != "" && !validGatewayFingerprint(dtlsFingerprint) {
		return fail()
	}
	var pins []string
	if err = json.Unmarshal([]byte(tlsFingerprints), &pins); err != nil || pins == nil {
		return fail()
	}
	for i, pin := range pins {
		if !validGatewayFingerprint(pin) {
			return fail()
		}
		pins[i] = strings.ToLower(pin)
	}
	l.identity = &gatewayIdentity{pub, strings.ToLower(dtlsFingerprint), pins}
	l.identityErr = nil
	return nil
}

func (l *Ladder) gatewayIdentitySnapshot() (gatewayIdentity, error) {
	l.identityMu.RLock()
	defer l.identityMu.RUnlock()
	if l.identityErr != nil {
		return gatewayIdentity{}, l.identityErr
	}
	if l.identity == nil {
		return defaultGatewayIdentity(), nil
	}
	g := l.identity.clone()
	for _, c := range l.items {
		if c.transport == transTLS && len(g.tlsFingerprints) == 0 {
			return gatewayIdentity{}, errors.New("для TLS шлюза не заданы отпечатки")
		}
		if c.transport == transUDP && c.useDTLS && g.dtlsFingerprint == "" {
			return gatewayIdentity{}, errors.New("для DTLS шлюза не задан отпечаток")
		}
	}
	return g, nil
}

func (s *session) verifyGatewayDTLS(certs [][]byte) error {
	if len(certs) == 0 || s.identity.dtlsFingerprint == "" ||
		certFingerprint(certs[0]) != s.identity.dtlsFingerprint {
		return errGatewayNotAuthentic
	}
	return nil
}
