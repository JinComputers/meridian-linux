package core

import (
	"context"
	"sync"
	"time"

	"meridian/engine/params"
)

// ParamsFetcher — то, чем Connect получает /v1/params: *ParamsClient или
// ParamsRace.
type ParamsFetcher interface {
	Fetch(ctx context.Context, key string) (*ParamsAnswer, error)
}

// ParamsRace — адреса API опрашиваются ОДНОВРЕМЕННО, побеждает первый
// ответ. Клиент params обходит адреса по очереди, и при молчащем API
// (белые списки) подряд набегали таймауты всех трёх (просьба кота 2,
// 08.10); здесь молчание тратит один короткий срок, а не сумму.
//
// Окончательным считается любой ответ службы, кроме «недоступно»: отказ по
// ключу или несовместимый договор возвращаются сразу, не дожидаясь
// остальных. Только когда молчат все — «недоступно».
type ParamsRace struct {
	Fetchers  []ParamsFetcher
	Fallbacks []ParamsFetcher // резерв вызывается лишь если все основные недоступны
}

// paramsRaceBudget — на весь опрос; свои сроки соединения и чтения у
// каждого клиента.
const paramsRaceBudget = 5 * time.Second

// NewParamsRace — по одному клиенту на каждый адрес (cfg.Addresses, а если
// пусто — на адреса по умолчанию), все с конвертом и Protector из cfg.
func NewParamsRace(cfg ParamsConfig) *ParamsRace {
	addrs := cfg.Addresses
	if len(addrs) == 0 {
		addrs = params.DefaultAddresses()
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 2500 * time.Millisecond
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 2500 * time.Millisecond
	}
	if cfg.WalkBudget == 0 {
		cfg.WalkBudget = paramsRaceBudget
	}
	r := &ParamsRace{}
	for _, a := range addrs {
		c := cfg
		c.Addresses = []params.Address{a}
		if (a.Host == "api-ru.meridianvpn.org" || a.Host == "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev") && a.Port == 443 && !a.Pinned {
			r.Fallbacks = append(r.Fallbacks, params.New(c))
		} else {
			r.Fetchers = append(r.Fetchers, params.New(c))
		}
	}
	return r
}

type raceResult struct {
	ans *ParamsAnswer
	err error
}

func (r *ParamsRace) Fetch(ctx context.Context, key string) (*ParamsAnswer, error) {
	var err error = &ParamsError{Kind: params.KindUnavailable}
	if len(r.Fetchers) > 0 {
		var ans *ParamsAnswer
		ans, err = r.fetchPrimary(ctx, key)
		if err == nil || !isUnavailable(err) || ctx.Err() != nil {
			return ans, err
		}
	}
	for _, f := range r.Fallbacks {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		ans, e := f.Fetch(ctx, key)
		if e == nil || !isUnavailable(e) {
			return ans, e
		}
		err = e
	}
	return nil, err
}

func (r *ParamsRace) fetchPrimary(ctx context.Context, key string) (*ParamsAnswer, error) {
	if len(r.Fetchers) == 0 {
		return nil, &ParamsError{Kind: params.KindUnavailable}
	}
	if len(r.Fetchers) == 1 {
		return r.Fetchers[0].Fetch(ctx, key)
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch := make(chan raceResult, len(r.Fetchers))
	var wg sync.WaitGroup
	for _, f := range r.Fetchers {
		wg.Add(1)
		go func(f ParamsFetcher) {
			defer wg.Done()
			a, err := f.Fetch(rctx, key)
			ch <- raceResult{a, err}
		}(f)
	}
	var firstErr error
	for range r.Fetchers {
		res := <-ch
		if res.err == nil {
			return res.ans, nil
		}
		if firstErr == nil {
			firstErr = res.err
		}
		if !isUnavailable(res.err) {
			return nil, res.err
		}
	}
	return nil, firstErr
}

// isUnavailable — «молчит» (а не отказ службы): по такой ошибке остальные
// адреса стоит дождаться.
func isUnavailable(err error) bool {
	pe, ok := err.(*ParamsError)
	return ok && pe.Kind == params.KindUnavailable
}
