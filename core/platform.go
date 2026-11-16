package core

import "context"

// Tun — то, что core просит у платформы поверх самого PacketFlow: знать
// собственное имя устройства (utunN на macOS, LUID/имя адаптера на
// Windows) — нужно и для журнала, и чтобы поставить на него маршруты.
type Tun interface {
	PacketFlow
	Name() (string, error)
}

// Platform — всё, что core.Connect/EnsureClean/Cleanup просят у
// платформы. Общий порядок подключения (план этапа 1, разделы 1, 4, 5)
// живёт в core/ и платформенно-нейтрален; конкретное «как» — за
// platform/darwin и platform/windows, каждая реализует этот интерфейс
// целиком.
type Platform interface {
	// Snapshot — исходное состояние сети, снимается ДО любых изменений
	// (план, раздел 1, п. 3): физический шлюз/интерфейс, идентификатор
	// сетевой службы/адаптера для DNS (macOS — UUID службы, Windows —
	// LUID/GUID) и исходный DNS. Заполняет соответствующие поля st —
	// PID/StartedAt проставляет вызывающий (core.Connect), не Snapshot.
	Snapshot(ctx context.Context, st *State) error

	// NewTun поднимает TUN-интерфейс движка. mtuCeiling — потолок
	// буферов (обычно tunMTU=1200 движка): настоящий MTU неизвестен до
	// TunnelInfo, а TUN нужно поднять раньше, чем он станет известен
	// (desktop.Start принимает PacketFlow параметром, до Connect внутри
	// себя) — см. план, обсуждение порядка MTU.
	NewTun(name string, mtuCeiling int) (Tun, error)

	// Protector — привязка сокетов движка к физическому интерфейсу
	// (engine.Protector), общая на всё время жизни Platform.
	Protector() Protector

	// AttachJournal подключает журнал к Protector ДО запуска движка
	// (engine-v1.2.0, кот3): движок зовёт необязательный ExcludeHost
	// уже во время рукопожатия, раньше, чем core.Connect дойдёт до
	// Apply — платформе нужно знать, куда журналировать новые
	// исключения (core.State.ExceptionTargets), до того как они начнут
	// приходить. Платформы без Protector.ExcludeHost (решение владельца
	// 24.09: там, где интерфейсной привязки достаточно самой по себе)
	// реализуют этот метод пустым — core.Connect зовёт его безусловно.
	AttachJournal(st *State, commit func() error)

	// Apply настраивает уже поднятый TUN (адрес/маска/MTU из info),
	// полный туннель и DNS. Помечает в st, что именно поставлено
	// (UtunName/FullTunnelSet/TunnelDNSSet/…), чтобы Restore знал, что
	// снимать. commit сохраняет ТЕКУЩЕЕ содержимое st в журнал — Apply
	// обязан звать его после каждого изменения st и ДО следующего
	// действия в системе (план, раздел 1, п. 4: «сначала запись в
	// журнал, потом действие» — это правило не только для core.Connect,
	// но и для каждого шага внутри Apply, иначе падение посреди Apply
	// оставит журнал не соответствующим тому, что реально поставлено).
	// На Windows, где настройка атомарна поверх LUID адаптера, commit
	// можно звать один раз в конце — платформа решает сама.
	Apply(ctx context.Context, st *State, t Tun, info TunnelInfo, commit func() error) error

	// Restore возвращает сеть к состоянию st. Идемпотентен и не
	// останавливается на первой ошибке (план, раздел 4, п. 4) — один и
	// тот же метод используется и при штатном отключении (Disconnect),
	// и при восстановлении после падения/на повторном запуске
	// (EnsureClean/Cleanup).
	Restore(ctx context.Context, st *State) error

	// ProcessAlive — жив ли процесс с этим pid (план, раздел 5: защита
	// от повторного входа). Платформенно: на Windows и Unix это разные
	// системные вызовы, core сам не выбирает который.
	ProcessAlive(pid int) bool
}
