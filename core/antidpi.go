package core

// AntiDPIGroups — переключатели по группам сайтов для обхода
// DPI-блокировок в чёрном режиме (core.TunnelRouter): трафик МИМО списка
// человека идёт напрямую через провайдера — там его и блокируют.
// Решение владельца 27.09.2026, разбор и данные кота 1 (движок
// meridian-antidpi, роутер) — Windows-only пока (WinDivert,
// platform/windows/antidpi). Без выбора техники обхода человеку — она
// внутренняя, как в панели роутера.
type AntiDPIGroups struct {
	YouTube    bool
	Discord    bool
	Cloudflare bool
	Social     bool
	Other      bool
}

func (g AntiDPIGroups) any() bool {
	return g.YouTube || g.Discord || g.Cloudflare || g.Social || g.Other
}

// AntiDPIRouter — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы (как DirectRouter/
// RUDirectRouter/TunnelRouter): платформа без него при непустых группах →
// Connect отказывает вслух, не притворяется, что обход работает. Действует
// ТОЛЬКО в чёрном режиме (TunnelOnly) — трафик белого режима и так идёт
// через ВК-релей, DPI по провайдеру его не видит.
type AntiDPIRouter interface {
	// ApplyAntiDPI — поднять перехват после того, как физический путь уже
	// известен платформе (зовётся ПОСЛЕ apply/ApplyTunnelOnly, st — тот же
	// журнал, что у ApplyDirect/ApplyRUDirect, physicalFromJournal читает
	// его так же). Ошибка — НЕ обрывает подключение целиком (тот же
	// принцип, что у RUDirect): без обхода DPI туннель по списку
	// по-прежнему работает, это лучше, чем вообще ничего.
	ApplyAntiDPI(st *State, groups AntiDPIGroups) error
	// StopAntiDPI — снять перехват; вызывается ДАЖЕ если ApplyAntiDPI не
	// звался (идемпотентно, как Platform.Restore) — Session.Disconnect
	// зовёт его безусловно при antiDPI != nil.
	StopAntiDPI()
}
