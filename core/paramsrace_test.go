package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"meridian/engine/params"
)

type fakeFetcher struct {
	delay time.Duration
	ans   *ParamsAnswer
	err   error
}

func (f fakeFetcher) Fetch(ctx context.Context, key string) (*ParamsAnswer, error) {
	select {
	case <-time.After(f.delay):
		return f.ans, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func silent() error { return &ParamsError{Kind: params.KindUnavailable} }

func TestParamsRaceFirstSuccessWinsWithoutWaitingForSilent(t *testing.T) {
	want := &ParamsAnswer{}
	r := &ParamsRace{Fetchers: []ParamsFetcher{
		fakeFetcher{delay: time.Hour, err: silent()}, // молчит «вечно»
		fakeFetcher{delay: 10 * time.Millisecond, ans: want},
		fakeFetcher{delay: time.Hour, err: silent()},
	}}
	start := time.Now()
	got, err := r.Fetch(context.Background(), "k")
	if err != nil || got != want {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("ждали молчащих: %v", time.Since(start))
	}
}

func TestParamsRaceAuthoritativeRefusalReturnsAtOnce(t *testing.T) {
	refused := &ParamsError{Kind: params.KindRefused, Code: 403}
	r := &ParamsRace{Fetchers: []ParamsFetcher{
		fakeFetcher{delay: time.Hour, err: silent()},
		fakeFetcher{delay: 5 * time.Millisecond, err: refused},
	}}
	start := time.Now()
	_, err := r.Fetch(context.Background(), "k")
	if err != refused {
		t.Fatalf("err = %v, ждали отказ по ключу", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("отказ службы должен возвращаться сразу")
	}
}

func TestParamsRaceAllSilentIsUnavailable(t *testing.T) {
	r := &ParamsRace{Fetchers: []ParamsFetcher{
		fakeFetcher{delay: 5 * time.Millisecond, err: silent()},
		fakeFetcher{delay: 10 * time.Millisecond, err: silent()},
	}}
	_, err := r.Fetch(context.Background(), "k")
	if !paramsFallbackAllowed(err) {
		t.Fatalf("при молчании всех должен быть разрешён запасной путь: %v", err)
	}
}

func TestParamsRaceRespectsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r := &ParamsRace{Fetchers: []ParamsFetcher{fakeFetcher{delay: time.Hour}, fakeFetcher{delay: time.Hour}}}
	_, err := r.Fetch(ctx, "k")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewParamsRaceOneClientPerAddress(t *testing.T) {
	r := NewParamsRace(ParamsConfig{})
	if len(r.Fallbacks) != 2 {
		t.Fatalf("expected two reserve paths, got %d", len(r.Fallbacks))
	}
	if len(r.Fetchers) != 3 {
		t.Fatalf("клиентов %d, ждали 3 (по адресу на клиента)", len(r.Fetchers))
	}
}

type countingFetcher struct {
	called *int
	answer *ParamsAnswer
	err    error
}

func (f countingFetcher) Fetch(ctx context.Context, key string) (*ParamsAnswer, error) {
	*f.called++
	return f.answer, f.err
}
func TestParamsRaceReserveOnlyAfterPrimaryUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name         string
		primary      error
		reserveCalls int
		success      bool
	}{
		{"primary success", nil, 0, true},
		{"primary unavailable", silent(), 1, true},
		{"primary refusal", &ParamsError{Kind: params.KindRefused, Code: 403}, 0, false},
		{"primary incompatible", &ParamsError{Kind: params.KindIncompatible}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			want := &ParamsAnswer{}
			r := &ParamsRace{Fetchers: []ParamsFetcher{countingFetcher{called: new(int), answer: want, err: tc.primary}}, Fallbacks: []ParamsFetcher{countingFetcher{called: &calls, answer: want}}}
			got, err := r.Fetch(context.Background(), "test")
			if calls != tc.reserveCalls || (err == nil) != tc.success || (tc.success && got != want) {
				t.Fatalf("reserve calls=%d success=%v", calls, err == nil)
			}
		})
	}
}

func TestParamsRaceReserveRefusalAndCancel(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	refused := &ParamsError{Kind: params.KindRefused, Code: 503}
	r := &ParamsRace{Fetchers: []ParamsFetcher{countingFetcher{called: &primaryCalls, err: silent()}}, Fallbacks: []ParamsFetcher{countingFetcher{called: &fallbackCalls, err: refused}}}
	if _, err := r.Fetch(context.Background(), "test"); err != refused || fallbackCalls != 1 {
		t.Fatal("reserve refusal must not become success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fallbackCalls = 0
	if _, err := r.Fetch(ctx, "test"); err == nil || fallbackCalls != 0 {
		t.Fatal("cancelled request reached reserve")
	}
}

func TestReservePathsAttemptedInOrder(t *testing.T) {
	a, b := 0, 0
	want := &ParamsAnswer{}
	r := &ParamsRace{Fetchers: []ParamsFetcher{countingFetcher{called: new(int), err: silent()}}, Fallbacks: []ParamsFetcher{countingFetcher{called: &a, answer: want}, countingFetcher{called: &b, answer: want}}}
	if got, err := r.Fetch(context.Background(), "test"); err != nil || got != want || a != 1 || b != 0 {
		t.Fatal("additional reserve used before primary reserve")
	}
	a, b = 0, 0
	r.Fallbacks[0] = countingFetcher{called: &a, err: silent()}
	if got, err := r.Fetch(context.Background(), "test"); err != nil || got != want || a != 1 || b != 1 {
		t.Fatal("additional reserve not used after first reserve unavailable")
	}
}
