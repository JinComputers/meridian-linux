package core

import (
	"context"
	"testing"
)

type fakeFlow struct{}

func (fakeFlow) ReadBatch() []byte        { return nil }
func (fakeFlow) WriteBatch(b []byte) bool { return true }
func (fakeFlow) Close()                   {}

type fakeProtector struct{}

func (fakeProtector) Protect(fd int32) bool { return true }

func TestStubEngineStartRequiresTunAndProtector(t *testing.T) {
	e := NewStubEngine()

	cfgNoProtector := EngineConfig{}
	if _, _, err := e.Start(context.Background(), cfgNoProtector, fakeFlow{}); err == nil {
		t.Fatal("Start(…) без Protector в конфиге должен вернуть ошибку")
	}

	cfg := EngineConfig{Protector: fakeProtector{}}
	if _, _, err := e.Start(context.Background(), cfg, nil); err == nil {
		t.Fatal("Start(nil tun, …) должен вернуть ошибку")
	}
}

func TestStubEngineStartReturnsHandleAndEvents(t *testing.T) {
	want := []EngineEvent{{Kind: "log", Reason: "тест"}}
	e := &StubEngine{Events: want}
	cfg := EngineConfig{Protector: fakeProtector{}}

	handle, events, err := e.Start(context.Background(), cfg, fakeFlow{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if handle == nil {
		t.Fatal("Start() вернул nil handle без ошибки")
	}

	select {
	case ev := <-events:
		if ev != want[0] {
			t.Fatalf("событие = %+v, хотели %+v", ev, want[0])
		}
	default:
		t.Fatal("канал событий пуст, хотя Events был задан")
	}
}

func TestStubEngineHandleInfoReturnsConfiguredTunnelInfo(t *testing.T) {
	want := TunnelInfo{IP: "10.77.77.5", PrefixLen: 16, MaskHex: "ffff0000", Gateway: "10.77.77.1", MTU: 1100}
	e := &StubEngine{Info: want}
	cfg := EngineConfig{Protector: fakeProtector{}}

	handle, _, err := e.Start(context.Background(), cfg, fakeFlow{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := handle.Info(); got != want {
		t.Fatalf("Info() = %+v, хотели %+v", got, want)
	}
}

func TestStubEngineDoubleStartFails(t *testing.T) {
	e := NewStubEngine()
	cfg := EngineConfig{Protector: fakeProtector{}}

	if _, _, err := e.Start(context.Background(), cfg, fakeFlow{}); err != nil {
		t.Fatalf("первый Start: %v", err)
	}
	if _, _, err := e.Start(context.Background(), cfg, fakeFlow{}); err == nil {
		t.Fatal("второй Start без Stop должен вернуть ошибку (одна сессия на процесс)")
	}
}

func TestStubEngineStopClosesEventsAndAllowsRestart(t *testing.T) {
	e := NewStubEngine()
	cfg := EngineConfig{Protector: fakeProtector{}}

	handle, events, err := e.Start(context.Background(), cfg, fakeFlow{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	handle.Stop()
	handle.Stop() // идемпотентность — не должен паниковать на закрытом канале

	if _, ok := <-events; ok {
		t.Fatal("канал событий должен быть закрыт после Stop()")
	}

	if _, _, err := e.Start(context.Background(), cfg, fakeFlow{}); err != nil {
		t.Fatalf("Start после Stop должен пройти: %v", err)
	}
}
