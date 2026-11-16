package core

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"meridian/engine/params"
)

const cacheAnswer = `{"status":"answered","entries":[{"id":"a","host":"203.0.113.5","port":1,"dtls":false}],"relay":{"enabled":true}}`

func TestParamsCacheSaveLoadPerKey(t *testing.T) {
	dir := t.TempDir()
	c := &ParamsCache{Dir: dir}
	c.Save("key-one", []byte(cacheAnswer))

	a, _, err := c.Load("key-one")
	if err != nil || len(a.Nodes) != 1 || a.Nodes[0].Host != "203.0.113.5" {
		t.Fatalf("Load: %v %+v", err, a)
	}
	if _, _, err := c.Load("key-two"); err == nil {
		t.Fatal("ответ другого ключа брать нельзя")
	}

	c.Save("key-two", []byte(cacheAnswer))
	if _, _, err := c.Load("key-one"); err == nil {
		t.Fatal("смена ключа обязана сбросить прежний ответ")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 1 {
		t.Fatalf("хранится один ответ, файлов %v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if strings.Contains(files[0], "key-two") || strings.Contains(string(raw), "key-two") {
		t.Fatal("ключ не должен попадать ни в имя, ни в файл")
	}
}

func TestParamsCacheNilSafe(t *testing.T) {
	var c *ParamsCache
	c.Save("k", []byte(cacheAnswer))
	if _, _, err := c.Load("k"); err == nil {
		t.Fatal("nil-кэш ничего не отдаёт")
	}
}

func TestParamsFallbackAllowed(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&params.Error{Kind: params.KindUnavailable}, true},
		{&params.Error{Kind: params.KindRefused, Code: 429}, true},
		{&params.Error{Kind: params.KindRefused, Code: 503}, true},
		{&params.Error{Kind: params.KindRefused, Code: 403}, false},
		{&params.Error{Kind: params.KindIncompatible}, false},
		{errors.New("другое"), false},
	} {
		if got := paramsFallbackAllowed(tc.err); got != tc.want {
			t.Errorf("%+v: %v, хочу %v", tc.err, got, tc.want)
		}
	}
}

func TestRefreshParamsViaTunnelSavesAfterRetries(t *testing.T) {
	dir := t.TempDir()
	c := &ParamsCache{Dir: dir}
	calls := 0
	fetch := func(context.Context, string) (*ParamsAnswer, error) {
		calls++
		if calls < 3 {
			return nil, &ParamsError{Kind: params.KindUnavailable}
		}
		return &ParamsAnswer{Raw: []byte(cacheAnswer)}, nil
	}
	refreshParamsViaTunnel(context.Background(), fetch, c, "k", []time.Duration{time.Millisecond})
	if calls != 3 {
		t.Fatalf("попыток %d, ждали 3", calls)
	}
	if _, _, err := c.Load("k"); err != nil {
		t.Fatalf("свежий ответ не сохранён: %v", err)
	}
}

func TestRefreshParamsViaTunnelStopsOnKeyRefusal(t *testing.T) {
	calls := 0
	fetch := func(context.Context, string) (*ParamsAnswer, error) {
		calls++
		return nil, &ParamsError{Kind: params.KindRefused, Code: 403}
	}
	refreshParamsViaTunnel(context.Background(), fetch, &ParamsCache{Dir: t.TempDir()}, "k", []time.Duration{time.Millisecond})
	if calls != 1 {
		t.Fatalf("после отказа по ключу повторы не нужны, попыток %d", calls)
	}
}

func TestRefreshParamsViaTunnelStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		refreshParamsViaTunnel(ctx, func(context.Context, string) (*ParamsAnswer, error) {
			return nil, &ParamsError{Kind: params.KindUnavailable}
		}, &ParamsCache{Dir: t.TempDir()}, "k", []time.Duration{time.Hour})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("не остановилось по отмене")
	}
}

func TestTunnelParamsAddressesNamesOnly(t *testing.T) {
	got := TunnelParamsAddresses()
	if len(got) != 4 {
		t.Fatalf("адресов %d, ждали 4 (имена без IP): %+v", len(got), got)
	}
	for _, a := range got {
		if net.ParseIP(a.Host) != nil {
			t.Errorf("голый IP %s в списке для туннеля", a.Host)
		}
	}
	if !got[0].Pinned || got[1].Pinned || got[2].Pinned || got[2].Host != "api-ru.meridianvpn.org" || got[3].Pinned || got[3].Host != "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev" {
		t.Errorf("пин: первый закреплён, cdn без пина: %+v", got)
	}
}

func TestBuiltinRelayOnlyParams(t *testing.T) {
	white := ConnectOptions{Ladder: LadderOptions{Mode: LadderModeRelayOnly, Hashes: []string{"h"}}}
	a := builtinRelayOnlyParams(white)
	if a == nil || !a.Relay.Enabled || len(a.Nodes) != 0 {
		t.Fatalf("встроенные параметры белого режима: %+v", a)
	}
	// Лестница собирается: только релей, без прямых ступеней.
	l, info, err := BuildLadder(a, white.Ladder)
	if err != nil || l == nil || info == nil || info.NoHashes {
		t.Fatalf("BuildLadder по встроенным: err=%v info=%+v", err, info)
	}
	if info.RelayTarget != "ok" {
		t.Fatalf("запасной relay.target не принят лестницей: %q", info.RelayTarget)
	}
	for name, o := range map[string]ConnectOptions{
		"чёрный режим":     {TunnelOnly: true, Ladder: LadderOptions{Hashes: []string{"h"}}},
		"белый без ссылок": {Ladder: LadderOptions{Mode: LadderModeRelayOnly}},
		"прямой без релея": {Ladder: LadderOptions{Mode: LadderModeDirectOnly, Hashes: []string{"h"}}},
	} {
		if builtinRelayOnlyParams(o) != nil {
			t.Errorf("%s: встроенные параметры не должны применяться", name)
		}
	}

	auto := ConnectOptions{Ladder: LadderOptions{Mode: LadderModeAuto, Hashes: []string{"h"}}}
	fallback := builtinRelayOnlyParams(auto)
	if fallback == nil {
		t.Fatal("белый режим с автоматической лестницей должен пробовать релей при недоступном API")
	}
	if l, info, err := BuildLadder(fallback, auto.Ladder); err != nil || l == nil || info == nil || info.NoHashes {
		t.Fatalf("автоматическая лестница с запасным релеем: %v", err)
	}
	dion := ConnectOptions{Ladder: LadderOptions{Mode: LadderModeRelayOnly}, DionRooms: []string{"abc-def-ghi"}}
	if builtinRelayOnlyParams(dion) == nil {
		t.Error("белый режим с комнатой DION должен получить встроенные параметры")
	}
}

func TestParamsRefreshEvery(t *testing.T) {
	for ttl, want := range map[int]time.Duration{
		0: time.Hour, -5: time.Hour, 60: 10 * time.Minute, 1800: 30 * time.Minute, 86400: 6 * time.Hour,
	} {
		if got := paramsRefreshEvery(ttl); got != want {
			t.Errorf("ttl %d: %v, ждали %v", ttl, got, want)
		}
	}
}

func TestParamsCacheUsableByAge(t *testing.T) {
	white := ConnectOptions{Ladder: LadderOptions{Mode: LadderModeRelayOnly, Hashes: []string{"h"}}}
	black := ConnectOptions{Ladder: LadderOptions{Hashes: []string{"h"}}}
	fresh, old := time.Now().Add(-time.Hour), time.Now().Add(-8*24*time.Hour)
	if !paramsCacheUsable(fresh, white) || !paramsCacheUsable(fresh, black) {
		t.Error("свежий ответ всегда годен")
	}
	if paramsCacheUsable(old, white) {
		t.Error("белый режим: ответ старше недели заменяется встроенным минимумом")
	}
	if !paramsCacheUsable(old, black) {
		t.Error("прямой режим: старый ответ лучше никакого")
	}
	whiteNoLinks := ConnectOptions{Ladder: LadderOptions{Mode: LadderModeRelayOnly}}
	if !paramsCacheUsable(old, whiteNoLinks) {
		t.Error("без ссылок встроенный минимум недоступен: остаётся старый")
	}
}

func TestKeepParamsFreshSavesAndStopsOnRefusal(t *testing.T) {
	old := nextRefresh
	nextRefresh = func(int) time.Duration { return time.Millisecond }
	defer func() { nextRefresh = old }()
	c := &ParamsCache{Dir: t.TempDir()}
	calls := 0
	fetch := func(context.Context, string) (*ParamsAnswer, error) {
		calls++
		switch calls {
		case 1:
			return nil, &ParamsError{Kind: params.KindUnavailable}
		case 2:
			return &ParamsAnswer{Raw: []byte(cacheAnswer)}, nil
		}
		return nil, &ParamsError{Kind: params.KindRefused, Code: 403}
	}
	done := make(chan struct{})
	go func() {
		keepParamsFresh(context.Background(), fetch, c, "k", time.Millisecond, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("не остановилось после отказа по ключу")
	}
	if calls != 3 {
		t.Fatalf("попыток %d, ждали 3 (молчание, успех, отказ)", calls)
	}
	if _, _, err := c.Load("k"); err != nil {
		t.Fatalf("свежий ответ не сохранён: %v", err)
	}
}
