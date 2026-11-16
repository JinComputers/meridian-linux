package core

import (
	"net/netip"
	"regexp"
	"strings"
)

// logscrub.go — обезличивание логов перед выдачей наружу (кнопки «Отправить
// в поддержку» / «Сохранить», хвост лога службы/помощника). Правила
// согласованы с котом 3 (Android) 25.09:
//   - ключ подписчика — не пишется вообще; здесь ещё и вычёркивается по
//     значению, если оно известно (Secrets);
//   - ссылки и хеши звонков ВК → первые 4…последние 4 (по ним можно войти
//     в звонок);
//   - device_id → «…последние 4»;
//   - публичные IPv4 → a.b.x.x (внешний адрес человека, адреса сайтов),
//     КРОМЕ адресов узлов Meridian (Keep: входы, релей, шлюз — без них
//     поддержке не разобрать, какая ступень что делала; они и так публичны
//     в /v1/params). Частные/служебные — как есть;
//   - IPv6 → первая группа;
//   - имя пользователя в путях (C:\Users\X, /Users/X), SID Windows, имя
//     компьютера и пользователя → заглушки; адреса e-mail → маска.
// Домены, которые человек посещал, в логи не пишутся вовсе (это на
// стороне тех, кто пишет лог, — здесь их не угадать).

// ScrubOptions — что известно вызывающему.
type ScrubOptions struct {
	Secrets   []string // вычеркнуть целиком (ключ подписчика и т. п.)
	Hashes    []string // хеши звонков ВК → первые 4…последние 4
	DeviceIDs []string // → «…последние 4»
	Names     []string // имя компьютера, имя пользователя → заглушка
	Keep      []string // адреса узлов Meridian — не маскировать
}

var (
	reVKLink = regexp.MustCompile(`(?i)(vk\.(?:com|ru)/call/join/)([A-Za-z0-9_\-]{6,})`)
	reIPv4   = regexp.MustCompile(`\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b`)
	// IPv6: не меньше трёх двоеточий или «::» — чтобы время 11:39:04 не
	// принималось за адрес.
	reIPv6    = regexp.MustCompile(`\b[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{0,4}){3,7}\b|\b[0-9A-Fa-f]{1,4}::[0-9A-Fa-f:]*\b`)
	reWinUser = regexp.MustCompile(`(?i)(\\Users\\+)([^\\"\s]+)`)
	reMacUser = regexp.MustCompile(`(/Users/)([^/"\s]+)`)
	reSID     = regexp.MustCompile(`S-1-5-21(?:-\d+){1,4}`)
	reEmail   = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
)

func shortMid(s string) string {
	if len(s) <= 8 {
		return "…"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

func shortTail(s string) string {
	if len(s) <= 4 {
		return "…"
	}
	return "…" + s[len(s)-4:]
}

// ScrubLog — обезличенная копия текста лога.
func ScrubLog(text string, o ScrubOptions) string {
	for _, s := range o.Secrets {
		if len(s) >= 6 {
			text = strings.ReplaceAll(text, s, "[скрыто]")
		}
	}
	for _, h := range o.Hashes {
		if len(h) > 8 {
			text = strings.ReplaceAll(text, h, shortMid(h))
		}
	}
	text = reVKLink.ReplaceAllStringFunc(text, func(m string) string {
		sub := reVKLink.FindStringSubmatch(m)
		return sub[1] + shortMid(sub[2])
	})
	for _, d := range o.DeviceIDs {
		if len(d) > 4 {
			text = strings.ReplaceAll(text, d, shortTail(d))
		}
	}
	text = reEmail.ReplaceAllStringFunc(text, func(m string) string {
		if i := strings.IndexByte(m, '@'); i > 0 {
			return m[:1] + "…@" + m[i+1:]
		}
		return "…"
	})
	text = reSID.ReplaceAllString(text, "S-1-5-21-…")
	text = reWinUser.ReplaceAllString(text, `${1}<пользователь>`)
	text = reMacUser.ReplaceAllString(text, `${1}<пользователь>`)
	for _, n := range o.Names {
		if len(n) >= 3 {
			text = replaceFold(text, n, "<имя>")
		}
	}

	keep := map[netip.Addr]bool{}
	for _, k := range o.Keep {
		if a, err := netip.ParseAddr(k); err == nil {
			keep[a] = true
		}
	}
	text = reIPv4.ReplaceAllStringFunc(text, func(m string) string {
		a, err := netip.ParseAddr(m)
		if err != nil || !a.Is4() || keep[a] || !a.IsGlobalUnicast() || a.IsPrivate() || isCGNAT(a) {
			return m
		}
		b := a.As4()
		return itoa(b[0]) + "." + itoa(b[1]) + ".x.x"
	})
	text = reIPv6.ReplaceAllStringFunc(text, func(m string) string {
		a, err := netip.ParseAddr(m)
		if err != nil || !a.Is6() || keep[a] || a.IsLoopback() || a.IsLinkLocalUnicast() {
			return m
		}
		if i := strings.IndexByte(m, ':'); i > 0 {
			return m[:i] + ":…"
		}
		return "…"
	})
	return text
}

func isCGNAT(a netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(a)
}

func itoa(b byte) string {
	const d = "0123456789"
	if b < 10 {
		return d[b : b+1]
	}
	if b < 100 {
		return string([]byte{d[b/10], d[b%10]})
	}
	return string([]byte{d[b/100], d[b/10%10], d[b%10]})
}

// replaceFold — замена без учёта регистра (имя компьютера бывает в разном
// регистре).
func replaceFold(s, old, repl string) string {
	lo, lold := strings.ToLower(s), strings.ToLower(old)
	if !strings.Contains(lo, lold) {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(lo[i:], lold)
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		b.WriteString(s[i : i+j])
		b.WriteString(repl)
		i += j + len(old)
	}
}
