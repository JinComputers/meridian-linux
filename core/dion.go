package core

// dion.go — комнаты DION (dion.vc, видеозвонки ВТБ) как второе звено
// ретранслятора рядом с VK (решение владельца 29.09 через кота 1).
// Договор по вводу и хранению — как в панели роутеров кота 2 (6.84):
// ссылка https://dion.vc/event/<slug> или голый slug, до 3 комнат,
// хранится нормализованной ссылкой, без повторов, в порядке ввода.
// Сами креды TURN из комнаты добывает движок (кот 3), здесь только ввод.

import (
	"net/url"
	"regexp"
	"strings"
)

// DionMaxRooms — предел комнат (как в панели роутеров).
const DionMaxRooms = 3

// Основное звено ретранслятора (ключ RELAY_PRIMARY у роутеров).
const (
	RelayPrimaryVK   = "vk"
	RelayPrimaryDion = "dion"
)

var dionSlugRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ParseDionRooms разбирает ввод человека: ссылки и slug через запятую,
// пробел или перевод строки. slugs — годные, без повторов, в порядке
// ввода (без обрезки по DionMaxRooms — лимит решает вызывающий, чтобы
// сказать человеку, что лишнее). bad — куски, не похожие на комнату.
func ParseDionRooms(text string) (slugs, bad []string) {
	seen := map[string]bool{}
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t'
	}) {
		slug, ok := dionSlug(tok)
		if !ok {
			bad = append(bad, tok)
			continue
		}
		if !seen[slug] {
			seen[slug] = true
			slugs = append(slugs, slug)
		}
	}
	return slugs, bad
}

// dionCodeRE — код комнаты вида abc-def-ghi, как DION печатает его в
// ссылках и приглашениях (Android 0.1.243, владелец 06.10).
var dionCodeRE = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])([a-z]{3}-[a-z]{3}-[a-z]{3})(?:$|[^A-Za-z0-9])`)

func dionSlug(tok string) (string, bool) {
	if dionSlugRE.MatchString(tok) {
		return tok, true
	}
	if slug, ok := dionEventSlug(tok); ok {
		return slug, true
	}
	// Ссылка не того вида, что dion.vc/event/<код>, но про DION — код берём
	// из неё (клиент 06.10 не смог добавить комнату).
	if strings.Contains(strings.ToLower(tok), "dion") {
		if m := dionCodeRE.FindStringSubmatch(tok); m != nil {
			return m[1], true
		}
	}
	return "", false
}

func dionEventSlug(tok string) (string, bool) {
	s := tok
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host != "dion.vc" {
		return "", false
	}
	rest, ok := strings.CutPrefix(u.Path, "/event/")
	if !ok {
		return "", false
	}
	slug := strings.TrimSuffix(rest, "/")
	if !dionSlugRE.MatchString(slug) {
		return "", false
	}
	return slug, true
}

// dionRoomsForLadder — комнаты из протокола, как их отдать движку: только
// годные slug (помощник не доверяет интерфейсу), без повторов, не больше
// DionMaxRooms.
func dionRoomsForLadder(rooms []string) []string {
	slugs, _ := ParseDionRooms(strings.Join(rooms, ","))
	if len(slugs) > DionMaxRooms {
		slugs = slugs[:DionMaxRooms]
	}
	return slugs
}

// DionRoomURL — нормализованная ссылка, как её хранит панель.
func DionRoomURL(slug string) string { return "https://dion.vc/event/" + slug }

// EffectiveRelayPrimary — основное звено с учётом правила панели: dion
// действует, только если комнаты есть; иначе и по умолчанию — vk.
func EffectiveRelayPrimary(primary string, rooms []string) string {
	if primary == RelayPrimaryDion && len(rooms) > 0 {
		return RelayPrimaryDion
	}
	return RelayPrimaryVK
}
