package core

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// direct.go — прямые домены белого режима (этап 3, docs/plan-etap3-
// white-mode.md): домены, которые идут МИМО туннеля. Общий для Mac и
// Windows — разбор списка здесь, установка маршрутов/DNS — у платформы
// (DirectRouter).

// DirectRouter — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы (проверяется через
// приведение типа в Connect), не новый метод core.Platform: иначе
// Windows-сборка кота 6 перестала бы собираться, пока он его не
// напишет. Платформа без него + непустой список доменов → Connect
// отказывает вслух, а не туннелирует всё молча.
type DirectRouter interface {
	// ApplyDirect ставит DNS прямых доменов и маршруты для их текущих
	// адресов; всё поставленное — в журнал через commit ДО действия
	// (тот же порядок, что Platform.Apply). Снимает — Platform.Restore
	// по журналу, отдельного метода снятия нет.
	ApplyDirect(ctx context.Context, st *State, domains []string, commit func() error) error
	// RefreshDirect — повторный резолв, дописывает маршруты для новых
	// адресов (CDN меняют адреса); DNS не трогает, ничего не снимает.
	RefreshDirect(ctx context.Context, domains []string) error
}

// ParseDirectDomains — список из поля ввода (по одному в строке; также
// запятая, точка с запятой, пробел) → нормализованные имена: регистр
// вниз, схема/путь/порт/точка в конце срезаются, "*." в начале
// срезается (поддомены и так ловит DNS-сопоставление), IP-адреса
// допускаются как есть, дубликаты — один раз, порядок первого
// появления. Кириллица → punycode. Мусор (пусто, без точки, чужие
// символы) пропускается.
func ParseDirectDomains(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '\n', '\r', '\t', ' ', ',', ';':
			return true
		}
		return false
	})
	seen := map[string]bool{}
	var out []string
	for _, f := range fields {
		d := normalizeDirectDomain(f)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func normalizeDirectDomain(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	}
	// Сеть a.b.c.d/n (чёрный режим: подсети сервисов) — до среза пути,
	// иначе «/24» ушло бы как путь и осталась бы одна точка.
	if p, err := netip.ParsePrefix(s); err == nil {
		if !p.Addr().Is4() {
			return ""
		}
		return p.Masked().String()
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.ToLower(strings.TrimSuffix(s, "."))
	s = strings.TrimPrefix(s, "*.")
	if ip := net.ParseIP(s); ip != nil {
		if ip.To4() == nil {
			return "" // прямой путь только v4 (план, раздел 4, п.2)
		}
		return ip.String()
	}
	// Кириллические домены (.рф и т.п. — в белых списках их много) →
	// punycode: DNS и SupplementalMatchDomains понимают только ASCII.
	if a, err := idna.Lookup.ToASCII(s); err == nil {
		s = a
	} else {
		return ""
	}
	if !strings.Contains(s, ".") {
		return ""
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return ""
		}
	}
	return s
}
