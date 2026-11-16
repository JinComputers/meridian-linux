// Package rudirect — «RU-адреса напрямую» (решение владельца 25.09,
// meridian-android-src docs/ru-direct.md, кот 3): российские блоки IPv4 и
// домены мимо туннеля. Общее для настольных платформ; маршруты ставит
// платформа (core.RUDirectRouter), домены идут в прямые домены белого
// режима.
//
// Источник — ОБЩИЕ ЗАМОРОЖЕННЫЕ снимки (решение владельца, кот 2; те же у
// роутеров и других клиентов): JinComputers/meridian-router, ветка entware.
// Вшиты как есть, сжатыми (scripts/fetch-ru-snapshots.sh); sha256
// исходного текста сверяется при загрузке — подменённый или битый файл не
// применяется.
package rudirect

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

//go:embed ru-ranges-2026.08.18.txt.gz
var rangesGz []byte

//go:embed ru-whitelist-2026.10.18.txt.gz
var domainsGz []byte

//go:embed ru-iskljucheniya-2026.10.18.txt.gz
var exclusionsGz []byte

const (
	rangesSHA256  = "19782c654e4dc03bfac9392fc1f8cd972afe0d99c3bb22b3b7e23c22d2e81200"
	// Корни белого списка мобильного интернета (Кот 2, решение владельца
	// 18.10.2026) и исключения: эти домены всегда идут в туннель.
	domainsSHA256    = "bb860964eee4da53540d0a8a6251740362f682097bf6e5477c9a70dc7b9469ea"
	exclusionsSHA256 = "f3ad67a5455ceb97c4de0085c91bc8036dbad7b6d231105e593e72cc58cd3e84"
	// rangesDate — дата реестра RIPE в снимке адресов.
	rangesDate = "20260817"
)

// Domains — корни белого списка (119 шт., ru-whitelist-2026.10.18): в белом
// режиме напрямую идёт только домен, чей суффикс входит в список (mail.ru
// покрывает e.mail.ru); адресные диапазоны RU не используются. Резолвятся по
// ответам DNS туннеля, адрес идёт маршрутом /32 мимо туннеля.
// Пусто, если снимок не прошёл проверку sha256.
var Domains = mustLines(domainsGz, domainsSHA256)

// Exclusions — домены, которые в белом режиме ВСЕГДА идут в туннель, даже
// если подходят под корень или имеют российский адрес (ru-iskljucheniya-
// 2026.10.18, 19 шт.). Исключение перебивает автоматический список, но не
// ручной выбор человека.
var Exclusions = mustLines(exclusionsGz, exclusionsSHA256)

// IsRoot — запись есть в списке корней (то есть пришла из встроенного
// списка, а не только из ручного).
func IsRoot(entry string) bool { return rootSet[strings.ToLower(strings.TrimSuffix(entry, "."))] }

// IsExcluded — имя или любой его суффикс входит в исключения.
func IsExcluded(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for name != "" {
		if exclSet[name] {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			return false
		}
		name = name[i+1:]
	}
	return false
}

var rootSet, exclSet = toSet(Domains), toSet(Exclusions)

func toSet(l []string) map[string]bool {
	m := make(map[string]bool, len(l))
	for _, d := range l {
		m[strings.ToLower(d)] = true
	}
	return m
}

// Prefixes — все российские блоки из снимка.
func Prefixes() ([]netip.Prefix, error) {
	lines, err := lines(rangesGz, rangesSHA256)
	if err != nil {
		return nil, err
	}
	out := make([]netip.Prefix, 0, len(lines))
	for _, l := range lines {
		p, err := netip.ParsePrefix(l)
		if err != nil || !p.Addr().Is4() || p.Bits() < 8 || p.Masked() != p {
			return nil, fmt.Errorf("снимок RU-адресов: строка %q неверна", l)
		}
		out = append(out, p)
	}
	return out, nil
}

// Date — дата реестра (ГГГГММДД), из которого собран снимок адресов.
func Date() string { return rangesDate }

func mustLines(gz []byte, sum string) []string {
	l, err := lines(gz, sum)
	if err != nil {
		return nil
	}
	return l
}

// lines — распаковать, сверить sha256 исходного текста, строки без
// комментариев (#) и пустых.
func lines(gz []byte, sum string) ([]string, error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("снимок: %w", err)
	}
	text, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("снимок: %w", err)
	}
	h := sha256.Sum256(text)
	if hex.EncodeToString(h[:]) != sum {
		return nil, fmt.Errorf("снимок: sha256 не сходится")
	}
	var out []string
	for _, l := range strings.Split(string(text), "\n") {
		if i := strings.IndexByte(l, '#'); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}
