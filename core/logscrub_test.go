package core

import (
	"strings"
	"testing"
)

func TestScrubLog(t *testing.T) {
	in := strings.Join([]string{
		`ключ: TestKey0000ZZZZ99`,
		`ВК: https://vk.com/call/join/AbCdEfGhIjKlMnOpQrStUvWxYz0123456789 и хеш AbCdEfGhIjKlMnOpQrStUvWxYz0123456789`,
		`connect: device="meridian-win-0123456789abcdef0123"`,
		`внешний 95.24.117.33, сайт 104.26.12.205/32, сеть 91.108.4.0/22`,
		`вход 62.76.231.231, шлюз 10.77.77.1, роутер 192.168.1.1, CGNAT 100.72.1.2`,
		`v6 2a02:6b8:a::a, петля ::1`,
		`время 11:39:04 и 2026/09/25`,
		`путь C:\\Users\\Ivan\\AppData и C:\Users\Ivan\x, /Users/ivan/Library`,
		`sid=S-1-5-21-111111111-222222222-333333333-1001`,
		`хост IVAN-PC, почта ivan.petrov@example.org`,
	}, "\n")
	out := ScrubLog(in, ScrubOptions{
		Secrets:   []string{"TestKey0000ZZZZ99"},
		Hashes:    []string{"AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"},
		DeviceIDs: []string{"meridian-win-0123456789abcdef0123"},
		Names:     []string{"ivan-pc"},
		Keep:      []string{"62.76.231.231"},
	})
	for _, bad := range []string{
		"TestKey0000ZZZZ99", "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789", "0123456789abcdef0123",
		"95.24.117.33", "104.26.12.205", "2a02:6b8:a::a", `\Ivan`, "/ivan/", "111111111", "IVAN-PC", "ivan.petrov",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("в выдаче осталось %q:\n%s", bad, out)
		}
	}
	for _, good := range []string{
		"[скрыто]", "AbCd…6789", "…0123", "95.24.x.x", "104.26.x.x/32", "91.108.x.x/22",
		"62.76.231.231", "10.77.77.1", "192.168.1.1", "100.72.1.2", "2a02:…", "::1",
		"11:39:04", `\Users\\<пользователь>`, "/Users/<пользователь>/", "S-1-5-21-…", "<имя>", "i…@example.org",
	} {
		if !strings.Contains(out, good) {
			t.Errorf("в выдаче нет %q:\n%s", good, out)
		}
	}
}
