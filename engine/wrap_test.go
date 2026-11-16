package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"io"
	"testing"

	"golang.org/x/crypto/hkdf"
)

// Контрольный вектор кота 1 (12.10.2026, сверено Go и Python).
// Пароль выдуманный, к боевым отношения не имеет.
func TestDeriveWrapKeyMeridianSalt(t *testing.T) {
	k, err := deriveWrapKey("abcdefghijklmnopqrstuvwxyz234567")
	if err != nil {
		t.Fatal(err)
	}
	got := base64.StdEncoding.EncodeToString(k)
	const want = "9gNP/XW4ES+NUflECC0O8li3cx4zthwD84WtbbsoGaQ="
	if got != want {
		t.Fatalf("wrap_key %s, ждали %s", got, want)
	}
}

// Старая соль даёт старый ключ — вектор сверен, и видно, что поменялась
// только соль: тот же пароль, тот же info.
func TestWrapKeyOldSaltVector(t *testing.T) {
	k := make([]byte, wrapKeyLen)
	r := hkdf.New(sha256.New, []byte("abcdefghijklmnopqrstuvwxyz234567"),
		[]byte("WDTT-WRAP-v1"), []byte("rtp-obfs/chacha20poly1305"))
	if _, err := io.ReadFull(r, k); err != nil {
		t.Fatal(err)
	}
	got := base64.StdEncoding.EncodeToString(k)
	const want = "RaNajD5j6EkZXBP6zhSquuJwWocgLoUbu/Jyyxk5ZxQ="
	if got != want {
		t.Fatalf("старый wrap_key %s, ждали %s", got, want)
	}
}
