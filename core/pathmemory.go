package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// NetworkKeyer — НЕОБЯЗАТЕЛЬНЫЙ интерфейс платформы: ключ ТЕКУЩЕЙ сети,
// порт PathMemory.networkKey (Android). Платформы без него (core.Connect
// делает type assertion, как и у RUDirectRouter/DirectRouter) просто не
// участвуют в памяти путей — лестница строится как раньше, без
// Known/SkipDirect/SkipStreams.
//
// Ключ грубый нарочно (см. PathMemory.kt): не SSID и не точный
// идентификатор адаптера, а транспорт+подсеть — тех же двух домашних
// сетей 192.168.1.0/24 достаточно, чтобы слиться в один ключ, и цена
// этой ошибки мала (лестница просто пойдёт по обычному порядку).
// Считается ДО подключения (core.Connect, до BuildLadder) — после
// установления туннеля активной сетью стал бы сам туннель.
type NetworkKeyer interface {
	NetworkKey() string
}

// PathMemory — память удачного пути, порт org.meridianvpn.app.PathMemory
// (Android/Kotlin) на настольный клиент. Разбор порогов и самой идеи —
// там же, здесь только различия платформы.
//
// Ключ — СЕТЬ (core.NetworkKeyer, платформа), а не устройство: дома и в
// дороге рабочие ступени разные. Хранилище — не SharedPreferences (её
// на Windows/macOS нет), а свой JSON-файл, записываемый атомарно
// (temp+rename): это системная служба, а не активность с гарантированным
// onDestroy, падение процесса посреди записи — обычный случай, который
// нельзя допустить порчей файла целиком.
//
// Как и у Android, память — УДОБСТВО, А НЕ НЕОБХОДИМОСТЬ: любая ошибка
// чтения/записи файла проглатывается, работа с памятью в этом случае
// продолжается только в оперативной части (текущий процесс её видит,
// следующий запуск — нет). Ронять или тормозить подключение из-за диска
// не тот обмен, который здесь нужен.
type PathMemory struct {
	path string

	mu   sync.Mutex
	data map[string]*pathMemoryEntry
}

// pathMemoryEntry — то, что PathMemory.kt хранит парой ключей на сеть:
// remembered (String) + relayStreak/streamMuteStreak/directMuteStreak
// (Int, каждый свой SharedPreferences-ключ с суффиксом). Здесь — одна
// запись в JSON, экономнее и проще держать согласованно.
type pathMemoryEntry struct {
	Remembered       string `json:"remembered,omitempty"`
	RelayStreak      int    `json:"relay_streak,omitempty"`
	StreamMuteStreak int    `json:"stream_mute_streak,omitempty"`
	DirectMuteStreak int    `json:"direct_mute_streak,omitempty"`
	// SlowKbps — замеренная скорость медленного прямого пути/потока на
	// этой сети (кбит/с), 0 — пометки нет (PathMemory.kt: SLOW_SUFFIX).
	// SlowN — сколько подъёмов прошло с пометки (для пробы раз в
	// probeEvery попыток, SLOW_N_SUFFIX).
	SlowKbps int `json:"slow_kbps,omitempty"`
	SlowN    int `json:"slow_n,omitempty"`
	// SlowAt — когда поставлена пометка (unix мс): её снимает только время
	// (slowTTL), не замер релея. SpeedSwitchAt — когда сеть последний раз
	// меняла путь из-за скорости (не чаще раза в speedSwitchGap).
	SlowAt        int64 `json:"slow_at,omitempty"`
	SpeedSwitchAt int64 `json:"speed_switch_at,omitempty"`
	// MuteHosts — входы (адреса узлов), на которых прямой путь этой сети
	// поднялся и онемел; MuteN — подключений с последней пометки (проба
	// онемевших раз в probeEvery). Порт PathMemory.slowHosts (Android 03.10).
	MuteHosts []string `json:"mute_hosts,omitempty"`
	MuteN     int      `json:"mute_n,omitempty"`
}

// isZero — пустую запись можно не хранить вовсе (как Android не хранит
// ключ, у которого ничего не установлено).
func (e *pathMemoryEntry) isZero() bool {
	return e.Remembered == "" && e.RelayStreak == 0 && e.StreamMuteStreak == 0 && e.DirectMuteStreak == 0 &&
		e.SlowKbps == 0 && e.SlowN == 0 && e.SlowAt == 0 && e.SpeedSwitchAt == 0 &&
		len(e.MuteHosts) == 0 && e.MuteN == 0
}

// NewPathMemory — память путей с хранением в файле path. Файл читается
// лениво, при первом обращении (не здесь): NewPathMemory сама никогда не
// возвращает ошибку — платформа не обязана проверять диск, чтобы
// подключиться.
func NewPathMemory(path string) *PathMemory {
	return &PathMemory{path: path}
}

// load — читает файл один раз за время жизни процесса (data != nil —
// уже читали, повторно не лезем). Отсутствующий или битый файл —
// не ошибка, просто пустая память (см. философию пакета выше).
func (p *PathMemory) load() {
	if p.data != nil {
		return
	}
	p.data = map[string]*pathMemoryEntry{}
	if p.path == "" {
		return
	}
	raw, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(raw, &p.data)
	if p.data == nil {
		p.data = map[string]*pathMemoryEntry{}
	}
}

// persist — атомарная запись (temp-файл в той же директории + rename):
// процесс службы может упасть или быть убит ровно между write и close,
// прямая перезапись оставила бы файл битым НАВСЕГДА (в отличие от
// SharedPreferences.apply(), которая на Android так себя не ведёт).
// Ошибка молча проглатывается — см. философию пакета: диск подвёл,
// подключение всё равно должно идти.
func (p *PathMemory) persist() {
	if p.path == "" {
		return
	}
	raw, err := json.Marshal(p.data)
	if err != nil {
		return
	}
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".pathmemory-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(raw)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, p.path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// entry — запись для key, создаётся при первом обращении на запись.
// Вызывающий обязан держать мьютекс.
func (p *PathMemory) entry(key string) *pathMemoryEntry {
	e, ok := p.data[key]
	if !ok {
		e = &pathMemoryEntry{}
		p.data[key] = e
	}
	return e
}

// dropIfZero — как и Android (там просто отсутствие ключа в
// SharedPreferences), пустую запись не держим — иначе файл памяти рос
// бы сетями, где сейчас ничего не отмечено.
func (p *PathMemory) dropIfZero(key string) {
	if e, ok := p.data[key]; ok && e.isZero() {
		delete(p.data, key)
	}
}

// Remembered — запомненная ступень для этой сети, или "" (Kotlin
// возвращал null — здесь пустая строка, тот же смысл: "ничего").
func (p *PathMemory) Remembered(key string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return e.Remembered
	}
	return ""
}

// Remember запоминает ступень candidate для сети key.
func (p *PathMemory) Remember(key, candidate string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	p.entry(key).Remembered = candidate
	p.persist()
}

// Forget — забыть и запомненную ступень, и счётчик проигрышей релею
// (relayStreak): PathMemory.kt forget().
func (p *PathMemory) Forget(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.Remembered = ""
		e.RelayStreak = 0
		p.dropIfZero(key)
	}
	p.persist()
}

// ForgetRung — забыть ТОЛЬКО запомненную ступень, счётчики не трогая
// (PathMemory.kt forgetRung): используется, когда запомненная ступень
// не довезла, а сеть саму по себе судить рано.
func (p *PathMemory) ForgetRung(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.Remembered = ""
		p.dropIfZero(key)
	}
	p.persist()
}

// RelayStreak — сколько раз подряд на этой сети победил релей, а не
// прямой UDP.
func (p *PathMemory) RelayStreak(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return e.RelayStreak
	}
	return 0
}

// NoteRelayWon — ещё раз победил не-прямой путь (поток или релей):
// счётчик вверх.
func (p *PathMemory) NoteRelayWon(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	p.entry(key).RelayStreak++
	p.persist()
}

// ClearRelayStreak — счётчик в ноль (прямой ожил, или стратегия
// «сразу на релей» не сработала).
func (p *PathMemory) ClearRelayStreak(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.RelayStreak = 0
		p.dropIfZero(key)
	}
	p.persist()
}

// SkipDirect — пропускать ли прямой путь на этой сети СЕЙЧАС (проигрыш
// гонки: прямой не поднялся). Как и Android: порог плюс пробный заход
// раз в probeEvery попыток.
func (p *PathMemory) SkipDirect(key string, skipAfter, probeEvery int) bool {
	n := p.RelayStreak(key)
	return n >= skipAfter && probeEvery > 0 && n%probeEvery != 0
}

// ProbingDirect — пропуск (по проигрышу гонки) уже включён, но сейчас
// пробный заход щупает прямой.
func (p *PathMemory) ProbingDirect(key string, skipAfter, probeEvery int) bool {
	n := p.RelayStreak(key)
	return n >= skipAfter && probeEvery > 0 && n%probeEvery == 0
}

// StreamMuteStreak — сколько раз подряд поток (TCP/TLS) на этой сети
// поднялся и онемел.
func (p *PathMemory) StreamMuteStreak(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return e.StreamMuteStreak
	}
	return 0
}

// NoteStreamMute — ещё один немой поток.
func (p *PathMemory) NoteStreamMute(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	p.entry(key).StreamMuteStreak++
	p.persist()
}

// ClearStreamMute — счётчик в ноль (поток отработал и не онемел, либо
// пропуск потока не помог и релей тоже не встал).
func (p *PathMemory) ClearStreamMute(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.StreamMuteStreak = 0
		p.dropIfZero(key)
	}
	p.persist()
}

// SkipStreams — пропускать ли потоковые ступени на этой сети СЕЙЧАС.
func (p *PathMemory) SkipStreams(key string, skipAfter, probeEvery int) bool {
	n := p.StreamMuteStreak(key)
	return n >= skipAfter && probeEvery > 0 && n%probeEvery != 0
}

// ProbingStreams — пропуск потока включён, но сейчас пробный заход.
func (p *PathMemory) ProbingStreams(key string, skipAfter, probeEvery int) bool {
	n := p.StreamMuteStreak(key)
	return n >= skipAfter && probeEvery > 0 && n%probeEvery == 0
}

// DirectMuteStreak — сколько раз подряд прямой UDP на этой сети
// поднялся и онемел ПОСРЕДИ сессии (отдельно от проигрыша гонки —
// разбор в PathMemory.kt, DIRECT_MUTE_SUFFIX).
func (p *PathMemory) DirectMuteStreak(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return e.DirectMuteStreak
	}
	return 0
}

// NoteDirectMute — ещё один немой прямой. Переживает Forget() (как и
// у Android): немота прямого пути — отдельная улика от «не поднялся».
func (p *PathMemory) NoteDirectMute(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	p.entry(key).DirectMuteStreak++
	p.persist()
}

// ClearDirectMute — счётчик в ноль (свежее подключение пробует прямой
// заново, либо пропуск не помог и релей тоже не встал).
func (p *PathMemory) ClearDirectMute(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.DirectMuteStreak = 0
		p.dropIfZero(key)
	}
	p.persist()
}

// SkipDirectMute — пропускать ли прямой из-за немоты на этой сети
// СЕЙЧАС. Устроено как SkipStreams.
func (p *PathMemory) SkipDirectMute(key string, skipAfter, probeEvery int) bool {
	n := p.DirectMuteStreak(key)
	return n >= skipAfter && probeEvery > 0 && n%probeEvery != 0
}

// MuteHosts — онемевшие входы этой сети.
func (p *PathMemory) MuteHosts(key string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return append([]string(nil), e.MuteHosts...)
	}
	return nil
}

// NoteMuteHost — вход host онемел на этой сети.
func (p *PathMemory) NoteMuteHost(key, host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e := p.entry(key)
	e.MuteN = 0
	for _, h := range e.MuteHosts {
		if h == host {
			p.persist()
			return
		}
	}
	e.MuteHosts = append(e.MuteHosts, host)
	p.persist()
}

// ClearMuteHost — вход host на этой сети снова держит.
func (p *PathMemory) ClearMuteHost(key, host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e, ok := p.data[key]
	if !ok {
		return
	}
	out := e.MuteHosts[:0]
	for _, h := range e.MuteHosts {
		if h != host {
			out = append(out, h)
		}
	}
	e.MuteHosts = out
	if len(out) == 0 {
		e.MuteHosts, e.MuteN = nil, 0
	}
	p.dropIfZero(key)
	p.persist()
}

// ClearMuteHosts — забыть все онемевшие входы сети (обход не помог).
func (p *PathMemory) ClearMuteHosts(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.MuteHosts, e.MuteN = nil, 0
		p.dropIfZero(key)
	}
	p.persist()
}

// tickMute — ещё одно подключение при помеченных входах; вернёт новый MuteN.
func (p *PathMemory) tickMute(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e, ok := p.data[key]
	if !ok || len(e.MuteHosts) == 0 {
		return 0
	}
	e.MuteN++
	p.persist()
	return e.MuteN
}

// directMuteSkipAfter и streamMuteSkipAfter — MeridianVpnService.
// DIRECT_MUTE_SKIP_AFTER/STREAM_MUTE_SKIP_AFTER (Android): пороги немоты
// НЕ приходят от сервера (в отличие от DirectSkipAfterFails/
// DirectProbeEveryAttempt проигрыша гонки) — это оценка твёрдости улики
// «путь поднялся и умер сам», не «путь просто медленнее». У прямого
// немота — надёжная улика с первого раза (1), у потока чуть менее
// надёжная (2, разбор — PathMemory.kt, MUTE_SUFFIX).
const (
	directMuteSkipAfter = 1
	streamMuteSkipAfter = 2
)

// pathMemoryInputs — то, что Connect уже знает к моменту сборки лестницы
// (ответ /v1/params, опции), нужное памяти путей для решения.
type pathMemoryInputs struct {
	Reconnect    bool
	Mode         LadderMode
	RelayEnabled bool
	HashesLen    int
	ProbeEvery   int
	// NodeHosts — адреса всех входов из ответа /v1/params.
	NodeHosts []string
}

// pathMemoryOutcome — решение памяти путей ДО лестницы плюс то, что
// нужно ПОСЛЕ исхода, чтобы снять именно ту метку, которая включила
// пропуск (PathMemory.kt, MeridianVpnService: «снимаем ровно ту метку,
// что и завела пропуск — иначе одна причина стёрла бы улику другой»).
// key == "" — память не участвует (pm==nil или платформа без
// NetworkKeyer): все методы ниже становятся no-op.
type pathMemoryOutcome struct {
	key         string
	known       string
	avoidHosts  map[string]bool // онемевшие входы, обходим на этом подключении
	skipStreams bool
	skipSlow    bool
	relayReady  bool
}

// pathMemoryBeforeLadder — решение памяти путей для сети key, ДО
// BuildLadder. Вынесена из Connect отдельной функцией ради теста: сама
// логика не трогает сеть, только PathMemory (у неё в тестах свой
// NewPathMemory(tempfile), сеть/диск платформы ни при чём).
//
// pm == nil или key == "" — память не участвует вовсе, лестница строится
// как раньше (нулевой pathMemoryOutcome).
func pathMemoryBeforeLadder(pm *PathMemory, key string, in pathMemoryInputs) pathMemoryOutcome {
	if pm == nil || key == "" {
		return pathMemoryOutcome{}
	}
	out := pathMemoryOutcome{key: key, known: pm.Remembered(key)}
	// Пропуск прямого по счётчику проигрышей релею ВЫКЛЮЧЕН (владелец 03.10,
	// как Android 0.1.231): прямой пробуется на каждом подключении.
	out.relayReady = in.Mode == LadderModeAuto && in.RelayEnabled && in.HashesLen > 0
	if out.relayReady {
		out.skipStreams = pm.SkipStreams(key, streamMuteSkipAfter, in.ProbeEvery)
	}
	if out.relayReady {
		out.skipSlow = pm.SkipSlow(key, in.ProbeEvery)
	}
	out.avoidHosts = muteAvoid(pm, key, in, out.relayReady)
	return out
}

// muteAvoid — какие входы обходить на этом подключении (немота ПО ВХОДУ,
// Android 03.10: оператор глушит часть входов после первых обменов, через
// другие прямой держится). Онемевшие входы обходим, пока есть другие; когда
// онемели все — мимо прямого целиком, только если релею есть на чём
// подняться. Раз в probeEvery подключений онемевшие пробуются снова.
func muteAvoid(pm *PathMemory, key string, in pathMemoryInputs, relayReady bool) map[string]bool {
	muted := pm.MuteHosts(key)
	if len(muted) == 0 || in.Mode == LadderModeRelayOnly {
		return nil
	}
	n := pm.tickMute(key)
	if in.ProbeEvery > 0 && n%in.ProbeEvery == 0 {
		return nil
	}
	avoid := map[string]bool{}
	for _, h := range muted {
		avoid[h] = true
	}
	all := true
	for _, h := range in.NodeHosts {
		if !avoid[h] {
			all = false
			break
		}
	}
	if all && !relayReady {
		return nil
	}
	return avoid
}

// filterNodes — убрать из ответа входы, которые обходим.
func (o pathMemoryOutcome) filterNodes(nodes []ParamsNode) []ParamsNode {
	if len(o.avoidHosts) == 0 {
		return nodes
	}
	out := make([]ParamsNode, 0, len(nodes))
	for _, n := range nodes {
		if !o.avoidHosts[n.Host] {
			out = append(out, n)
		}
	}
	return out
}

// applyToLadder переносит решение в LadderOptions, которые пойдут в
// BuildLadder.
func (o pathMemoryOutcome) applyToLadder(ladder *LadderOptions) {
	if o.key == "" {
		return
	}
	ladder.Known = o.known
	ladder.SkipDirect = o.skipSlow
	ladder.SkipStreams = o.skipStreams
}

// onLadderFailed — лестница не поднялась ни одной ступенью (PathMemory.kt:
// «лестница не поднялась целиком»). Забывает запомненную ступень (больше
// не годится) и снимает ровно те метки пропуска, что сами включили его —
// стратегия «мимо прямого/потока» не сработала, в следующий раз пробуем
// заново.
//
// БЕЗ networkChanged(): десктоп пока не отличает обрыв из-за смены сети
// от обычного отказа лестницы (у Android это отдельная проверка,
// защищающая рабочий путь от наказания за смену сети посреди попытки) —
// известное упрощение, см. отчёт кота 6 от 2026-09-25.
func (o pathMemoryOutcome) onLadderFailed(pm *PathMemory) {
	if o.key == "" {
		return
	}
	if o.known != "" {
		pm.Forget(o.key)
	}
	if len(o.avoidHosts) > 0 {
		pm.ClearMuteHosts(o.key)
	}
	if o.skipStreams {
		pm.ClearStreamMute(o.key)
	}
	if o.skipSlow {
		pm.ClearSlow(o.key)
	}
}

// onWinner — лестница поднялась, winner — буква победившей ступени
// (EngineHandle.Winner). Запоминаем ТОЛЬКО прямые UDP-ступени (relayName
// "R" — релей, info.Streams[winner] — поток): память нужна ровно для
// одного — пропустить гонку, когда для этой сети уже известна хорошая
// прямая ступень (PathMemory.kt: «ПРЯМАЯ — ЭТО UDP-ПРЯМАЯ»).
func (o pathMemoryOutcome) onWinner(pm *PathMemory, winner string, info *LadderInfo) {
	if o.key == "" || winner == "" {
		return
	}
	isStream := info != nil && info.Streams[winner]
	isRelay := winner == "R" // engine/params/ladder.go: relayName = "R"
	if isStream || isRelay {
		// Поток или релей — UDP-прямой не держится, сеть набирает вес в
		// сторону «мимо UDP».
		pm.NoteRelayWon(o.key)
		if o.known != "" {
			// Запомненная UDP-ступень не довезла — забываем, иначе она
			// осталась бы первой и в следующий раз снова съедала секунды
			// на заведомо мёртвую попытку.
			pm.ForgetRung(o.key)
		}
		return
	}
	nodeID := winner
	if info != nil {
		if id, ok := info.IDs[winner]; ok {
			nodeID = id // устойчивый id узла, а не буква (та зависит от места в списке)
		}
	}
	pm.Remember(o.key, nodeID)
	// Прямой ожил — счётчик пропуска гонки в ноль. Немоту (directMute)
	// это НЕ трогает: она про другую беду (поднялся и умер посреди
	// сессии), эта победа лестницы её не опровергает.
	pm.ClearRelayStreak(o.key)
}

// --- МЕДЛЕННЫЕ ПРЯМЫЕ ПУТИ (speedGuard; порт PathMemory.kt SLOW_SUFFIX,
// просьба владельца 26.09: клиент сам уходит на более быстрый путь) ---

// SlowKbps — замеренная скорость прямого пути/потока на этой сети,
// кбит/с; 0 — пометки нет.
func (p *PathMemory) SlowKbps(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		return e.SlowKbps
	}
	return 0
}

// MarkSlow — на этой сети прямые пути и потоки медленные: следующие
// подъёмы идут на релей. Счётчик попыток — в ноль.
func (p *PathMemory) MarkSlow(key string, kbps int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e := p.entry(key)
	if kbps < 1 {
		kbps = 1
	}
	e.SlowKbps = kbps
	e.SlowN = 0
	e.SlowAt = pmNow().UnixMilli()
	p.persist()
}

// ClearSlow снимает пометку «медленно».
func (p *PathMemory) ClearSlow(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	if e, ok := p.data[key]; ok {
		e.SlowKbps = 0
		e.SlowN = 0
		e.SlowAt = 0
		p.dropIfZero(key)
	}
	p.persist()
}

// SkipSlow — идти ли мимо прямых путей и потока на этой сети СЕЙЧАС.
// Считает попытки сама (звать ровно один раз за подъём): раз в
// probeEvery попыток возвращает false, чтобы заметить, что прямой снова
// быстр (PathMemory.kt: skipSlow).
func (p *PathMemory) SkipSlow(key string, probeEvery int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e, ok := p.data[key]
	if !ok || e.SlowKbps == 0 {
		return false
	}
	// Пометку снимает время (защита от петли 27.09): старше slowTTL — снята;
	// без метки времени (файл версий до включения стража) — тоже устарела.
	if e.SlowAt == 0 || pmNow().UnixMilli()-e.SlowAt > slowTTL.Milliseconds() {
		e.SlowKbps, e.SlowN, e.SlowAt = 0, 0, 0
		p.dropIfZero(key)
		p.persist()
		return false
	}
	e.SlowN++
	p.persist()
	if probeEvery < 2 {
		probeEvery = 2
	}
	return e.SlowN%probeEvery != 0
}

// Пороги speedGuard — Android 0.1.254 (Кот 3): SLOW_KBPS=1000, SLOW_LOSS_PCT=50,
// SLOW_TTL=2 ч, SPEED_SWITCH_GAP=30 мин. У клиента 26.09 прямые давали
// 60-100 кбит/с против 83 Мбит/с через релей; порог снижен с 3000 до 1000,
// чтобы не принимать за шейп обычный медленный канал.
//
// Три защиты от петли 27.09 (замер пачкой релей режет сам, и сравнение с ним
// гоняло клиента по кругу): пометку «медленно» снимает только время
// (slowTTL), замер релея её не трогает; смена пути по скорости — не чаще
// раза в speedSwitchGap на сеть; решение — только по порогу и потерям
// прямого пути, без сравнения с релеем.
const (
	slowKbpsThreshold = 1000
	slowLossPct       = 50
	slowTTL           = 2 * time.Hour
	speedSwitchGap    = 30 * time.Minute
)

// pmNow — часы памяти путей; подменяются в тестах.
var pmNow = time.Now

// SpeedSwitchAllowed — можно ли сейчас менять путь из-за скорости на этой сети.
func (p *PathMemory) SpeedSwitchAllowed(key string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	e, ok := p.data[key]
	if !ok || e.SpeedSwitchAt == 0 {
		return true
	}
	return pmNow().UnixMilli()-e.SpeedSwitchAt >= speedSwitchGap.Milliseconds()
}

// NoteSpeedSwitch — запомнить, что путь на этой сети сменили из-за скорости.
func (p *PathMemory) NoteSpeedSwitch(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.load()
	p.entry(key).SpeedSwitchAt = pmNow().UnixMilli()
	p.persist()
}

// Состояния замера скорости (engine/speedprobe.go).
const (
	SpeedProbeNone    int32 = 0
	SpeedProbeRunning int32 = 1
	SpeedProbeDone    int32 = 2
	SpeedProbeSilent  int32 = 3
)

type speedAction int

const (
	speedNone speedAction = iota
	speedMarkSlow
)

// speedGuardDecision — что делать по итогу замера (MeridianVpnService.
// speedGuard). Чистая функция ради теста. На релее замер ничего не решает
// (его режет сам релей, петля 27.09) и пометку не снимает. На прямом пути
// при доступном релее медленный/потерянный замер — помечаем сеть и
// переподключаемся.
func speedGuardDecision(onRelay, relayReady bool, kbps int64, lossPct int32) speedAction {
	if onRelay || !relayReady {
		return speedNone
	}
	if kbps >= slowKbpsThreshold && lossPct < slowLossPct {
		return speedNone
	}
	return speedMarkSlow
}

// speedGuardPoll и speedGuardPolls — как часто и сколько раз спрашивать
// движок о ходе замера. Пачка engine-v1.10.9 стартует через 6 с после
// подъёма и ждёт ответы 4 с, итог готов не раньше ~11 с — отводим 30 с.
// Переменные ради теста.
var (
	speedGuardPoll  = 100 * time.Millisecond
	speedGuardPolls = 300
)

// runSpeedGuard — порт MeridianVpnService.speedGuard: ждёт итог замера
// скорости (engine/speedprobe.go) и, если поднялись НЕ релеем, а путь
// медленный или теряет пакеты при доступном релее, помечает сеть в
// памяти путей и останавливает сессию — KeepConnected видит закрытие
// канала событий как «движок сам умер» и переподключается уже мимо
// прямых путей (pathMemoryBeforeLadder: skipSlow). На релее замер
// не судится вовсе, пометку снимает только время (slowTTL); путь по
// скорости меняется не чаще раза в speedSwitchGap.
//
// Не пишет в onEvent: тот зовётся из цикла KeepConnected, а отправка в
// канал управления из двух горутин перемешала бы кадры — только в
// stderr (лог службы). Выходит по ctx (пользователь остановил, сессия
// закончилась).
func (s *Session) runSpeedGuard(ctx context.Context) {
	o := s.pmOutcome
	if s.pm == nil || o.key == "" {
		return
	}
	var st int32
	var kbps int64
	var loss int32
	for i := 0; i < speedGuardPolls; i++ {
		if ctx.Err() != nil {
			return
		}
		st, kbps, loss = s.engine.SpeedProbe()
		if st == SpeedProbeDone || st == SpeedProbeSilent {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(speedGuardPoll):
		}
	}
	switch st {
	case SpeedProbeSilent:
		// Молчание на ПРЯМОМ пути — путь режется (шейп МТС отвечал на всю
		// пачку тишиной, Кот 3); на релее молчание ничего не значит.
		if s.winner == "R" || !o.relayReady {
			fmt.Fprintln(os.Stderr, "замер скорости: шлюз не ответил")
			return
		}
		kbps, loss = 0, 100
		fmt.Fprintln(os.Stderr, "замер скорости: шлюз не ответил на прямом пути — считаю путь медленным")
	case SpeedProbeDone:
		fmt.Fprintf(os.Stderr, "замер скорости: %d кбит/с, потери %d%%\n", kbps, loss)
	default:
		return
	}

	// TCP сохраняем: медленный UDP не доказывает, что поток хуже релея.
	if s.winnerTCP && st == SpeedProbeDone {
		return
	}

	if speedGuardDecision(s.winner == "R", o.relayReady, kbps, loss) == speedMarkSlow {
		if ctx.Err() != nil {
			return
		}
		if !s.pm.SpeedSwitchAllowed(o.key) {
			fmt.Fprintln(os.Stderr, "путь медленный, но путь уже меняли недавно — остаюсь")
			return
		}
		s.pm.MarkSlow(o.key, int(kbps))
		s.pm.NoteSpeedSwitch(o.key)
		s.speedSwitched.Store(true)
		fmt.Fprintln(os.Stderr, "путь медленный — перехожу на запасной")
		s.engine.Stop()
	}
}

// minSecondRoundMs — нижний предел второго круга гонки (Android 0.1.231).
const minSecondRoundMs = 5000

// muteWindow — сессия на прямом входе, умершая сама раньше этого срока,
// засчитывает немоту входу; прожившая дольше — снимает с него пометку.
const muteWindow = 5 * time.Minute

// nodeHost — адрес входа победившей ступени winner ("" — релей или не нашли).
func nodeHost(nodes []ParamsNode, info *LadderInfo, winner string) string {
	if info == nil || winner == "" {
		return ""
	}
	id, ok := info.IDs[winner]
	if !ok {
		return ""
	}
	for _, n := range nodes {
		if n.ID == id {
			return n.Host
		}
	}
	return ""
}

// noteSessionEnd — итог сессии для немоты входа (порт forgetMutePath).
func (s *Session) noteSessionEnd(unexpected bool, lived time.Duration) {
	key := s.pmOutcome.key
	if s.pm == nil || key == "" || s.winnerHost == "" {
		return
	}
	if s.speedSwitched.Load() {
		return // ушли сами из-за скорости — это не немота входа
	}
	switch {
	case lived >= muteWindow:
		s.pm.ClearMuteHost(key, s.winnerHost)
	case unexpected:
		s.pm.Forget(key)
		s.pm.NoteMuteHost(key, s.winnerHost)
		fmt.Fprintf(os.Stderr, "память путей: вход онемел через %s — на этой сети обхожу его\n", lived.Round(time.Second))
	}
}
