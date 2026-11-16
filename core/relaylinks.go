package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"meridian/engine/params"
)

// FetchRelayLinks tries only clients configured for the trusted CDN.
func (r *ParamsRace) FetchRelayLinks(ctx context.Context, key, device string) (*params.RelayLinks, error) {
	for _, f := range append(append([]ParamsFetcher(nil), r.Fetchers...), r.Fallbacks...) {
		if c, ok := f.(*params.Client); ok {
			links, err := c.FetchRelayLinks(ctx, key, device)
			if err == nil {
				return links, nil
			}
			if errors.Is(err, params.ErrRelayRefused) {
				return nil, err
			}
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("не удалось получить комнаты релея")
}

func relayLinksPath(c *ParamsCache, key, device string) string {
	if c == nil || c.Dir == "" || key == "" || device == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key + "\x00" + device))
	return filepath.Join(c.Dir, "relay-links-"+hex.EncodeToString(sum[:])+".json")
}

func cachedRelayLinks(c *ParamsCache, key, device string) *params.RelayLinks {
	path := relayLinksPath(c, key, device)
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) > 65536 {
		return nil
	}
	var v params.RelayLinks
	if json.Unmarshal(raw, &v) != nil || len(v.VK) > 5 || len(v.Dion) > 3 {
		return nil
	}
	return &v
}

func saveRelayLinks(c *ParamsCache, key, device string, v *params.RelayLinks) {
	path := relayLinksPath(c, key, device)
	if path == "" || v == nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".relay-links-*.tmp")
	if err != nil {
		return
	}
	_, err = tmp.Write(b)
	if e := tmp.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
}

func addUniqueStrings(existing []string, additions []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, v := range existing {
		seen[v] = true
	}
	for _, v := range additions {
		if v != "" && !seen[v] {
			existing = append(existing, v)
			seen[v] = true
		}
	}
	return existing
}

// RelayEligibility requires an explicit trial flag and a successful verdict.
// Trial users receive both kinds of relay, regardless of any stored tier.
func relayEligibilityBody(body map[string]interface{}) (eligible, premium bool) {
	trial, present := body["trial"].(bool)
	verdict, _ := body["verdict"].(string)
	if !present || verdict != "ok" {
		return false, false
	}
	if trial {
		return true, true
	}
	tier, _ := body["tier"].(string)
	return true, tier == "" || tier == "premium"
}
func relayEligibility(ctx context.Context, c *params.Client, key string) (bool, bool, bool) {
	r, ok := c.Call(ctx, "/v1/key", map[string]string{"key": key}).(params.Ok)
	if !ok {
		return false, false, false
	}
	eligible, premium := relayEligibilityBody(r.Body)
	trial, _ := r.Body["trial"].(bool)
	return eligible, premium, trial
}
func relayEligibilityGroup(ctx context.Context, fetchers []ParamsFetcher, key string) (bool, bool, bool, bool) {
	clients := []*params.Client{}
	for _, f := range fetchers {
		if c, ok := f.(*params.Client); ok {
			clients = append(clients, c)
		}
	}
	if len(clients) == 0 {
		return false, false, false, false
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan params.Result, len(clients))
	for _, c := range clients {
		go func(c *params.Client) { ch <- c.Call(ctx, "/v1/key", map[string]string{"key": key}) }(c)
	}
	for range clients {
		select {
		case result := <-ch:
			if _, unavailable := result.(params.Unavailable); unavailable {
				continue
			}
			if answer, ok := result.(params.Ok); ok {
				eligible, premium := relayEligibilityBody(answer.Body)
				trial, _ := answer.Body["trial"].(bool)
				return eligible, premium, trial, true
			}
			return false, false, false, true
		case <-ctx.Done():
			return false, false, false, false
		}
	}
	return false, false, false, false
}
func (r *ParamsRace) RelayEligibility(ctx context.Context, key string) (bool, bool, bool) {
	eligible, premium, trial, answered := relayEligibilityGroup(ctx, r.Fetchers, key)
	if answered || ctx.Err() != nil {
		return eligible, premium, trial
	}
	for _, fallback := range r.Fallbacks {
		eligible, premium, trial, answered = relayEligibilityGroup(ctx, []ParamsFetcher{fallback}, key)
		if answered || ctx.Err() != nil {
			return eligible, premium, trial
		}
	}
	return false, false, false
}

func (p PartnerParamsFetcher) RelayEligibility(ctx context.Context, key string) (bool, bool, bool) {
	return relayEligibility(ctx, p.Client, key)
}
func (p PartnerParamsFetcher) FetchRelayLinks(ctx context.Context, key, device string) (*params.RelayLinks, error) {
	if p.RelayClient == nil {
		return nil, errors.New("нет клиента доверенного CDN")
	}
	return p.RelayClient.FetchRelayLinks(ctx, key, device)
}

// startupRelayResult carries the room request without publishing it to cache
// until the independently fetched route has passed scope validation.
type startupRelayResult struct {
	links                                      *params.RelayLinks
	eligible, premium, trial, fetched, refused bool
}
type startupRelayFetcher interface {
	RelayEligibility(context.Context, string) (bool, bool, bool)
	FetchRelayLinks(context.Context, string, string) (*params.RelayLinks, error)
}

func fetchStartup(ctx context.Context, fetcher ParamsFetcher, key, device string) (*ParamsAnswer, startupRelayResult, error) {
	f, ok := fetcher.(startupRelayFetcher)
	if !ok || device == "" {
		a, e := fetcher.Fetch(ctx, key)
		return a, startupRelayResult{}, e
	}
	poolCtx, cancel := context.WithTimeout(ctx, 36*time.Second)
	defer cancel()
	ch := make(chan startupRelayResult, 1)
	go func() {
		r := startupRelayResult{}
		eligibilityCtx, stop := context.WithTimeout(poolCtx, 17*time.Second)
		r.eligible, r.premium, r.trial = f.RelayEligibility(eligibilityCtx, key)
		stop()
		if r.eligible {
			var err error
			r.links, err = f.FetchRelayLinks(poolCtx, key, device)
			r.fetched = err == nil && r.links != nil
			r.refused = errors.Is(err, params.ErrRelayRefused)
		}
		ch <- r
	}()
	a, err := fetcher.Fetch(ctx, key)
	if err != nil {
		cancel()
	}
	result := <-ch
	return a, result, err
}

// relayPoolForSession never reuses a trial pool after the device request fails.
func relayPoolForSession(pool startupRelayResult, cache *ParamsCache, key, device string) *params.RelayLinks {
	if !pool.eligible {
		return nil
	}
	if pool.fetched {
		saveRelayLinks(cache, key, device, pool.links)
		return pool.links
	}
	if pool.trial || pool.refused {
		return nil
	}
	return cachedRelayLinks(cache, key, device)
}
