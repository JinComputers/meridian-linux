package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StubEngine — заглушка EngineStarter той же формы, что у RealEngine
// (engine-v1.0.0, engine/desktop, кот 3). Сеть не поднимает, пакеты не
// шифрует — нужна только чтобы проверить остальной код прототипа
// (маршруты, DNS, журнал) без настоящей сети, до её появления и в
// тестах после. Не использовать за пределами прототипа этапа 1.
type StubEngine struct {
	// Events — что будет отдано вызывающему через канал событий сразу
	// при Start (в отличие от настоящего движка, который шлёт их по
	// ходу сессии) — для прототипа этого достаточно.
	Events []EngineEvent
	// Info — что вернёт Handle.Info(), как будто это ответ шлюза на AUTH.
	Info TunnelInfo
	// Winner — что вернёт Handle.Winner(), как будто это буква
	// победившей ступени (core.EngineHandle.Winner). Пусто по умолчанию —
	// тесты, которым память путей безразлична, ничего не задают.
	Winner string
	// Probe* — что вернёт Handle.SpeedProbe() (замер скорости).
	ProbeState int32
	ProbeKbps  int64
	ProbeLoss  int32

	// SimulateUnexpectedStop, если > 0 — сама закрывает канал событий
	// через это время ПОСЛЕ Start, БЕЗ внешнего Stop(): как настоящий
	// движок гасит сессию сам при ошибке (engine/engine.go: «приложение
	// обязано переподключаться само», channel закрывается после
	// «stopped» независимо от того, кто его вызвал). Нужна тестам
	// core.KeepConnected — StubEngine без этого поля различить
	// «мы остановили» и «движок сам умер» нечем, реальный движок эту
	// разницу тоже не выражает по-другому, кроме факта закрытия канала
	// БЕЗ нашего Stop().
	SimulateUnexpectedStop time.Duration

	mu      sync.Mutex
	started bool
}

func NewStubEngine() *StubEngine {
	return &StubEngine{}
}

// stubHandle — EngineHandle заглушки: Stop() идемпотентен и закрывает
// канал событий, как это делает настоящий desktop.Handle после
// "stopped" (desktop.go: OnStopped закрывает канал). stop сам уже
// обёрнут в один общий sync.Once с возможным SimulateUnexpectedStop —
// кто бы ни закрыл канал первым (мы через Stop, или заглушка сама через
// SimulateUnexpectedStop), второй вызов молча ничего не делает.
type stubHandle struct {
	probeState int32
	probeKbps  int64
	probeLoss  int32
	stop       func()
	info       TunnelInfo
	winner     string
}

func (h stubHandle) Stop() {
	h.stop()
}

func (h stubHandle) Info() TunnelInfo {
	return h.info
}

func (h stubHandle) SpeedProbe() (int32, int64, int32) {
	return h.probeState, h.probeKbps, h.probeLoss
}

func (h stubHandle) Winner() string {
	return h.winner
}

func (e *StubEngine) Start(ctx context.Context, cfg EngineConfig, tun PacketFlow) (EngineHandle, <-chan EngineEvent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.started {
		return nil, nil, fmt.Errorf("StubEngine.Start: уже подключено (одна сессия на процесс, как у настоящего движка)")
	}
	if tun == nil {
		return nil, nil, fmt.Errorf("StubEngine.Start: tun == nil")
	}
	if cfg.Protector == nil {
		return nil, nil, fmt.Errorf("StubEngine.Start: cfg.Protector == nil")
	}
	e.started = true

	events := make(chan EngineEvent, len(e.Events))
	for _, ev := range e.Events {
		events <- ev
	}

	var once sync.Once
	closeFn := func() {
		e.mu.Lock()
		e.started = false
		e.mu.Unlock()
		close(events)
	}
	handle := stubHandle{
		info:       e.Info,
		winner:     e.Winner,
		probeState: e.ProbeState,
		probeKbps:  e.ProbeKbps,
		probeLoss:  e.ProbeLoss,
		stop:       func() { once.Do(closeFn) },
	}

	if e.SimulateUnexpectedStop > 0 {
		go func() {
			time.Sleep(e.SimulateUnexpectedStop)
			once.Do(closeFn) // не сработает, если внешний Stop() уже опередил
		}()
	}

	return handle, events, nil
}
