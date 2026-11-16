package core

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// recordedControlMessage/fakeControlSend — записывают всё, что послал
// RunControlConnect/buildControlCallbacks, без транспорта (ни
// Unix-сокета, ни именованного канала — эта часть логики их не
// касается вообще).
type recordedControlMessage struct {
	typ     ControlMessageType
	payload any
}

type fakeControlSend struct {
	sent []recordedControlMessage
}

func (f *fakeControlSend) send(typ ControlMessageType, payload any) error {
	f.sent = append(f.sent, recordedControlMessage{typ: typ, payload: payload})
	return nil
}

// --- buildControlCallbacks: чистая логика перевода, без сети -----------

func TestBuildControlCallbacksOnConnectedSendsFinalState(t *testing.T) {
	fs := &fakeControlSend{}
	onConnected, _, _, _ := buildControlCallbacks(fs.send)

	info := TunnelInfo{IP: "10.77.77.5", PrefixLen: 16, Gateway: "10.77.77.1", MTU: 1200}
	onConnected(&Session{TunnelInfo: info})

	if len(fs.sent) != 1 {
		t.Fatalf("сообщений отправлено = %d, хотели 1", len(fs.sent))
	}
	got, ok := fs.sent[0].payload.(ControlStatePayload)
	if !ok {
		t.Fatalf("payload = %T, хотели ControlStatePayload", fs.sent[0].payload)
	}
	if fs.sent[0].typ != ControlState {
		t.Fatalf("Type = %q, хотели %q", fs.sent[0].typ, ControlState)
	}
	if !got.Connected {
		t.Fatal("onConnected должен слать Connected=true")
	}
	if got.TunnelInfo == nil || *got.TunnelInfo != info {
		t.Fatalf("TunnelInfo = %+v, хотели %+v", got.TunnelInfo, info)
	}
	if got.Reconnects != 0 {
		t.Fatalf("Reconnects на первом подключении = %d, хотели 0", got.Reconnects)
	}
}

// Победившая ступень уходит в интерфейс: по ней (а не по настройке белого
// режима) планета становится белой на релее (решение владельца 26.09).
func TestBuildControlCallbacksOnConnectedSendsWinner(t *testing.T) {
	fs := &fakeControlSend{}
	onConnected, _, _, _ := buildControlCallbacks(fs.send)
	onConnected(&Session{winner: "R"})
	got, ok := fs.sent[0].payload.(ControlStatePayload)
	if !ok || got.Winner != "R" {
		t.Fatalf("payload = %+v, хотели Winner=\"R\"", fs.sent[0].payload)
	}
}

func TestBuildControlCallbacksOnEventForwardsEngineEventAsIs(t *testing.T) {
	fs := &fakeControlSend{}
	_, onEvent, _, _ := buildControlCallbacks(fs.send)

	ev := EngineEvent{Kind: "log", Reason: "тест"}
	onEvent(ev)

	if len(fs.sent) != 1 {
		t.Fatalf("сообщений отправлено = %d, хотели 1", len(fs.sent))
	}
	if fs.sent[0].typ != ControlEvent {
		t.Fatalf("Type = %q, хотели %q", fs.sent[0].typ, ControlEvent)
	}
	got, ok := fs.sent[0].payload.(EngineEvent)
	if !ok {
		t.Fatalf("payload = %T, хотели EngineEvent (как есть, без обёртки)", fs.sent[0].payload)
	}
	if got != ev {
		t.Fatalf("EngineEvent изменился при пересылке: было %+v, стало %+v", ev, got)
	}
}

func TestBuildControlCallbacksOnReconnectSendsTransitionalNotState(t *testing.T) {
	fs := &fakeControlSend{}
	_, _, onReconnect, _ := buildControlCallbacks(fs.send)

	onReconnect(3)

	if len(fs.sent) != 1 {
		t.Fatalf("сообщений отправлено = %d, хотели 1", len(fs.sent))
	}
	if fs.sent[0].typ != ControlReconnecting {
		t.Fatalf("Type = %q, хотели %q (НЕ %q — интерфейс должен отличать переходный разрыв от окончательного)",
			fs.sent[0].typ, ControlReconnecting, ControlState)
	}
	got, ok := fs.sent[0].payload.(ControlReconnectingPayload)
	if !ok {
		t.Fatalf("payload = %T, хотели ControlReconnectingPayload", fs.sent[0].payload)
	}
	if got.Attempt != 3 {
		t.Fatalf("Attempt = %d, хотели 3", got.Attempt)
	}
}

// TestBuildControlCallbacksReconnectsCounterThreadsIntoNextConnected —
// сценарий кота 6: onReconnect(n) → следующий onConnected должен нести
// ТОТ ЖЕ n в Reconnects, не 0 и не что-то ещё — общий счётчик через
// reconnects (указатель), не отдельная копия у каждого колбэка.
func TestBuildControlCallbacksReconnectsCounterThreadsIntoNextConnected(t *testing.T) {
	fs := &fakeControlSend{}
	onConnected, _, onReconnect, reconnects := buildControlCallbacks(fs.send)

	onConnected(&Session{}) // первое подключение: Reconnects=0
	onReconnect(1)          // непреднамеренный обрыв — переходное сообщение
	onConnected(&Session{}) // восстановились — второй ControlState должен нести Reconnects=1

	if len(fs.sent) != 3 {
		t.Fatalf("сообщений отправлено = %d, хотели 3", len(fs.sent))
	}
	first := fs.sent[0].payload.(ControlStatePayload)
	if first.Reconnects != 0 {
		t.Fatalf("первый ControlState.Reconnects = %d, хотели 0", first.Reconnects)
	}
	if fs.sent[1].typ != ControlReconnecting {
		t.Fatalf("второе сообщение = %q, хотели %q", fs.sent[1].typ, ControlReconnecting)
	}
	second := fs.sent[2].payload.(ControlStatePayload)
	if second.Reconnects != 1 {
		t.Fatalf("второй ControlState.Reconnects = %d, хотели 1 (тот же счётчик, что в onReconnect)", second.Reconnects)
	}
	if *reconnects != 1 {
		t.Fatalf("итоговый reconnects = %d, хотели 1", *reconnects)
	}
}

// --- RunControlConnect: хвост (err==nil/err!=nil), без сети -------------
//
// err!=nil проверяем через отсутствующий файл ключа — тот же приём,
// что TestConnectMissingKeyFileFails, отказ происходит ДО первого
// сетевого похода (resolveKey/readKeyFile), paramsClient=nil не
// трогается. err==nil (штатная остановка) требует хотя бы одного
// успешного Connect — та же сетевая граница, что у KeepConnected
// целиком (core/session.go, drainUntilClosedOrDone) — не тестируется
// здесь отдельно по той же причине, только буквально одна лишняя
// send() после неё, разобрана визуально в RunControlConnect.

func TestRunControlConnectSendsErrorNotStateOnFailure(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	eng := NewStubEngine()
	fs := &fakeControlSend{}

	opts := ConnectOptions{
		KeyPath: filepath.Join(t.TempDir(), "нет-такого-файла.txt"),
		Gateway: "edge.example",
	}
	err := RunControlConnect(context.Background(), store, plat, eng, nil, opts, fs.send)
	if err == nil {
		t.Fatal("RunControlConnect с несуществующим файлом ключа должен вернуть ошибку")
	}
	if len(fs.sent) != 1 {
		t.Fatalf("сообщений отправлено = %d, хотели 1 (ControlError)", len(fs.sent))
	}
	if fs.sent[0].typ != ControlError {
		t.Fatalf("Type = %q, хотели %q — интерфейс не должен принять сбой за штатное отключение", fs.sent[0].typ, ControlError)
	}
	got, ok := fs.sent[0].payload.(ControlErrorPayload)
	if !ok {
		t.Fatalf("payload = %T, хотели ControlErrorPayload", fs.sent[0].payload)
	}
	if got.Code != ControlErrConnectFailed {
		t.Fatalf("Code = %q, хотели %q", got.Code, ControlErrConnectFailed)
	}
	if got.Message == "" {
		t.Fatal("Message пуст — интерфейсу нечего показать/залогировать")
	}
}

// Код причины движка доезжает через цепочку %w (как её строят Connect и
// KeepConnected) до errors.As в RunControlConnect. Полный путь через
// RunControlConnect требует /v1/params — настоящую сеть, как и остальной
// KeepConnected; здесь — сам перенос.
func TestEngineStartErrorSurvivesWrapping(t *testing.T) {
	err := fmt.Errorf("KeepConnected: попытка 1: %w", fmt.Errorf("Connect: движок: %w",
		&EngineStartError{Failure: "blocked-nolinks", Err: errors.New("лестница не поднялась")}))
	var se *EngineStartError
	if !errors.As(err, &se) || se.Failure != "blocked-nolinks" {
		t.Fatalf("errors.As не достал код причины из цепочки: %v", err)
	}
	if err.Error() == "" || !errors.Is(err, se.Err) {
		t.Fatalf("исходная ошибка движка потерялась: %v", err)
	}
}

func TestSystemDNSFailureReachesControlProtocol(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	plat.snapshotErr = fmt.Errorf("private detail: %w", ErrSystemDNSUnavailable)
	sent := &fakeControlSend{}
	err := RunControlConnect(context.Background(), store, plat, NewStubEngine(), nil, ConnectOptions{KeyMaterial: "synthetic-key", Gateway: "192.0.2.1"}, sent.send)
	if err == nil || len(sent.sent) != 1 || sent.sent[0].typ != ControlError {
		t.Fatalf("expected control failure: %v %+v", err, sent.sent)
	}
	got := sent.sent[0].payload.(ControlErrorPayload)
	if got.Failure != FailureSystemDNSUnavailable {
		t.Fatalf("missing DNS classification: %+v", got)
	}
	if st, loadErr := store.Load(); loadErr != nil || st != nil {
		t.Fatalf("snapshot error must not persist network state: %v", loadErr)
	}
}
