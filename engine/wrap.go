package engine

import (
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

// wrapKeyLen — длина ключа ChaCha20-Poly1305.
const wrapKeyLen = 32

// deriveWrapKey — вывод ключа обфускации. Соль и info — константы
// протокола: обе стороны выводят ключ независимо и должны получить один и
// тот же.
//
// СОЛЬ MERIDIAN-WRAP-v1 С 12.10.2026 (кот 1, миграция согласована
// владельцем). Была «WDTT-WRAP-v1». Боевые шлюзы с 12.10 принимают обе;
// старые клиенты работают, а этот клиент на шлюз без новой соли не
// подключится. info и длина прежние. Контрольный вектор — в wrap_test.go.
func deriveWrapKey(password string) ([]byte, error) {
	key := make([]byte, wrapKeyLen)
	r := hkdf.New(
		sha256.New,
		[]byte(password),
		[]byte("MERIDIAN-WRAP-v1"),
		[]byte("rtp-obfs/chacha20poly1305"),
	)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
