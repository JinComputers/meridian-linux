package core

import (
	"context"

	"meridian/engine"
	"meridian/engine/desktop"
)

// Реэкспорт того, что реально определено в engine-v1.0.0 (тег кота 3,
// подпакет engine/desktop — фасад специально для настольных клиентов,
// не проходящих через gomobile). Это НЕ предположение по форме — сверено
// с исходником 24.09.2026 (desktop.go, engine.go, flowconn.go). Алиасы,
// не копии: остальной код core/ и platform/darwin не пишет свою версию
// этих типов, только использует.
type (
	EngineConfig = desktop.Config
	EngineEvent  = desktop.Event
	EngineStatus = desktop.Status
	// TunnelInfo — то, что выдал шлюз в ответе на AUTH: всё нужное, чтобы
	// настроить интерфейс TUN (engine-v1.1.0, кот 3, desktopinfo.go).
	// Известно сразу после Start, до первого пакета.
	TunnelInfo = desktop.TunnelInfo

	// Protector — движок зовёт Protect(fd) для КАЖДОГО своего сокета ДО
	// bind, кроме системного DNS-резолва до подъёма туннеля и резолва
	// имён внутри Dialer (план этапа 1, раздел 3; подтверждено кот3
	// 24.09 постатейно по коду: race.go, streamdial.go, relay.go,
	// vkcreds.go, engine/params.Config.Protector — тот же интерфейс).
	// Реализация на macOS — platform/darwin.SocketProtector (IP_BOUND_IF).
	Protector = engine.Protector
	// PacketFlow — куда движок пишет пакеты из туннеля и откуда читает
	// пакеты в туннель (ReadBatch/WriteBatch/Close, engine/flowconn.go).
	// На macOS — обёртка над golang.zx2c4.com/wireguard/tun.Device.
	PacketFlow    = engine.PacketFlow
	NetworkGuard  = engine.NetworkGuard
	CaptchaSolver = engine.CaptchaSolver
	Ladder        = engine.Ladder
)

// EngineHandle — то немногое, что core берёт от desktop.Handle: остановить
// сессию и узнать параметры туннеля. Интерфейс, а не алиас на
// *desktop.Handle: тот — конкретный тип без экспортированного
// конструктора (создаётся только внутри desktop.Start), тестовому
// двойнику (StubEngine) его не собрать. *desktop.Handle реализует этот
// интерфейс структурно, без изменений в коде кота 3.
type EngineHandle interface {
	Stop()
	// Info — параметры туннеля от шлюза (engine-v1.1.0): адрес, префикс,
	// маска хексом, шлюз, MTU. Настолько же надёжно, насколько Start уже
	// вернулась без ошибки — блокер плана этапа 1 (раздел 9) снят.
	Info() TunnelInfo
	// Winner — буква ступени, которой поднялась ЭТА сессия ("A", "R" —
	// релей, и т.д., ladder.go кота 3: relayName="R"). Нужна для памяти
	// путей (core.PathMemory): по ней Connect отличает UDP-прямую от
	// потока/релея — запоминаем только прямую (core/pathmemory.go,
	// комментарий у вызова). *desktop.Handle сам буквы не хранит (только
	// движок целиком, через desktop.CurrentStatus/engine.Winner) —
	// realHandle ниже подмешивает её тем же способом, каким Handle.Info()
	// уже читает движок; кода кота 3 это не трогает.
	Winner() string
	// SpeedProbe — замер скорости поднятого туннеля (engine/speedprobe.go,
	// engine-v1.7.0): state — SpeedProbeNone/Running/Done/Silent, kbps и
	// lossPct — итог при Done. Включён в RealEngine.Start (SetSpeedProbe);
	// шлёт ~150 КБ в каждую сторону один раз за сессию (engine-v1.10.9).
	SpeedProbe() (state int32, kbps int64, lossPct int32)
}

// EngineStarter — единственное, что core берёт от движка: поднять сессию
// и получить Handle с каналом событий. Абстракция нужна для тестирования
// кода вокруг движка (маршруты/DNS/журнал) без настоящей сети — в тестах
// подменяется StubEngine, в бою — RealEngine.
//
// Получение /v1/params сюда не входит: это engine/params кота 3
// (Client.Fetch), вызывается отдельно, до сборки EngineConfig.Ladder.
type EngineStarter interface {
	Start(ctx context.Context, cfg EngineConfig, tun PacketFlow) (EngineHandle, <-chan EngineEvent, error)
}

// setRelayPrimaryDion — основное звено релея в движке (engine-v1.10.0,
// кот 3): true — креды сначала из комнат DION, при отказе VK; false — VK
// первым. Значение в движке глобальное, поэтому ставится перед КАЖДЫМ
// подключением, и выбор VK сбрасывает прежний DION. Переменная — чтобы
// тесты Connect не трогали настоящий движок.
var setRelayPrimaryDion = engine.SetRelayPrimaryDion

// RealEngine — боевая реализация EngineStarter поверх engine/desktop.Start.
type RealEngine struct{}

func (RealEngine) Start(ctx context.Context, cfg EngineConfig, tun PacketFlow) (EngineHandle, <-chan EngineEvent, error) {
	// Замер скорости включён: порт Android 0.1.254 с тремя защитами от петли
	// 27.09 (см. pathmemory.go: slowTTL, speedSwitchGap, решение без
	// сравнения с релеем).
	engine.SetSpeedProbe(true)
	h, events, err := desktop.Start(ctx, cfg, tun)
	if err != nil {
		// Код причины — СРАЗУ здесь (кот 3: значение глобальное, сбрасывает
		// его только следующий engine.Connect). Позже, в RunControlConnect,
		// спрашивать нельзя: если Connect сорвался до движка (/v1/params,
		// TUN), движок не звался и код остался бы от прошлой попытки.
		err = &EngineStartError{Failure: engine.FailureKind(), Err: err}
	}
	if h == nil {
		// Без этой проверки типизированный nil *desktop.Handle,
		// завёрнутый в интерфейс EngineHandle, был бы формально
		// НЕ-nil — классическая ловушка Go (nil-указатель в интерфейсе).
		return nil, events, err
	}
	return realHandle{h}, events, err
}

// realHandle подмешивает Winner() к *desktop.Handle (см. комментарий у
// EngineHandle.Winner) — сам Handle этот метод не экспортирует, буква
// победившей ступени доступна только пакетным геттером desktop.
// CurrentStatus/engine.Winner (ОДНА СЕССИЯ НА ПРОЦЕСС — как раз эта, раз
// realHandle получен только что от успешного Start).
type realHandle struct{ h *desktop.Handle }

func (r realHandle) Stop()            { r.h.Stop() }
func (r realHandle) Info() TunnelInfo { return r.h.Info() }
func (r realHandle) Winner() string   { return desktop.CurrentStatus().Winner }
func (r realHandle) SpeedProbe() (int32, int64, int32) {
	return engine.SpeedProbeState(), engine.SpeedProbeKbps(), engine.SpeedProbeLossPct()
}

// EngineStartError — отказ desktop.Start вместе с кодом причины движка
// (engine.FailureKind: "blocked-nolinks", "silent", ...; пусто — движок
// причину не назвал). RunControlConnect достаёт его через errors.As и
// кладёт в ControlErrorPayload.Failure — по нему интерфейс выбирает текст
// для человека (docs/desktop-design.md кота 3, раздел 4).
type EngineStartError struct {
	Failure string
	Err     error
}

func (e *EngineStartError) Error() string { return e.Err.Error() }
func (e *EngineStartError) Unwrap() error { return e.Err }
