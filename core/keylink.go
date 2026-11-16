package core

import (
	"net/url"
	"strconv"
	"strings"
)

// ParseKeyInput — ключ подписчика из того, что вставил человек или открыла
// ссылка: голый ключ, meridian://key/<ключ>, meridian://key?k=<ключ> или
// полная форма meridian://<хост>:<порт>:<порт>:<порт>:<ключ>[:<хеш>] (ключ —
// пятое поле, хост, порты и хеш клиенту не нужны; портов три и они числа).
// Старая схема wdtt:// принимается только на вход в тех же формах и нигде не
// показывается и не регистрируется. Другие ссылки не принимаются. Ключ не
// логировать.
func ParseKeyInput(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		if sch := s[:i]; !strings.EqualFold(sch, "meridian") && !strings.EqualFold(sch, "wdtt") {
			return "", false
		}
		rest := s[i+3:]
		if k, ok := parseFullLink(rest); ok {
			s = k
		} else {
			u, err := url.Parse(s)
			if err != nil || !strings.EqualFold(u.Host, "key") {
				return "", false
			}
			k := strings.Trim(u.Path, "/")
			if k == "" {
				k = u.Query().Get("k")
			}
			if strings.Contains(k, "/") {
				return "", false
			}
			s = strings.TrimSpace(k)
		}
	}
	if len(s) == 0 || strings.ContainsAny(s, " \t\r\n") {
		return "", false
	}
	return s, true
}

// parseFullLink — хост:порт1:порт2:порт3:ключ[:хеш] (без схемы).
func parseFullLink(rest string) (string, bool) {
	if strings.ContainsAny(rest, "/?#") {
		return "", false
	}
	f := strings.Split(rest, ":")
	if len(f) != 5 && len(f) != 6 || f[0] == "" || f[4] == "" {
		return "", false
	}
	for _, p := range f[1:4] {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	return f[4], true
}

// MaskKey — последние четыре знака ключа, чтобы человек узнал его, не раскрывая.
func MaskKey(k string) string {
	if len(k) <= 4 {
		return "…"
	}
	return "…" + k[len(k)-4:]
}
