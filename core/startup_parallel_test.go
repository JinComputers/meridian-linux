package core

import (
	"context"
	"errors"
	"meridian/engine/params"
	"testing"
	"time"
)

type parallelFetcher struct {
	paramsStarted, eligibilityStarted chan struct{}
	release                           chan struct{}
	fail                              bool
	eligible, premium, trial          bool
}

func (f *parallelFetcher) Fetch(ctx context.Context, _ string) (*ParamsAnswer, error) {
	close(f.paramsStarted)
	if f.fail {
		return nil, errors.New("отказ")
	}
	select {
	case <-f.release:
		return &ParamsAnswer{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (f *parallelFetcher) RelayEligibility(ctx context.Context, _ string) (bool, bool, bool) {
	close(f.eligibilityStarted)
	select {
	case <-f.release:
		return f.eligible, f.premium, f.trial
	case <-ctx.Done():
		return false, false, false
	}
}
func (f *parallelFetcher) FetchRelayLinks(context.Context, string, string) (*params.RelayLinks, error) {
	return &params.RelayLinks{VK: []string{"a"}, Dion: []string{"b"}}, nil
}
func TestFetchStartupRunsRequestsConcurrently(t *testing.T) {
	f := &parallelFetcher{paramsStarted: make(chan struct{}), eligibilityStarted: make(chan struct{}), release: make(chan struct{}), eligible: true, premium: false}
	done := make(chan startupRelayResult, 1)
	go func() { _, r, _ := fetchStartup(context.Background(), f, "key", "device"); done <- r }()
	for _, started := range []chan struct{}{f.paramsStarted, f.eligibilityStarted} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("запросы не стартовали параллельно")
		}
	}
	close(f.release)
	select {
	case r := <-done:
		if !r.eligible || r.premium || !r.fetched {
			t.Fatalf("неверная проверка: %+v", r)
		}
	case <-time.After(time.Second):
		t.Fatal("запрос не завершился")
	}
}
func TestFetchStartupCancelsPoolBeforeReturningFailure(t *testing.T) {
	f := &parallelFetcher{paramsStarted: make(chan struct{}), eligibilityStarted: make(chan struct{}), release: make(chan struct{}), fail: true}
	done := make(chan error, 1)
	go func() { _, _, err := fetchStartup(context.Background(), f, "key", "device"); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ожидали отказ")
		}
	case <-time.After(time.Second):
		t.Fatal("ожидание отменённого запроса")
	}
}

type slowEligibilityFetcher struct{}

func (slowEligibilityFetcher) Fetch(context.Context, string) (*ParamsAnswer, error) {
	return &ParamsAnswer{}, nil
}
func (slowEligibilityFetcher) RelayEligibility(ctx context.Context, _ string) (bool, bool, bool) {
	select {
	case <-time.After(5100 * time.Millisecond):
		return true, true, true
	case <-ctx.Done():
		return false, false, false
	}
}
func (slowEligibilityFetcher) FetchRelayLinks(context.Context, string, string) (*params.RelayLinks, error) {
	return &params.RelayLinks{VK: []string{"test"}}, nil
}
func TestStartupAllowsReserveAfterSlowPrimary(t *testing.T) {
	_, pool, err := fetchStartup(context.Background(), slowEligibilityFetcher{}, "key", "device")
	if err != nil || !pool.eligible || !pool.fetched {
		t.Fatal("reserve must have time after slow primary")
	}
}
