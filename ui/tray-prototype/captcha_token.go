package main

import (
	"net/url"
	"strings"
)

// findCaptchaToken — success_token решённой капчи VK из адреса или текста
// страницы. Те же признаки, что у Android (CaptchaActivity): подстрока
// «success_token=» или «"success_token":"», длина ≥ 8, значение URL-декодируется.
// Токен в логи не пишется.
func findCaptchaToken(s string) (string, bool) {
	for _, marker := range []string{`success_token=`, `"success_token":"`, `"success_token": "`} {
		rest := s
		for {
			i := strings.Index(rest, marker)
			if i < 0 {
				break
			}
			rest = rest[i+len(marker):]
			end := strings.IndexAny(rest, `&"'#\ <>`+"\n\r\t")
			v := rest
			if end >= 0 {
				v = rest[:end]
			}
			if d, err := url.QueryUnescape(v); err == nil {
				v = d
			}
			if len(v) >= 8 {
				return v, true
			}
		}
	}
	return "", false
}

// captchaURLAllowed — страницу капчи открываем только по https.
func captchaURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
