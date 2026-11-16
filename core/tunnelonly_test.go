package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDomainMatcher(t *testing.T) {
	m := NewDomainMatcher([]string{"youtube.com", "91.108.4.0/22", "1.2.3.4", "googlevideo.com"})
	for name, want := range map[string]bool{
		"youtube.com":                  true,
		"www.youtube.com.":             true,
		"rr1---sn-abc.googlevideo.com": true,
		"notyoutube.com":               false,
		"youtube.com.evil.ru":          false,
		"1.2.3.4":                      false,
		"com":                          false,
	} {
		if got := m.Match(name); got != want {
			t.Errorf("Match(%q) = %v, хотели %v", name, got, want)
		}
	}
	if !NewDomainMatcher([]string{"1.2.3.4"}).Empty() || NewDomainMatcher(nil).Match("youtube.com") {
		t.Error("список без имён ничего не ловит")
	}
}

// Чёрный режим на платформе без TunnelRouter — отказ ДО снимка сети;
// вместе с прямыми доменами — тоже.
func TestConnectTunnelOnlyRefusesEarly(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: filepath.Join(dir, "state.json")}
	for _, opts := range []ConnectOptions{
		{KeyPath: keyPath, Gateway: "edge.example", TunnelOnly: true, TunnelDomains: []string{"youtube.com"}},
		{KeyPath: keyPath, Gateway: "edge.example", TunnelOnly: true, DirectDomains: []string{"2ip.ru"}},
	} {
		plat := newFakePlatform()
		if _, err := Connect(context.Background(), store, plat, NewStubEngine(), nil, opts); err == nil {
			t.Fatalf("Connect(%+v) должен отказать", opts)
		}
		if plat.snapshotCalls != 0 {
			t.Fatalf("отказ должен быть ДО снимка сети, Snapshot вызван %d раз", plat.snapshotCalls)
		}
	}
}

// «RU-адреса напрямую» на платформе без RUDirectRouter — отказ ДО снимка.
func TestConnectRUDirectRefusesEarly(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: filepath.Join(dir, "state.json")}
	plat := newFakePlatform()
	opts := ConnectOptions{KeyPath: keyPath, Gateway: "edge.example", RUDirect: true}
	if _, err := Connect(context.Background(), store, plat, NewStubEngine(), nil, opts); err == nil {
		t.Fatal("Connect с RUDirect на платформе без поддержки должен отказать")
	}
	if plat.snapshotCalls != 0 {
		t.Fatalf("отказ должен быть ДО снимка сети, Snapshot вызван %d раз", plat.snapshotCalls)
	}
}

func TestDirectRouterNeeded(t *testing.T) {
	cases := []struct {
		manual   []string
		ruDirect bool
		want     bool
	}{
		{nil, false, false},               // ни ручного списка, ни RUDirect — не нужен
		{nil, true, true},                 // только RUDirect — нужен ради встроенного списка
		{[]string{"2ip.ru"}, false, true}, // только ручной список — как раньше
		{[]string{"2ip.ru"}, true, true},  // оба сразу
	}
	for _, c := range cases {
		if got := directRouterNeeded(c.manual, c.ruDirect); got != c.want {
			t.Errorf("directRouterNeeded(%v, %v) = %v, хотели %v", c.manual, c.ruDirect, got, c.want)
		}
	}
}

// Белый режим (18.10.2026): корни — напрямую, исключения — всегда в туннель,
// ручной выбор человека главнее исключений.
func TestDirectMatcherExclusions(t *testing.T) {
	m := NewDirectMatcher([]string{"mail.ru", "habr.com", "ya.ru"})
	for name, want := range map[string]bool{
		"e.mail.ru":  true,
		"ya.ru":      true,
		"www.ya.ru":  true,
		"habr.com":   true, // ручная запись человека
		"other.ru":   false,
		"comms.ru":   false,
		"x.comms.ru": false,
		"4pda.ru":    false,
		"vk.com":     false, // не в списке запроса
	} {
		if got := m.Match(name); got != want {
			t.Errorf("%s: %v, хочу %v", name, got, want)
		}
	}
	// Исключение перебивает и автоматический корень, если бы он покрывал имя.
	auto := NewDirectMatcher([]string{"vk.com", "comms.ru"})
	if auto.Match("comms.ru") != true {
		// comms.ru — не корень, значит это ручная запись: остаётся в силе.
		t.Error("ручная запись comms.ru должна работать")
	}
}
