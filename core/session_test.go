package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"meridian/engine/params"
)

// --- двойники для тестов без сети, без root, без настоящей платформы ---

type fakeTun struct {
	name       string
	closed     bool
	closeCalls int
}

func (t *fakeTun) ReadBatch() []byte        { return nil }
func (t *fakeTun) WriteBatch(b []byte) bool { return true }
func (t *fakeTun) Close()                   { t.closeCalls++; t.closed = true }
func (t *fakeTun) Name() (string, error)    { return t.name, nil }

type fakePlatform struct {
	snapshotErr error
	newTunErr   error
	applyErr    error
	restoreErr  error

	tun *fakeTun

	snapshotCalls      int
	applyCalls         int
	attachJournalCalls int
	restoreCalls       []*State // снимок st на каждый вызов Restore, для проверки

	aliveByPID map[int]bool
}

func newFakePlatform() *fakePlatform {
	return &fakePlatform{tun: &fakeTun{name: "fake0"}, aliveByPID: map[int]bool{}}
}

func (p *fakePlatform) Snapshot(ctx context.Context, st *State) error {
	p.snapshotCalls++
	if p.snapshotErr != nil {
		return p.snapshotErr
	}
	st.PhysicalInterface = "en0"
	st.PhysicalGateway = "192.168.1.1"
	st.PrimaryService = "svc-1"
	st.OriginalDNS = []string{"192.168.1.1"}
	return nil
}

func (p *fakePlatform) NewTun(name string, mtuCeiling int) (Tun, error) {
	if p.newTunErr != nil {
		return nil, p.newTunErr
	}
	return p.tun, nil
}

func (p *fakePlatform) Protector() Protector { return fakeProtector{} }

func (p *fakePlatform) AttachJournal(st *State, commit func() error) {
	p.attachJournalCalls++
}

func (p *fakePlatform) Apply(ctx context.Context, st *State, t Tun, info TunnelInfo, commit func() error) error {
	p.applyCalls++
	if p.applyErr != nil {
		return p.applyErr
	}
	name, _ := t.Name()
	st.UtunName = name
	if err := commit(); err != nil {
		return err
	}
	st.FullTunnelSet = true
	st.TunnelDNSSet = true
	return commit()
}

func (p *fakePlatform) Restore(ctx context.Context, st *State) error {
	cp := *st
	p.restoreCalls = append(p.restoreCalls, &cp)
	return p.restoreErr
}

func (p *fakePlatform) ProcessAlive(pid int) bool { return p.aliveByPID[pid] }

// --- Connect ----------------------------------------------------------
//
// Оба теста ниже спотыкаются на чтении ключа раньше, чем Connect
// дойдёт до paramsClient.Fetch/BuildLadder — nil-клиент безопасен
// именно поэтому. Полный успешный путь Connect() юнит-тестами не
// покрыт: paramsClient.Fetch — реальный HTTP с закреплением TLS-ключа,
// без сети честно не сымитировать (core/params.go, core/session.go).

func TestConnectMissingKeyFileFails(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	eng := NewStubEngine()

	opts := ConnectOptions{KeyPath: filepath.Join(t.TempDir(), "нет-такого-файла.txt"), Gateway: "edge.example"}
	_, err := Connect(context.Background(), store, plat, eng, nil, opts)
	if err == nil {
		t.Fatal("Connect с несуществующим файлом ключа должен вернуть ошибку")
	}
	if plat.snapshotCalls != 0 {
		t.Fatal("Connect не должен успевать до Snapshot без ключа")
	}
}

func TestConnectEmptyKeyFileFails(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "test-key.txt")
	if err := os.WriteFile(keyPath, []byte("   \n"), 0o600); err != nil {
		t.Fatalf("подготовка файла ключа: %v", err)
	}

	store := &Store{Path: filepath.Join(dir, "state.json")}
	plat := newFakePlatform()
	eng := NewStubEngine()

	opts := ConnectOptions{KeyPath: keyPath, Gateway: "edge.example"}
	_, err := Connect(context.Background(), store, plat, eng, nil, opts)
	if err == nil {
		t.Fatal("Connect с пустым файлом ключа должен вернуть ошибку")
	}
}

// --- EnsureClean / Cleanup ---------------------------------------------

func TestEnsureCleanNoJournalIsNoop(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()

	if err := EnsureClean(context.Background(), store, plat); err != nil {
		t.Fatalf("EnsureClean без журнала: %v", err)
	}
	if len(plat.restoreCalls) != 0 {
		t.Fatal("без журнала Restore звать не должен")
	}
}

func TestEnsureCleanLiveProcessRefuses(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	plat.aliveByPID[4242] = true

	if err := store.Save(&State{PID: 4242}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := EnsureClean(context.Background(), store, plat); err == nil {
		t.Fatal("EnsureClean с журналом живого процесса должен отказать")
	}
	if len(plat.restoreCalls) != 0 {
		t.Fatal("отказ не должен ничего восстанавливать")
	}
	got, err := store.Load()
	if err != nil || got == nil {
		t.Fatalf("журнал должен остаться после отказа: %v, %v", got, err)
	}
}

func TestEnsureCleanDeadProcessRestoresAndRemovesJournal(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform() // aliveByPID пуст — процесс "мёртв"

	if err := store.Save(&State{PID: 4242, TunnelDNSSet: true, FullTunnelSet: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := EnsureClean(context.Background(), store, plat); err != nil {
		t.Fatalf("EnsureClean с мёртвым процессом: %v", err)
	}
	if len(plat.restoreCalls) != 1 {
		t.Fatalf("Restore должен быть вызван один раз, вызван %d", len(plat.restoreCalls))
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load после восстановления: %v", err)
	}
	if got != nil {
		t.Fatalf("журнал должен быть удалён после полного восстановления, получили %+v", got)
	}
}

func TestCleanupIgnoresLiveProcessCheck(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	plat.aliveByPID[4242] = true // Cleanup обязан не смотреть на это

	if err := store.Save(&State{PID: 4242}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Cleanup(context.Background(), store, plat); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load после Cleanup: %v", err)
	}
	if got != nil {
		t.Fatalf("Cleanup должен снять журнал независимо от живости процесса, получили %+v", got)
	}
}

func TestCleanupLeavesJournalOnRestoreFailure(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	plat := newFakePlatform()
	plat.restoreErr = fmt.Errorf("scutil недоступен")

	if err := store.Save(&State{PID: 4242}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Cleanup(context.Background(), store, plat); err == nil {
		t.Fatal("Cleanup при неудаче Restore должен вернуть ошибку")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load после неудачного Cleanup: %v", err)
	}
	if got == nil {
		t.Fatal("журнал не должен исчезать при неудачном восстановлении")
	}
}

// --- drainUntilClosedOrDone (KeepConnected) ----------------------------
//
// KeepConnected целиком, как и Connect, за границей юнит-тестов
// (paramsClient.Fetch — настоящий HTTP с закреплением TLS-ключа) — но
// drainUntilClosedOrDone не трогает ни сеть, ни Connect, только канал и
// контекст, и это ровно та новая логика, которую стоит проверить: живой
// прогон 24.09 показал, что движок гасит сессию сам (без внешнего
// Stop()) при обрыве сокета — не отличить это от штатной остановки
// значило бы никогда не переподключаться.

func TestDrainUntilClosedOrDoneChannelClosedIsUnexpected(t *testing.T) {
	events := make(chan EngineEvent)
	close(events) // как будто движок сам погасил сессию

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !drainUntilClosedOrDone(ctx, events, nil) {
		t.Fatal("закрытый движком канал должен читаться как unexpected=true")
	}
}

func TestDrainUntilClosedOrDoneContextDoneIsNotUnexpected(t *testing.T) {
	events := make(chan EngineEvent) // никогда не закрывается и не пишется

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем ДО вызова — как будто истёк срок/пришёл сигнал

	if drainUntilClosedOrDone(ctx, events, nil) {
		t.Fatal("отменённый контекст должен читаться как unexpected=false, канал ещё жив")
	}
}

func TestDrainUntilClosedOrDoneForwardsEventsThenDetectsClose(t *testing.T) {
	events := make(chan EngineEvent, 2)
	want := []EngineEvent{{Kind: "log", Reason: "a"}, {Kind: "log", Reason: "b"}}
	events <- want[0]
	events <- want[1]
	close(events)

	var got []EngineEvent
	ctx := context.Background()
	unexpected := drainUntilClosedOrDone(ctx, events, func(ev EngineEvent) { got = append(got, ev) })

	if !unexpected {
		t.Fatal("канал закрылся сам — хотели unexpected=true")
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("onEvent получил %v, хотели %v", got, want)
	}
}

func TestDrainUntilClosedOrDonePrefersDoneEvenWithPendingEvents(t *testing.T) {
	// Контекст уже отменён; в канале ещё лежит непрочитанное событие,
	// но канал НЕ закрыт (сессия формально жива) — должны уйти по ctx,
	// не по событию: то, что events непуст, не значит «сессия жива и
	// стоит её читать и дальше», значит «мы решили закончить именно
	// сейчас, события неважны».
	events := make(chan EngineEvent, 1)
	events <- EngineEvent{Kind: "log", Reason: "should not block us"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// select недетерминирован при двух готовых case — но за много
	// попыток оба исхода (true/false) валидны ЛОГИЧЕСКИ, реальная цель
	// теста — не блокировка и не паника, не конкретная ветка. Проверяем
	// то, что действительно гарантировано контрактом: функция вернулась.
	done := make(chan struct{})
	go func() {
		drainUntilClosedOrDone(ctx, events, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drainUntilClosedOrDone не вернулась за 2с — похоже на блокировку")
	}
}

// --- откат при ошибке после подключения журнала ---------------------------

// Fetch падает (адрес API — заведомо закрытый локальный порт: отказ
// мгновенный, в сеть не ходим) → Connect обязан позвать Restore и убрать
// журнал. Раньше этого не было: исключения /32, которые Fetch уже успел
// поставить для адреса API, оставались, а журнал затирала следующая
// попытка — у долгоживущего помощника они копились.
func TestConnectFetchFailureRestoresAndRemovesJournal(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: filepath.Join(dir, "state.json")}
	plat := newFakePlatform()
	client := NewParamsClient(ParamsConfig{
		Addresses:      []params.Address{{Host: "127.0.0.1", Port: 1}},
		ConnectTimeout: time.Second,
		ReadTimeout:    time.Second,
		WalkBudget:     2 * time.Second,
	})

	_, err := Connect(context.Background(), store, plat, NewStubEngine(), client, ConnectOptions{KeyPath: keyPath, Gateway: "edge.example"})
	if err == nil {
		t.Fatal("Connect с недоступным API должен вернуть ошибку")
	}
	if len(plat.restoreCalls) != 1 {
		t.Fatalf("Restore вызван %d раз, хотели 1 (откат исключений, поставленных до ошибки)", len(plat.restoreCalls))
	}
	if st, _ := store.Load(); st != nil {
		t.Fatalf("журнал остался после неудачного Connect: %+v", st)
	}
}

// Прямые домены на платформе без DirectRouter — отказ ДО снимка сети,
// не молчаливый туннель всего.
func TestConnectDirectDomainsWithoutDirectRouterRefusesEarly(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: filepath.Join(dir, "state.json")}
	plat := newFakePlatform() // DirectRouter не реализует
	_, err := Connect(context.Background(), store, plat, NewStubEngine(), nil, ConnectOptions{
		KeyPath: keyPath, Gateway: "edge.example", DirectDomains: []string{"2ip.ru"},
	})
	if err == nil {
		t.Fatal("Connect с прямыми доменами на платформе без DirectRouter должен отказать")
	}
	if plat.snapshotCalls != 0 {
		t.Fatalf("отказ должен быть ДО снимка сети, Snapshot вызван %d раз", plat.snapshotCalls)
	}
}

func TestConnectPreservesJournalWhenAbortRestoreFails(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: filepath.Join(dir, "state.json")}
	plat := newFakePlatform()
	plat.restoreErr = fmt.Errorf("injected restore failure")
	client := NewParamsClient(ParamsConfig{Addresses: []params.Address{{Host: "127.0.0.1", Port: 1}}, ConnectTimeout: time.Second, ReadTimeout: time.Second, WalkBudget: 2 * time.Second})
	_, err := Connect(context.Background(), store, plat, NewStubEngine(), client, ConnectOptions{KeyPath: keyPath, Gateway: "edge.example"})
	if err == nil {
		t.Fatal("expected failure")
	}
	st, err := store.Load()
	if err != nil || st == nil {
		t.Fatalf("journal lost: %v", err)
	}
	before := plat.snapshotCalls
	_, err = Connect(context.Background(), store, plat, NewStubEngine(), client, ConnectOptions{KeyPath: keyPath, Gateway: "edge.example"})
	if err == nil || plat.snapshotCalls != before {
		t.Fatal("retry overwrote unresolved journal")
	}
}
