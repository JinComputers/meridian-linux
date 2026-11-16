package core

import (
	"context"
	"net/netip"
	"strings"

	"github.com/JinComputers/meridian-desktop/core/rudirect"
)

// tunnelonly.go — чёрный режим (решение владельца 25.09): в туннель идёт
// ТОЛЬКО список человека — домены (с поддоменами), адреса, сети a.b.c.d/n;
// всё остальное напрямую. Пустой список — в туннель не идёт ничего
// (выбор владельца: «всегда только по списку»). Разбор списка — тот же
// ParseDirectDomains, что у белого режима.

// TunnelRouter — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы (как DirectRouter):
// платформа без него + opts.TunnelOnly → Connect отказывает вслух.
type TunnelRouter interface {
	// ApplyTunnelOnly — ВМЕСТО Platform.Apply: адрес/DNS/защита как
	// обычно, но без маршрута по умолчанию в туннель — в туннель только
	// DNS туннеля и адреса из списка (домены — по резолву и по ответам
	// DNS, чтобы ловить поддомены). Журнал и снятие — как у Apply
	// (Platform.Restore).
	ApplyTunnelOnly(ctx context.Context, st *State, t Tun, info TunnelInfo, entries []string, commit func() error) error
	// RefreshTunnelOnly — повторный резолв доменов списка (CDN меняют
	// адреса): только дописывает, ничего не снимает.
	RefreshTunnelOnly(ctx context.Context, entries []string) error
}

// DomainMatcher — «имя попадает в список»: само имя или любой его
// поддомен (youtube.com ловит www.youtube.com, но не notyoutube.com).
// Адреса и сети из списка сюда не входят — их платформа маршрутизирует
// сразу.
type DomainMatcher struct {
	names map[string]bool
	// directExcl — белый режим: записи из встроенного списка корней
	// (rudirect) не срабатывают для имён из исключений; ручные записи
	// человека срабатывают всегда. Только у NewDirectMatcher.
	directExcl bool
	manual     map[string]bool
}

// NewDirectMatcher — домены прямого маршрута белого режима: ручные записи
// человека всегда в силе, записи встроенного списка корней — кроме имён из
// rudirect.Exclusions (те всегда в туннель).
func NewDirectMatcher(entries []string) *DomainMatcher {
	m := NewDomainMatcher(entries)
	m.directExcl = true
	m.manual = map[string]bool{}
	for k := range m.names {
		if !rudirect.IsRoot(k) {
			m.manual[k] = true
		}
	}
	return m
}

func NewDomainMatcher(entries []string) *DomainMatcher {
	m := &DomainMatcher{names: map[string]bool{}}
	for _, e := range entries {
		if e == "" || strings.Contains(e, "/") || isIPv4Literal(e) {
			continue
		}
		m.names[strings.ToLower(strings.TrimSuffix(e, "."))] = true
	}
	return m
}

// Empty — в списке нет ни одного имени.
func (m *DomainMatcher) Empty() bool { return m == nil || len(m.names) == 0 }

func (m *DomainMatcher) Match(name string) bool {
	if m.Empty() {
		return false
	}
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if m.directExcl && rudirect.IsExcluded(name) {
		// Исключение перебивает только встроенный список: ищем ручную запись.
		for n := name; n != ""; {
			if m.manual[n] {
				return true
			}
			i := strings.IndexByte(n, '.')
			if i < 0 {
				break
			}
			n = n[i+1:]
		}
		return false
	}
	for name != "" {
		if m.names[name] {
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

func isIPv4Literal(s string) bool {
	dots := 0
	for _, r := range s {
		switch {
		case r == '.':
			dots++
		case r < '0' || r > '9':
			return false
		}
	}
	return dots == 3
}

// RUDirectRouter — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы: «RU-адреса
// напрямую» (rudirect, docs/ru-direct.md кота 3). ApplyRUDirect ставит
// prefixes маршрутом через физический шлюз ПАЧКОЙ (тысячи блоков — не по
// команде на каждый), журнал — State.RUDirectRoutes одним commit ДО
// маршрутов; снимает Platform.Restore по журналу.
type RUDirectRouter interface {
	ApplyRUDirect(ctx context.Context, st *State, prefixes []netip.Prefix, commit func() error) error
}
