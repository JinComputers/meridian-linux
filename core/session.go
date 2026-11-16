package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JinComputers/meridian-desktop/core/rudirect"
)

// defaultTunMTUCeiling — tunMTU движка (engine/session.go): потолок
// буферов TUN для ЛЮБОЙ ступени лестницы, не зависит от того, какая
// выиграет. Используется, пока настоящий MTU неизвестен (см. Platform.NewTun).
const defaultTunMTUCeiling = 1200

// ConnectOptions — всё, что нужно для одной попытки подключения (план
// этапа 1, разделы 1 и 3). Ladder собирает сам Connect — через
// core.BuildLadder (engine-params-v1.1.0, кот 3), не свою версию;
// вызывающий передаёт только LadderOptions (память путей, хеши VK).
type ConnectOptions struct {
	// KeyPath — путь к файлу с ключом подписчика (план, раздел 1, п. 1;
	// используется meridian-cli этапа 1). Содержимое нигде не
	// печатается и не логируется.
	KeyPath string
	// KeyMaterial — само содержимое ключа, минуя файл (этап 2, решение
	// владельца 24.09: интерфейс держит ключ в Keychain, шлёт помощнику
	// только на время сессии). Непусто — используется ВМЕСТО KeyPath
	// (resolveKey в session.go), пусто — старое поведение (KeyPath)
	// не меняется.
	KeyMaterial string

	DeviceID        string
	Gateway         string // имя/адрес шлюза — и для /v1/params, и для движка
	CachedFallback  string
	PartnerRouting  bool
	DisableAutoDion bool

	// ParamsCache — сохранённый ответ /v1/params (core/paramscache.go);
	// nil — без сохранения, API обязан ответить.
	ParamsCache *ParamsCache

	// ParamsViaTunnel — клиент /v1/params БЕЗ Protector: его сокеты идут по
	// таблице маршрутов, то есть через поднятый туннель. Нужен, только когда
	// Connect стартовал по сохранённым параметрам: пока сессия жива, свежий
	// ответ добывается через туннель и ложится в ParamsCache на следующий
	// старт (сессию он не перестраивает). nil — не обновлять.
	ParamsViaTunnel *ParamsClient

	// Guard и Captcha — необязательны, оба nil-safe на стороне движка
	// (engine/session.go: guard==nil → «сеть не менялась»; engine/
	// captcha.go: solver==nil → капча просто проваливает попытку, не
	// паника — сверено по коду 24.09). Прототип этапа 1 (консоль, без
	// интерфейса) их не даёт; платформа с настоящим UI (или своим
	// NetworkGuard) подставляет сюда свою реализацию.
	Guard   NetworkGuard
	Captcha CaptchaSolver

	// TunnelDNS — адреса DNS, которые платформа ставит на интерфейс
	// туннеля (engine-v1.3.0, кот3). Пусто — движок берёт свои
	// DefaultTunnelDNS (1.1.1.1, 8.8.8.8). Движок НИКОГДА не пускает эти
	// адреса в ExcludeHost сам (defense в глубину поверх собственного
	// фильтра платформы, решение владельца 24.09 — п.2, ни один из двух
	// фильтров не заменяет другой, оба должны совпадать с тем, что
	// платформа реально прописывает интерфейсу тем же списком).
	TunnelDNS []string

	// Ladder — Known/SkipDirect/SkipStreams (память путей клиента) и
	// Hashes (нормализованные ссылки VK, core.ParseHashes). Nodes/Relay в
	// опции не входят — те строит BuildLadder сам из ответа /v1/params.
	// Known/SkipDirect/SkipStreams Connect ЗАПОЛНЯЕТ САМ из PathMemory
	// перед вызовом BuildLadder — то, что здесь задаст вызывающий,
	// используется только пока PathMemory == nil или не знает эту сеть.
	Ladder LadderOptions

	// PathMemory — память удачного пути (core/pathmemory.go, порт
	// PathMemory.kt с Android): Connect читает её ДО BuildLadder, чтобы
	// заполнить Ladder.Known/SkipDirect/SkipStreams, и пишет в неё ПОСЛЕ
	// исхода (что победило, не довезла ли лестница целиком). nil —
	// прежнее поведение, лестница строится как раньше, без памяти
	// (meridian-cli/tray-prototype её пока не подключают).
	PathMemory *PathMemory

	// Reconnect — этот вызов Connect идёт из повторной попытки
	// core.KeepConnected (человек ничего не нажимал), а не со свежего
	// подключения. Только для PathMemory: свежее подключение пробует
	// прямой путь заново (сбрасывает счётчик немоты, см. Connect) —
	// внутри одной сессии переподключений счётчик копится как обычно.
	Reconnect bool

	// TunName — пожелание к имени TUN-интерфейса; пусто — платформа
	// выбирает сама (на macOS ядро подставляет номер: "utun" → "utunN").
	TunName string
	// TunMTUCeiling — потолок буферов TUN; 0 — берётся defaultTunMTUCeiling.
	TunMTUCeiling int

	ParamsEnvelope map[string]string

	// DirectDomains — прямые домены белого режима (этап 3, core.
	// DirectRouter). Пусто — всё в туннель, как раньше.
	DirectDomains []string

	// TunnelOnly — чёрный режим (core.TunnelRouter): в туннель только
	// TunnelDomains, остальное напрямую; пустой список — в туннель ничего.
	// Вместе с DirectDomains — отказ (режимы взаимоисключающие).
	TunnelOnly    bool
	TunnelDomains []string

	// RUDirect — «RU-адреса напрямую» (core.RUDirectRouter, rudirect):
	// российские блоки маршрутом мимо туннеля, российские суффиксы — к
	// DirectDomains. В чёрном режиме не действует: там и так мимо всё,
	// кроме списка.
	RUDirect bool

	// AntiDPI — обход DPI-блокировок в чёрном режиме (core.AntiDPIRouter):
	// действует только вместе с TunnelOnly, см. комментарий там же.
	AntiDPI AntiDPIGroups

	// TunnelApps — приложения, трафик которых идёт в туннель (core.
	// AppSplitRouter, appsplit.go): путь к exe или каталог с `\` на конце.
	// Действует только вместе с TunnelOnly.
	TunnelApps []string

	// DionRooms/RelayPrimary — см. ControlConnectPayload. Пока движок
	// (кот 3) не умеет DION, Connect их не использует.
	DionRooms    []string
	RelayPrimary string
}

// Session — одна поднятая сессия прототипа: журнал, движок, TUN.
// Disconnect — штатное отключение (план, раздел 4), идемпотентно.
type Session struct {
	store    *Store
	platform Platform
	engine   EngineHandle
	tun      Tun
	st       *State

	// Events — канал событий движка (engine/desktop.Event), пробрасывается
	// наружу как есть: cmd/ решает, что с ним делать (лог, Kind="ready").
	Events <-chan EngineEvent

	// LadderInfo — что вернул BuildLadder: буквы/id ступеней (для памяти
	// путей на будущее), NoHashes, RelayTarget, Skipped — cmd/ решает,
	// что с этим делать (лог, предупреждение пользователю).
	LadderInfo *LadderInfo

	// TunnelInfo — что вернул движок (Handle.Info()): адрес/шлюз/маска/
	// MTU туннеля. Для журнала это не нужно (Platform.Apply сам решает,
	// что писать в State), здесь — только для наблюдаемости у cmd/.
	TunnelInfo TunnelInfo

	// Для speedGuard (runSpeedGuard): память путей, её решение по этой
	// сети и буква победившей ступени.
	pm        *PathMemory
	pmOutcome pathMemoryOutcome
	winner    string
	winnerTCP bool
	// winnerHost — адрес входа победившей прямой ступени ("" — релей);
	// startedAt — когда сессия поднялась (немота входа, noteSessionEnd).
	winnerHost string
	startedAt  time.Time
	// speedSwitched — сессию остановил сам сторож скорости (не обрыв).
	speedSwitched atomic.Bool

	// antiDPI — не nil, только если ApplyAntiDPI реально встал (см. Connect):
	// не звать StopAntiDPI попусту, когда его и не поднимали.
	antiDPI AntiDPIRouter
	// appSplit — не nil, только если ApplyAppSplit реально встал.
	appSplit AppSplitRouter

	disconnectOnce sync.Once
	disconnectErr  error

	// Повторный резолв прямых доменов (этап 3) — живёт ровно до
	// Disconnect: тот сначала гасит его и ждёт, потом Restore (иначе
	// RefreshDirect мог бы дописать маршрут в журнал посреди снятия).
	refreshCancel context.CancelFunc
	paramsCancel  context.CancelFunc
	paramsDone    chan struct{}
	refreshDone   chan struct{}
}

// DirectRefreshInterval — как часто пересматривать адреса прямых
// доменов (CDN меняют их). Переменная — ради тестов.
var DirectRefreshInterval = 60 * time.Second

// Connect — порядок подключения целиком (план, раздел 1): ключ из файла
// → /v1/params → Ladder (core.BuildLadder) → снимок сети → журнал →
// TUN → движок → настройка TUN/маршрутов/DNS (Platform.Apply). Каждый
// шаг — после того, как предыдущий подтверждённо прошёл; при ошибке
// позже снимка журнал убирается за собой (что не поставлено — снимать
// нечего).
// directRouterNeeded checks whether the platform must route direct domains.
func directRouterNeeded(manualDomains []string, ruDirectApplicable bool) bool {
	return len(manualDomains) > 0 || ruDirectApplicable
}

func Connect(
	ctx context.Context,
	store *Store,
	platform Platform,
	eng EngineStarter,
	paramsClient ParamsFetcher,
	opts ConnectOptions,
) (*Session, error) {
	key, err := resolveKey(opts)
	if err != nil {
		return nil, fmt.Errorf("Connect: %w", err)
	}
	// Прямые домены без поддержки платформы — отказ ДО любых изменений
	// в системе, вслух: туннелировать всё молча, когда просили часть
	// пустить напрямую, — не то, что человек выбрал.
	// White-mode direct domains require platform support before network changes.
	var ruDirect RUDirectRouter
	if opts.RUDirect && !opts.TunnelOnly {
		rr, ok := platform.(RUDirectRouter)
		if !ok {
			return nil, fmt.Errorf("Connect: платформа не умеет «RU-адреса напрямую»")
		}
		ruDirect = rr
	}
	var direct DirectRouter
	ruDirectApplicable := opts.RUDirect && !opts.TunnelOnly
	if directRouterNeeded(opts.DirectDomains, ruDirectApplicable) {
		dr, ok := platform.(DirectRouter)
		if !ok {
			return nil, fmt.Errorf("Connect: платформа не умеет прямые домены (белый режим) — список из %d доменов не применить", len(opts.DirectDomains))
		}
		direct = dr
	}
	var tunnelOnly TunnelRouter
	if opts.TunnelOnly {
		if len(opts.DirectDomains) > 0 {
			return nil, fmt.Errorf("Connect: чёрный режим (только список в туннель) и прямые домены белого режима вместе не бывают")
		}
		tr, ok := platform.(TunnelRouter)
		if !ok {
			return nil, fmt.Errorf("Connect: платформа не умеет чёрный режим (только список в туннель)")
		}
		tunnelOnly = tr
	}

	var antiDPI AntiDPIRouter
	if opts.TunnelOnly && opts.AntiDPI.any() {
		ad, ok := platform.(AntiDPIRouter)
		if !ok {
			return nil, fmt.Errorf("Connect: платформа не умеет обход DPI-блокировок (чёрный режим)")
		}
		antiDPI = ad
	}

	var appSplit AppSplitRouter
	if opts.TunnelOnly && len(NormalizeApps(opts.TunnelApps)) > 0 {
		as, ok := platform.(AppSplitRouter)
		if !ok {
			return nil, fmt.Errorf("Connect: платформа не умеет раздельное туннелирование по приложениям (чёрный режим)")
		}
		appSplit = as
	}

	st := &State{PID: os.Getpid(), StartedAt: time.Now()}
	if err := platform.Snapshot(ctx, st); err != nil {
		return nil, fmt.Errorf("Connect: снимок сети: %w", err)
	}
	commit := func() error { return store.Save(st) }
	if err := commit(); err != nil {
		return nil, fmt.Errorf("Connect: журнал: %w", err)
	}

	// ДО первого сетевого похода: и engine/params.Client.Fetch, и
	// движок зовут необязательный ExcludeHost (engine-v1.2.0) — Fetch
	// уже на этом самом вызове (адрес API), движок — чуть позже, во
	// время рукопожатия. Живой прогон 24.09 поймал первую версию этого
	// порядка: AttachJournal стоял ПОСЛЕ Fetch, и SocketProtector.
	// ExcludeHost без журнала намеренно отказывает совсем (не журналировать
	// и всё равно ставить маршрут — значит потерять его при Restore) —
	// исключение для адреса API попросту не встало, только легло строкой
	// в лог «журнал ещё не подключён».
	platform.AttachJournal(st, commit)

	// abort — откат того, что успело встать с этого места: уже Fetch
	// ставит исключения /32 (ExcludeHost для адреса API, в журнал), а
	// следующая попытка Connect ПЕРЕЗАПИСЫВАЕТ журнал своим снимком —
	// без Restore здесь эти маршруты терялись бы навсегда и копились у
	// долгоживущего помощника (этап 2) с каждой неудачной попыткой.
	// Restore на частичном журнале безопасен — каждый его шаг под своим
	// флагом. Фоновый контекст — ctx мог быть уже отменён.
	abort := func() {
		_ = platform.Restore(context.Background(), st)
		_ = store.Remove()
	}

	answer, pool, err := fetchStartup(ctx, paramsClient, key, opts.DeviceID)
	fromCache := false
	if err == nil {
		if previous, _, e := opts.ParamsCache.Load(key); e == nil && previous.RoutingScope == "partner" && answer.RoutingScope != "partner" {
			abort()
			return nil, errors.New("Connect: служба не подтвердила партнёрский маршрут")
		}
		opts.ParamsCache.Save(key, answer.Raw)
	} else if paramsFallbackAllowed(err) {
		// Под белыми списками API молчит, а релей через VK/DION жив:
		// работаем по последнему удачному ответу этого ключа.
		cached, at, cerr := opts.ParamsCache.Load(key)
		var scopeErr *ParamsError
		partnerRefusal := errors.As(err, &scopeErr) && scopeErr.RoutingScope == "partner"
		if cerr == nil && cachedScopeAllowed(cached, err) && paramsCacheUsable(at, opts) {
			fmt.Fprintf(os.Stderr, "/v1/params недоступен (%v) — работаю по сохранённым параметрам (получены %s назад)\n", err, paramsAge(at))
			answer, err = cached, nil
			fromCache = true
		} else if b := builtinRelayOnlyParams(opts); b != nil && cerr == nil && cached.RoutingScope == "ordinary" && !partnerRefusal {
			// Сохранённого нет (первая установка этой версии или ключ
			// сменили), API молчит: белый режим идёт только по релею, ему
			// узлы входа не нужны. Идём на VK/DION встроенным минимумом;
			// шлюз всё равно проверит ключ.
			fmt.Fprintf(os.Stderr, "/v1/params недоступен (%v), сохранённых нет — иду на релей по встроенным параметрам\n", err)
			answer, err = b, nil
			fromCache = true
		}
	}
	if err != nil {
		abort()
		return nil, fmt.Errorf("Connect: /v1/params: %w", err)
	}
	if opts.PartnerRouting && answer.RoutingScope != "partner" {
		abort()
		return nil, errors.New("Connect: служба не подтвердила партнёрский маршрут")
	}
	if !opts.PartnerRouting && answer.RoutingScope == "partner" {
		abort()
		return nil, errors.New("Connect: изменился тип маршрута ключа, переподключитесь")
	}
	if answer.RoutingScope == "partner" {
		opts.Gateway = answer.Gateway
		opts.CachedFallback = ""
		opts.Ladder.NoTURNBackup = true
	}
	// Apply or cache the pool only after the route scope is validated.
	if links := relayPoolForSession(pool, opts.ParamsCache, key, opts.DeviceID); links != nil {
		opts.Ladder.Hashes = addUniqueStrings(opts.Ladder.Hashes, ParseHashes(strings.Join(links.VK, ",")))
		if pool.premium && !opts.DisableAutoDion {
			opts.DionRooms = addUniqueStrings(opts.DionRooms, links.Dion)
		}
	}

	// ПАМЯТЬ ПУТЕЙ (core/pathmemory.go, порт PathMemory.kt с Android) —
	// заполняет Ladder.Known/SkipDirect/SkipStreams ДО сборки лестницы.
	// Ключ сети считается ДО подключения (platform.(NetworkKeyer), а не
	// после): после установления туннеля активной сетью стал бы он сам.
	// Платформа без NetworkKeyer (meridian-cli, tray-prototype пока) или
	// opts.PathMemory==nil — pmOutcome нулевой, вся память путей просто
	// не участвует, лестница строится как раньше.
	pm := opts.PathMemory
	var pmKey string
	if pm != nil {
		if nk, ok := platform.(NetworkKeyer); ok {
			pmKey = nk.NetworkKey()
		}
	}
	// Второй круг гонки не короче 5 с, даже если служба прислала меньше
	// (Android 0.1.231, 03.10: на мобильной сети вход отвечает дольше).
	if answer.Ladder.SecondRoundMs < minSecondRoundMs {
		answer.Ladder.SecondRoundMs = minSecondRoundMs
	}
	nodeHosts := make([]string, 0, len(answer.Nodes))
	for _, n := range answer.Nodes {
		nodeHosts = append(nodeHosts, n.Host)
	}
	pmOutcome := pathMemoryBeforeLadder(pm, pmKey, pathMemoryInputs{
		Reconnect:    opts.Reconnect,
		Mode:         opts.Ladder.Mode,
		RelayEnabled: answer.Relay.Enabled,
		HashesLen:    len(opts.Ladder.Hashes) + len(dionRoomsForLadder(opts.DionRooms)),
		ProbeEvery:   answer.Ladder.DirectProbeEveryAttempt,
		NodeHosts:    nodeHosts,
	})
	pmOutcome.applyToLadder(&opts.Ladder)
	if len(pmOutcome.avoidHosts) > 0 {
		answer.Nodes = pmOutcome.filterNodes(answer.Nodes)
		fmt.Fprintf(os.Stderr, "память путей: обхожу онемевшие входы (%d), осталось входов %d из %d\n", len(pmOutcome.avoidHosts), len(answer.Nodes), len(nodeHosts))
	}

	ladder, ladderInfo, err := BuildLadder(answer, opts.Ladder)
	if err != nil {
		abort()
		return nil, fmt.Errorf("Connect: сборка лестницы: %w", err)
	}
	if LadderDiagnostics {
		for _, node := range answer.Nodes {
			if letter, ok := ladderInfo.Letters[node.ID]; ok {
				transport := node.Transport
				if transport != "tcp" && transport != "tls" && transport != "udp" {
					transport = "другой"
				}
				log.Printf("лестница: ступень %s: транспорт %s", letter, transport)
			}
		}
		log.Printf("лестница: пропуск UDP=%t, потоков=%t", opts.Ladder.SkipDirect, opts.Ladder.SkipStreams)
	}
	// Комнаты DION — запасной источник кредов релея после VK (engine-v1.9.0,
	// кот 3). В params.LadderOptions поля нет: только AddRelayDion на уже
	// собранную лестницу.
	dionSlugs := dionRoomsForLadder(opts.DionRooms)
	for _, slug := range dionSlugs {
		ladder.AddRelayDion(slug)
	}
	// Основное звено (engine-v1.10.0): DION первым, только если его выбрали
	// и комнаты есть; живость комнаты проверяет интерфейс до отправки.
	setRelayPrimaryDion(EffectiveRelayPrimary(opts.RelayPrimary, dionSlugs) == RelayPrimaryDion)

	mtuCeiling := opts.TunMTUCeiling
	if mtuCeiling == 0 {
		mtuCeiling = defaultTunMTUCeiling
	}
	t, err := platform.NewTun(opts.TunName, mtuCeiling)
	if err != nil {
		abort()
		return nil, fmt.Errorf("Connect: TUN: %w", err)
	}

	cfg := EngineConfig{
		Gateway:        opts.Gateway,
		CachedFallback: opts.CachedFallback,
		Ladder:         ladder,
		Password:       key,
		DeviceID:       opts.DeviceID,
		Protector:      platform.Protector(),
		Guard:          opts.Guard,
		Captcha:        opts.Captcha,
		TunnelDNS:      opts.TunnelDNS,
		// ClampMSS — решение владельца 24.09, engine-v1.4.0: настольный
		// путь всегда со своим MTU туннеля (в отличие от Android/iOS,
		// где его задаёт платформа) — опускать MSS в TCP SYN/SYN-ACK,
		// не полагаться на то, что удалённая сторона сама подберёт
		// правильный размер по PMTU.
		ClampMSS: true,
	}
	handle, events, err := eng.Start(ctx, cfg, t)
	if err != nil {
		logLadderEvents(events)
		t.Close()
		abort()
		pmOutcome.onLadderFailed(pm)
		return nil, fmt.Errorf("Connect: движок: %w", err)
	}
	winner := handle.Winner()
	if LadderDiagnostics {
		log.Printf("лестница: выбран путь %q (поток=%t)", winner, ladderInfo.Streams[winner])
	}
	pmOutcome.onWinner(pm, winner, ladderInfo)
	winnerHost := nodeHost(answer.Nodes, ladderInfo, winner)
	winnerTCP := false
	for _, node := range answer.Nodes {
		if ladderInfo.Letters[node.ID] == winner && node.Transport == "tcp" {
			winnerTCP = true
			break
		}
	}

	// The common direct-domain list applies in white mode regardless of the
	// transport that won. DirectMatcher still honours the exclusion list.
	if ruDirect != nil {
		opts.DirectDomains = append(append([]string(nil), opts.DirectDomains...), rudirect.Domains...)
	}

	info := handle.Info()
	apply := platform.Apply
	if tunnelOnly != nil {
		apply = func(ctx context.Context, st *State, t Tun, info TunnelInfo, commit func() error) error {
			return tunnelOnly.ApplyTunnelOnly(ctx, st, t, info, opts.TunnelDomains, commit)
		}
	}
	if err := apply(ctx, st, t, info, commit); err != nil {
		handle.Stop()
		t.Close()
		abort()
		return nil, fmt.Errorf("Connect: настройка сети: %w", err)
	}
	if err := commit(); err != nil {
		// Подключение уже встало физически — журнал не обновился, но
		// откатывать вслепую то, что реально работает, хуже, чем громко
		// сообщить и оставить решение вызывающему (он видит ошибку).
		return nil, fmt.Errorf("Connect: подключено, но журнал не обновился (%w) — свериться и снять руками при необходимости", err)
	}

	sess := &Session{store: store, platform: platform, engine: handle, tun: t, st: st, Events: events, LadderInfo: ladderInfo, TunnelInfo: info, pm: pm, pmOutcome: pmOutcome, winner: winner, winnerTCP: winnerTCP, winnerHost: winnerHost, startedAt: time.Now()}

	// Адресные диапазоны RU больше не ставим (решение владельца 18.10.2026):
	// в белом режиме напрямую только корни белого списка по ответам DNS
	// туннеля (rudirect.Domains) минус исключения (rudirect.Exclusions).

	if antiDPI != nil {
		// Тот же принцип, что у RUDirect выше: не встал обход — туннель по
		// списку всё равно работает, это лучше, чем ничего.
		if err := antiDPI.ApplyAntiDPI(st, opts.AntiDPI); err != nil {
			fmt.Fprintf(os.Stderr, "Обход DPI-блокировок: %v\n", err)
		} else {
			sess.antiDPI = antiDPI
		}
	}

	if appSplit != nil {
		// Не встало — туннель по списку всё равно работает (как AntiDPI).
		if err := appSplit.ApplyAppSplit(st, t, info, NormalizeApps(opts.TunnelApps)); err != nil {
			fmt.Fprintf(os.Stderr, "Приложения в туннель: %v\n", err)
		} else {
			sess.appSplit = appSplit
		}
	}

	var refresh func(ctx context.Context)
	if direct != nil {
		if err := direct.ApplyDirect(ctx, st, opts.DirectDomains, commit); err != nil {
			// Туннель уже стоит — снимаем всё целиком: Connect либо
			// выполняет то, что просили, либо не оставляет ничего.
			_ = sess.Disconnect(context.Background())
			return nil, fmt.Errorf("Connect: прямые домены: %w", err)
		}
		domains := append([]string(nil), opts.DirectDomains...)
		refresh = func(ctx context.Context) { _ = direct.RefreshDirect(ctx, domains) }
	}
	if tunnelOnly != nil && len(opts.TunnelDomains) > 0 {
		domains := append([]string(nil), opts.TunnelDomains...)
		refresh = func(ctx context.Context) { _ = tunnelOnly.RefreshTunnelOnly(ctx, domains) }
	}
	if refresh != nil {
		rctx, cancel := context.WithCancel(context.Background())
		sess.refreshCancel = cancel
		sess.refreshDone = make(chan struct{})
		go func() {
			defer close(sess.refreshDone)
			tick := time.NewTicker(DirectRefreshInterval)
			defer tick.Stop()
			for {
				select {
				case <-rctx.Done():
					return
				case <-tick.C:
					refresh(rctx)
				}
			}
		}()
	}

	// Пока сессия жива, сохранённый ответ держится свежим: набор входов у
	// ключа меняется (неделя на трёх адресах, потом все), а при молчащем API
	// следующий старт пойдёт по сохранённому. Стартовали по сохранённому —
	// сначала скорые попытки получить настоящий ответ.
	if opts.ParamsCache != nil && (opts.ParamsViaTunnel != nil || paramsClient != nil) {
		fetch := paramsFetchChain(opts.ParamsViaTunnel, paramsClient)
		every := paramsRefreshEvery(answer.TTLSeconds)
		pctx, cancel := context.WithCancel(context.Background())
		sess.paramsCancel = cancel
		sess.paramsDone = make(chan struct{})
		go func() {
			defer close(sess.paramsDone)
			if fromCache && !refreshParamsViaTunnel(pctx, fetch, opts.ParamsCache, key, paramsRefreshDelays) {
				return
			}
			keepParamsFresh(pctx, fetch, opts.ParamsCache, key, every, paramsRetryEvery)
		}()
	}

	return sess, nil
}

// Disconnect — штатное отключение (план, раздел 4): обратный порядок,
// идемпотентно (повторный вызов — не ошибка, просто ничего не делает).
func (s *Session) Disconnect(ctx context.Context) error {
	s.disconnectOnce.Do(func() {
		if s.refreshCancel != nil {
			s.refreshCancel()
			<-s.refreshDone
		}
		if s.paramsCancel != nil {
			s.paramsCancel()
			<-s.paramsDone
		}
		if s.antiDPI != nil {
			s.antiDPI.StopAntiDPI()
		}
		if s.appSplit != nil {
			s.appSplit.StopAppSplit()
		}
		s.engine.Stop()
		s.tun.Close()
		if err := s.platform.Restore(ctx, s.st); err != nil {
			s.disconnectErr = fmt.Errorf("Disconnect: %w", err)
			return
		}
		if err := s.store.Remove(); err != nil {
			s.disconnectErr = fmt.Errorf("Disconnect: журнал не удалился: %w", err)
		}
	})
	return s.disconnectErr
}

// EnsureClean — точка входа в начало каждого запуска (план, раздел 5):
// журнала нет → можно подключаться; журнал есть и его процесс жив →
// отказ «уже запущен» (защита от повторного входа); журнал есть, а
// процесс мёртв → восстановление по журналу, потом можно подключаться.
func EnsureClean(ctx context.Context, store *Store, platform Platform) error {
	st, err := store.Load()
	if err != nil {
		return fmt.Errorf("EnsureClean: %w", err)
	}
	if st == nil {
		return nil
	}
	if platform.ProcessAlive(st.PID) {
		return fmt.Errorf("уже запущен: журнал %s принадлежит живому процессу %d", store.Path, st.PID)
	}
	return restoreFromJournal(ctx, store, platform, st)
}

// Cleanup — команда `meridian-cli cleanup`: то же восстановление, но
// без проверки живого процесса (план, раздел 5) — пользователь просит явно.
func Cleanup(ctx context.Context, store *Store, platform Platform) error {
	st, err := store.Load()
	if err != nil {
		return fmt.Errorf("Cleanup: %w", err)
	}
	if st == nil {
		return nil
	}
	return restoreFromJournal(ctx, store, platform, st)
}

func restoreFromJournal(ctx context.Context, store *Store, platform Platform, st *State) error {
	if err := platform.Restore(ctx, st); err != nil {
		return fmt.Errorf("восстановление не полное, журнал %s оставлен: %w", store.Path, err)
	}
	if err := store.Remove(); err != nil {
		return fmt.Errorf("восстановление прошло, но журнал не удалился: %w", err)
	}
	return nil
}

// KeepConnected держит подключение, автоматически переподключаясь
// после НЕПРЕДНАМЕРЕННОГО обрыва: движок сам не восстанавливает сессию
// (engine/engine.go, кот3: «сокет мёртв достоверно, и приложение
// обязано переподключаться само») — это делает эта функция, а не
// голый Connect. Возвращается штатно, когда ctx отменён (истёк срок
// или пришёл сигнал) — тогда сессия отключается сама и функция
// возвращает nil (или ctx.Err(), если он не context.Canceled).
//
// Различение «мы остановили» / «сессия умерла сама»: KeepConnected НЕ
// зовёт sess.Disconnect, пока сама не решит остановиться (по ctx) —
// значит если канал Events закрылся раньше, чем сработал ctx.Done(),
// закрыл его не наш код, а движок изнутри (ошибка сокета, обрыв сети и
// т.п.) — это и есть непреднамеренный обрыв, за него счётчик растёт.
//
// onEvent — необязательный проброс событий движка наружу (лог у cmd/).
// onReconnect(n) — необязательный, зовётся после КАЖДОГО обнаруженного
// непреднамеренного обрыва, n — счётчик с 1 (для отчёта в проверке
// этапа 1 — «число переподключений движка»).
func KeepConnected(
	ctx context.Context,
	store *Store,
	platform Platform,
	eng EngineStarter,
	paramsClient ParamsFetcher,
	opts ConnectOptions,
	onConnected func(*Session),
	onEvent func(EngineEvent),
	onReconnect func(n int),
) error {
	reconnects := 0
	for {
		attempt := opts
		attempt.Reconnect = reconnects > 0
		sess, err := Connect(ctx, store, platform, eng, paramsClient, attempt)
		if err != nil {
			return fmt.Errorf("KeepConnected: попытка %d: %w", reconnects+1, err)
		}
		if onConnected != nil {
			onConnected(sess)
		}

		guardCtx, stopGuard := context.WithCancel(ctx)
		guardDone := make(chan struct{})
		go func() {
			defer close(guardDone)
			sess.runSpeedGuard(guardCtx)
		}()
		unexpected := drainUntilClosedOrDone(ctx, sess.Events, onEvent)
		stopGuard()
		<-guardDone
		sess.noteSessionEnd(unexpected, time.Since(sess.startedAt))

		if err := sess.Disconnect(context.Background()); err != nil {
			if onEvent != nil {
				onEvent(EngineEvent{Kind: "log", Reason: fmt.Sprintf("KeepConnected: Disconnect: %v", err)})
			}
		}

		if !unexpected {
			// ctx.Done() сработал — не важно, почему (Canceled,
			// DeadlineExceeded из -duration, отмена родителя): в любом
			// случае это «нас попросили остановиться», не ошибка.
			return nil
		}

		reconnects++
		if onReconnect != nil {
			onReconnect(reconnects)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Пауза перед повтором — не долбить шлюз/API в плотном цикле,
		// если обрывы идут подряд (план этапа 1: одна тяжёлая работа за
		// раз, тот же принцип — не создавать вторую нагрузку поверх ещё
		// не осевшей первой).
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil
		}
	}
}

// RunControlConnect — core.KeepConnected под помощника/службу (этап 2,
// решение владельца 24.09: протокол ОБЩИЙ для Mac и Windows, живёт в
// core/, платформа не пишет свой перевод колбэков в сообщения —
// пишет только транспорт). send — единственное, что platform/darwin
// и platform/windows подставляют сами (обычно тонкая обёртка вокруг
// core.WriteControlMessage поверх своего net.Conn/канала); ошибка
// send НЕ прерывает саму VPN-сессию (UI мог на секунду отвалиться —
// это не повод рвать туннель) — RunControlConnect её просто
// проглатывает, платформа при желании логирует сама через свою send.
//
// Четыре момента, все — из core.KeepConnected как есть, ничего не
// добавляет от себя:
//  1. onConnected → ControlState{Connected:true, TunnelInfo, Reconnects}
//     (Reconnects — текущий счётчик, 0 на самом первом подключении).
//  2. onEvent → ControlEvent с EngineEvent как есть.
//  3. onReconnect(n) → ControlReconnecting{Attempt:n} — ПЕРЕХОДНОЕ,
//     не ControlState (иначе интерфейс не отличит "рвём, пробуем
//     снова" от окончательного разрыва — оба Connected:false).
//  4. KeepConnected вернулся: err==nil (штатная остановка, ctx отменён
//     по любой причине) → финальный ControlState{Connected:false,
//     Reconnects: итог}; err!=nil (первая попытка или один из
//     повторов не прошли, KeepConnected сдался) → ControlError{
//     ControlErrConnectFailed, err.Error()} — НЕ ControlState, чтобы
//     интерфейс не спутал настоящий сбой со штатным "пользователь сам
//     отключился".
func RunControlConnect(
	ctx context.Context,
	store *Store,
	platform Platform,
	eng EngineStarter,
	paramsClient ParamsFetcher,
	opts ConnectOptions,
	send func(ControlMessageType, any) error,
) error {
	onConnected, onEvent, onReconnect, reconnects := buildControlCallbacks(send)

	err := KeepConnected(ctx, store, platform, eng, paramsClient, opts, onConnected, onEvent, onReconnect)
	if err != nil {
		var se *EngineStartError
		failure := ""
		var pe *ParamsError
		switch {
		case errors.As(err, &se):
			failure = se.Failure
		case errors.As(err, &pe) && pe.Kind == ParamsKindUnavailable:
			failure = FailureAPIUnavailable
		}
		_ = send(ControlError, ControlErrorPayload{Code: ControlErrConnectFailed, Message: err.Error(), Failure: failure})
		return err
	}
	_ = send(ControlState, ControlStatePayload{Connected: false, Reconnects: *reconnects})
	return nil
}

// buildControlCallbacks — сам перевод колбэков KeepConnected в
// сообщения, вынесен из RunControlConnect отдельной функцией ради
// теста: KeepConnected требует настоящую сеть (core.Connect →
// paramsClient.Fetch — та же граница, что документирована у
// drainUntilClosedOrDone), а эта часть логики — чистая, sess/ev
// приходят готовыми, никакой сети не трогает. reconnects возвращается
// указателем — RunControlConnect читает его финальное значение уже
// после KeepConnected, для последнего ControlState.
func buildControlCallbacks(send func(ControlMessageType, any) error) (
	onConnected func(*Session),
	onEvent func(EngineEvent),
	onReconnect func(n int),
	reconnects *int,
) {
	reconnects = new(int)
	onConnected = func(sess *Session) {
		info := sess.TunnelInfo
		_ = send(ControlState, ControlStatePayload{
			Connected:  true,
			TunnelInfo: &info,
			Reconnects: *reconnects,
			Winner:     sess.winner,
		})
	}
	onEvent = func(ev EngineEvent) {
		logLadderEvent(ev)
		_ = send(ControlEvent, ev)
	}
	onReconnect = func(n int) {
		*reconnects = n
		_ = send(ControlReconnecting, ControlReconnectingPayload{Attempt: n})
	}
	return onConnected, onEvent, onReconnect, reconnects
}

// drainUntilClosedOrDone читает events, пока не случится одно из двух:
// канал закрылся сам (движок счёл сессию мёртвой — возвращает true,
// «непреднамеренно») или ctx отменён (мы решили остановиться —
// возвращает false). Вынесена из KeepConnected отдельной функцией ради
// теста: она не создаёт сессию и не трогает сеть/диск, только канал и
// контекст — как раз то новое, что стоит проверить прицельно, без
// границы «Connect требует настоящий /v1/params» (core/session_test.go).
func drainUntilClosedOrDone(ctx context.Context, events <-chan EngineEvent, onEvent func(EngineEvent)) (unexpected bool) {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return true // канал закрылся сам — движок счёл сессию мёртвой
			}
			if onEvent != nil {
				onEvent(ev)
			}
		case <-ctx.Done():
			return false
		}
	}
}

func readKeyFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("чтение ключа %s: %w", path, err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("файл ключа %s пуст", path)
	}
	return key, nil
}

// resolveKey — источник ключа подписчика: KeyMaterial (решение
// владельца 24.09, этап 2 — интерфейс достаёт ключ из Keychain и шлёт
// помощнику по ControlConnectPayload.KeyMaterial, см. core/control.go)
// имеет приоритет, если непусто; иначе — старый путь через файл
// (KeyPath), которым по-прежнему пользуется консольный meridian-cli
// этапа 1 (не сломан этим изменением).
func resolveKey(opts ConnectOptions) (string, error) {
	if opts.KeyMaterial != "" {
		return opts.KeyMaterial, nil
	}
	return readKeyFile(opts.KeyPath)
}
