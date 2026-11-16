package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"meridian/engine/params"
)

// ParamsCache — последний удачный ответ /v1/params ключа подписчика.
// Под белыми списками API недоступен, а релей через VK/DION работает:
// без сохранённого ответа клиент не доходил до релея вовсе (клиент
// Windows 07.10). Хранится один ответ — того ключа, с которым он получен;
// смена ключа его сбрасывает, чужой ответ не берётся (контракт /v1/params, §5).
// nil — сохранения нет.
type ParamsCache struct {
	Dir string
}

const paramsCachePrefix = "params-"

func (c *ParamsCache) file(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.Dir, paramsCachePrefix+hex.EncodeToString(sum[:])+".json")
}

// Save — запомнить сырой ответ ключа; ответы других ключей удаляются.
// Ошибки диска проглатываются: подключение важнее.
func (c *ParamsCache) Save(key string, raw []byte) {
	if c == nil || c.Dir == "" || key == "" || len(raw) == 0 {
		return
	}
	if old, _, err := c.Load(key); err == nil && old.RoutingScope == "partner" {
		next, err := params.ParseAnswer(raw)
		if err != nil || next.RoutingScope != "partner" {
			return
		}
	}
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return
	}
	path := c.file(key)
	tmp, err := os.CreateTemp(c.Dir, ".params-*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	old, _ := filepath.Glob(filepath.Join(c.Dir, paramsCachePrefix+"*.json"))
	for _, f := range old {
		if f != path {
			_ = os.Remove(f)
		}
	}
}

// Load — сохранённый ответ ключа и когда он получен.
func (c *ParamsCache) Load(key string) (*ParamsAnswer, time.Time, error) {
	if c == nil || c.Dir == "" || key == "" {
		return nil, time.Time{}, errors.New("сохранённых параметров нет")
	}
	path := c.file(key)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	a, err := params.ParseAnswer(raw)
	if err != nil {
		return nil, time.Time{}, err
	}
	return a, fi.ModTime(), nil
}

// paramsFallbackAllowed — по какой ошибке Fetch можно работать по
// сохранённому: служба недоступна или перегружена (429/5xx). Отказ по
// ключу и несовместимый договор — нет: там сохранённое не поможет.
func paramsFallbackAllowed(err error) bool {
	var pe *ParamsError
	if !errors.As(err, &pe) {
		return false
	}
	switch pe.Kind {
	case params.KindUnavailable:
		return true
	case params.KindRefused:
		return pe.Code == 429 || pe.Code >= 500
	}
	return false
}

// cachedScopeAllowed keeps unclassified legacy cache and mismatched scoped
// responses from sending a partner key to the ordinary gateway.
func cachedScopeAllowed(a *ParamsAnswer, err error) bool {
	if a == nil || (a.RoutingScope != "ordinary" && a.RoutingScope != "partner") {
		return false
	}
	var pe *ParamsError
	return !(errors.As(err, &pe) && pe.RoutingScope == "partner" && a.RoutingScope != "partner")
}

// paramsAge — возраст для лога, в часах.
func paramsAge(t time.Time) string {
	return fmt.Sprintf("%.0f ч", time.Since(t).Hours())
}

// paramsRefreshDelays — паузы перед попытками получить свежий ответ через
// туннель: сначала скоро (релей только поднялся), потом реже; последняя
// пауза повторяется, пока не получится или сессия не кончится.
var paramsRefreshDelays = []time.Duration{20 * time.Second, time.Minute, 3 * time.Minute, 10 * time.Minute}

// refreshParamsViaTunnel — пока сессия жива и ответ был взят из сохранённых,
// добывает свежий через туннель и кладёт в кэш (для СЛЕДУЮЩЕГО старта).
// Отказ по ключу или несовместимый договор — прекращает попытки: повтор не
// поможет. Текущую сессию не трогает.
func refreshParamsViaTunnel(ctx context.Context, fetch func(context.Context, string) (*ParamsAnswer, error), cache *ParamsCache, key string, delays []time.Duration) bool {
	for i := 0; ; i++ {
		d := delays[len(delays)-1]
		if i < len(delays) {
			d = delays[i]
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
		}
		fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ans, err := fetch(fctx, key)
		cancel()
		if err == nil {
			cache.Save(key, ans.Raw)
			fmt.Fprintln(os.Stderr, "сохранённые параметры обновлены через туннель")
			return true
		}
		if !paramsFallbackAllowed(err) {
			var pe *ParamsError
			if errors.As(err, &pe) {
				fmt.Fprintf(os.Stderr, "свежие параметры через туннель не получены (%v) — повторов не будет\n", pe.Kind)
				return false
			}
		}
	}
}

// TunnelParamsAddresses — адреса API для запроса ИЗ туннеля: только имена.
// Замер с выходного узла (07.10): имена отвечают, голый IP — таймаут; по IP
// ходить из туннеля нечего, а ждать его таймаута — терять время попытки.
func TunnelParamsAddresses() []params.Address {
	var out []params.Address
	for _, a := range params.DefaultAddresses() {
		if net.ParseIP(a.Host) == nil {
			out = append(out, a)
		}
	}
	return out
}

// builtinRelayTarget — запасной конечный адрес релея (relay.target) на
// момент сборки (вход 1; с 03.10 вместо Парижа: VK-путь до Парижа не
// доставляет, кот 1). Последний запас: первой пробуется ступень на gateway,
// при живом /v1/params берётся свежий target. Сменится, только если вход
// заблокируют, кот 1 предупредит.
const builtinRelayTarget = "62.76.231.231"

// builtinRelayOnlyParams — последний запасной вариант, когда /v1/params
// недоступен и сохранённого ответа нет (кэш появился в 0.5.32, у кого не
// было ни одного удачного запроса после обновления, его просто нет).
// Для белого режима с автоматической лестницей или режима только релей,
// если есть ссылки VK или комнаты DION. Прямых узлов здесь нет. Для
// чёрного режима и режима только прямой путь — nil; в кэш не пишется.
func builtinRelayOnlyParams(opts ConnectOptions) *ParamsAnswer {
	if opts.Ladder.Mode != LadderModeRelayOnly && (opts.Ladder.Mode != LadderModeAuto || opts.TunnelOnly) {
		return nil
	}
	if len(opts.Ladder.Hashes) == 0 && len(dionRoomsForLadder(opts.DionRooms)) == 0 {
		return nil
	}
	a := &ParamsAnswer{}
	a.Relay.Enabled = true
	a.Relay.Target = builtinRelayTarget
	a.Ladder.SecondRoundMs = 3000
	a.Ladder.DirectSkipAfterFails = 5
	a.Ladder.DirectProbeEveryAttempt = 4
	return a
}

const (
	paramsRefreshMin     = 10 * time.Minute
	paramsRefreshMax     = 6 * time.Hour
	paramsRefreshDefault = time.Hour
	paramsRetryEvery     = 10 * time.Minute

	// paramsCacheMaxAge — сохранённому ответу белого режима верим не дольше
	// недели: набор входов у ключа меняется (неделя на трёх адресах, потом
	// все), а встроенный минимум белому режиму всегда есть чем заменить.
	paramsCacheMaxAge = 7 * 24 * time.Hour
)

// paramsRefreshEvery — как часто перезапрашивать params, пока сессия жива:
// по сроку жизни ответа (ttl), в пределах 10 минут … 6 часов; не прислали —
// раз в час.
func paramsRefreshEvery(ttlSeconds int) time.Duration {
	if ttlSeconds <= 0 {
		return paramsRefreshDefault
	}
	d := time.Duration(ttlSeconds) * time.Second
	if d < paramsRefreshMin {
		return paramsRefreshMin
	}
	if d > paramsRefreshMax {
		return paramsRefreshMax
	}
	return d
}

// paramsCacheUsable — можно ли идти по сохранённому ответу этого возраста.
// Старше недели его не берём только там, где есть чем заменить: белому
// режиму встроенный минимум (входы ему не нужны). Прямым режимам, у которых
// иного набора входов нет, остаётся старый — лучше старый, чем никакой.
func paramsCacheUsable(at time.Time, opts ConnectOptions) bool {
	if time.Since(at) <= paramsCacheMaxAge {
		return true
	}
	return opts.Ladder.Mode != LadderModeRelayOnly || builtinRelayOnlyParams(opts) == nil
}

// paramsFetchChain — сначала через туннель (клиент без Protector), потом
// обычным путём; любой из двоих может отсутствовать.
func paramsFetchChain(tunnel *ParamsClient, main ParamsFetcher) func(context.Context, string) (*ParamsAnswer, error) {
	return func(ctx context.Context, key string) (*ParamsAnswer, error) {
		var firstErr error
		if tunnel != nil {
			a, err := tunnel.Fetch(ctx, key)
			if err == nil {
				return a, nil
			}
			firstErr = err
			if !paramsFallbackAllowed(err) {
				var pe *ParamsError
				if errors.As(err, &pe) {
					return nil, err // отказ службы: второй путь не поможет
				}
			}
		}
		if main != nil {
			a, err := main.Fetch(ctx, key)
			if err == nil {
				return a, nil
			}
			return nil, err
		}
		return nil, firstErr
	}
}

// nextRefresh — подменяется в тестах.
var nextRefresh = paramsRefreshEvery

// keepParamsFresh — пока сессия жива, раз в every (по сроку жизни ответа)
// перезапрашивает params и кладёт свежий ответ в кэш. Сессию не
// перестраивает: расширенный набор входов заберёт следующее подключение.
// Неудача — повтор через retry; отказ по ключу прекращает обновления.
func keepParamsFresh(ctx context.Context, fetch func(context.Context, string) (*ParamsAnswer, error), cache *ParamsCache, key string, every, retry time.Duration) {
	wait := every
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ans, err := fetch(fctx, key)
		cancel()
		if err == nil {
			cache.Save(key, ans.Raw)
			wait = nextRefresh(ans.TTLSeconds)
			continue
		}
		var pe *ParamsError
		if errors.As(err, &pe) && !paramsFallbackAllowed(err) {
			return
		}
		wait = retry
	}
}
