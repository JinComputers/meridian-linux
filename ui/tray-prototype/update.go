// update.go — проверка новой версии и обновление «на лету» (общее для
// macOS и Windows; установка скачанного — installUpdate в host_*.go).
//
// Как на Android (meridian-android-src, direct/Update.kt), тот же дух:
//   - источник — открытый репозиторий раздачи
//     JinComputers/meridian-desktop-releases, путь releases/latest/download/
//     (НЕ api.github.com: там 60 запросов в час на адрес, а за операторским
//     NAT адрес общий на многих людей);
//   - описание релиза — latest-<платформа>.json:
//     {"version":"0.4.4","url":"https://…","sha256":"<64 hex>","size":N,"notes":"…"};
//   - тихая проверка не чаще раза в сутки, после неудачи — через 15 минут;
//     неудача тихой проверки человеку не показывается;
//   - навязчивости нет: строка на главном экране (закрывается — до
//     СЛЕДУЮЩЕЙ версии, не навсегда) и строка версии в настройках;
//   - автопроверку можно выключить в настройках («Проверять обновления»);
//     ручная проверка — нажатием на строку версии — работает всегда.
//
// Скачанное сверяется по размеру и sha256 из описания. Это защита от
// битой загрузки, НЕ от подмены: описание приходит из того же места, что
// и файл. От подмены — HTTPS до github.com и (позже) подпись Authenticode /
// Developer ID, которую тогда же надо проверять перед запуском.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// appVersion — версия сборки; build-msi.ps1 (и сборка macOS) ставят
// настоящую через -ldflags "-X main.appVersion=a.b.c".
var appVersion = "0.1.0"

// updateChannel — канал обновлений, ставится при сборке
// (-X main.updateChannel=debug): "release" — открытый репозиторий раздачи
// абонентам, "debug" — закрытый meridian-desktop-debug (беты владельца,
// нужен токен GitHub на чтение, getUpdateToken, host_*.go). Решение
// владельца 25.09: две ветки — бета у него, выпуск для всех.
var updateChannel = "release"

const (
	updateRepo        = "JinComputers/meridian-desktop-releases"
	updateRepoMacos   = "JinComputers/meridian-macos"
	updateRepoDebug   = "JinComputers/meridian-desktop-debug"
	updateQuietEvery  = 24 * time.Hour
	updateFailRetry   = 15 * time.Minute
	updateMaxSize     = 300 << 20 // больше — описание врёт
	updateManifestMax = 64 << 10
)

// updateManifestURL — адрес описания. Для проверки на стенде settings.json
// может задать свой (update_manifest_url): только https или http на
// 127.0.0.1/localhost. В интерфейсе этого поля нет.
func updateManifestURL() string {
	if u := loadSettings().UpdateManifestURL; u != "" && updateURLAllowed(u) {
		return u
	}
	repo := updateRepo
	if updatePlatform == "macos" {
		repo = updateRepoMacos
	}
	return "https://github.com/" + repo + "/releases/latest/download/latest-" + updatePlatform + ".json"
}

func updateURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "https":
		return u.Host != ""
	case "http":
		h := u.Hostname()
		return h == "127.0.0.1" || h == "localhost"
	}
	return false
}

type updateManifest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Notes   string `json:"notes"`

	viaTunnel bool // описание и файл — из туннеля (update_mirror.go), не по https
}

func (m *updateManifest) validate() error {
	if _, ok := parseVersion(m.Version); !ok {
		return fmt.Errorf("версия %q не a.b.c", m.Version)
	}
	if !updateURLAllowed(m.URL) {
		return fmt.Errorf("адрес файла не https")
	}
	if len(m.SHA256) != 64 {
		return fmt.Errorf("длина sha256 не та")
	}
	if _, err := hex.DecodeString(m.SHA256); err != nil {
		return fmt.Errorf("sha256 не hex")
	}
	if m.Size <= 0 || m.Size > updateMaxSize {
		return fmt.Errorf("размер %d вне пределов", m.Size)
	}
	return nil
}

// parseVersion — «a.b.c» (допускается ведущая v и хвост -что-то, который
// не сравнивается).
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// versionNewer — a новее b.
func versionNewer(a, b string) bool {
	va, ok1 := parseVersion(a)
	vb, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

type updateStage string

const (
	updIdle        updateStage = "idle"
	updChecking    updateStage = "checking"
	updLatest      updateStage = "latest"
	updAvailable   updateStage = "available"
	updDownloading updateStage = "downloading"
	updInstalling  updateStage = "installing"
	updFailed      updateStage = "failed"
)

type updater struct {
	a *app

	mu       sync.Mutex
	stage    updateStage
	found    *updateManifest
	got      int64
	problem  string
	lastTry  time.Time
	busy     bool
	afterMsg string // «обновлено до …» после перезапуска — одной строкой
}

func newUpdater(a *app) *updater { return &updater{a: a, stage: updIdle} }

// snapshot — поля для события "state".
func (u *updater) snapshot(m map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := loadSettings()
	m["update_stage"] = string(u.stage)
	m["update_problem"] = u.problem
	m["update_version"], m["update_notes"], m["update_percent"] = "", "", 0
	if u.found != nil {
		m["update_version"] = u.found.Version
		m["update_notes"] = u.found.Notes
		if u.found.Size > 0 {
			m["update_percent"] = int(u.got * 100 / u.found.Size)
		}
	}
	// Строка на главном экране: есть новее и её не закрывали (закрытие —
	// до следующей версии). Скачивание/установку закрытием не прячем.
	m["update_banner"] = u.found != nil &&
		(u.stage == updDownloading || u.stage == updInstalling ||
			(u.stage == updAvailable && st.UpdateDismissed != u.found.Version))
	m["update_after"] = u.afterMsg
}

// loop — тихая проверка: через минуту после старта — всегда (иначе после
// перезапуска программы синяя пометка о новой версии внизу настроек
// пропадала до конца суток с прошлой проверки, просьба владельца 27.09),
// дальше раз в час смотрим, не пора ли (сутки после удачи, 15 минут
// после неудачи).
func (u *updater) loop() {
	time.Sleep(time.Minute)
	u.quietCheck(true)
	for {
		time.Sleep(time.Hour)
		u.quietCheck(false)
	}
}

func (u *updater) quietCheck(atStart bool) {
	st := loadSettings()
	u.mu.Lock()
	// updAvailable не останавливает: вышла версия ещё новее — пометка
	// переедет на неё.
	if u.busy || u.stage == updDownloading || u.stage == updInstalling {
		u.mu.Unlock()
		return
	}
	tried := u.lastTry
	u.mu.Unlock()
	now := time.Now()
	last := time.Unix(st.UpdateLastCheck, 0)
	// Часы могли уехать назад — тогда тоже проверяем.
	if !atStart && st.UpdateLastCheck > 0 && now.After(last) && now.Sub(last) < updateQuietEvery {
		return
	}
	if !tried.IsZero() && now.Sub(tried) < updateFailRetry {
		return
	}
	u.check(false)
}

// check — спросить описание. loud: ручная проверка — показать и
// «установлена последняя», и неудачу.
func (u *updater) check(loud bool) {
	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.lastTry = time.Now()
	prev := u.stage
	if loud {
		u.stage, u.problem = updChecking, ""
	}
	u.mu.Unlock()
	u.a.applyLocal()

	m, err := fetchManifest()

	u.mu.Lock()
	u.busy = false
	switch {
	case err != nil:
		log.Println("обновление: проверка:", err)
		if loud {
			u.stage, u.problem = updFailed, "проверить не удалось"
		} else {
			u.stage = prev
		}
	case versionNewer(m.Version, appVersion):
		log.Printf("обновление: есть %s (у нас %s)", m.Version, appVersion)
		u.found, u.stage, u.problem = m, updAvailable, ""
	default:
		log.Printf("обновление: последняя %s, у нас %s", m.Version, appVersion)
		u.found = nil
		if loud {
			u.stage = updLatest
		} else {
			u.stage = updIdle
		}
	}
	u.mu.Unlock()
	if err == nil {
		updateSettings(func(s *settings) { s.UpdateLastCheck = time.Now().Unix() })
	}
	u.a.applyLocal()
	if u.stageIs(updLatest) {
		// «Установлена последняя» — ненадолго, потом обычная строка версии.
		time.AfterFunc(4*time.Second, func() {
			u.mu.Lock()
			if u.stage == updLatest {
				u.stage = updIdle
			}
			u.mu.Unlock()
			u.a.applyLocal()
		})
	}
}

func (u *updater) stageIs(s updateStage) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stage == s
}

func fetchManifest() (*updateManifest, error) {
	if updateChannel == "debug" && loadSettings().UpdateManifestURL == "" {
		return fetchManifestDebug()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Сначала зеркало на API (до 8 с), потом GitHub; всё вместе — в пределах 20 с.
	if mirrorEnabled() {
		if m, err := fetchMirrorManifest(ctx); err == nil {
			return m, nil
		} else {
			log.Printf("обновление: описание с зеркала не получено (%v), пробую GitHub", err)
		}
	}
	// Туннель поднят — обычному каналу даём не больше tunnelFirstWait, чтобы
	// осталось время на ступень через туннель (общий предел 20 с).
	viaTunnel := mirrorEnabled() && tunnelIsUp()
	gctx := ctx
	if viaTunnel {
		var gcancel context.CancelFunc
		gctx, gcancel = context.WithTimeout(ctx, tunnelFirstWait)
		defer gcancel()
	}
	m, err := fetchGitHubManifest(gctx)
	if err == nil {
		return m, nil
	}
	if viaTunnel {
		if tm, terr := fetchTunnelManifest(ctx); terr == nil {
			return tm, nil
		} else {
			log.Printf("обновление: через туннель описание не получено (%v)", terr)
		}
	}
	return nil, err
}

func fetchGitHubManifest(ctx context.Context) (*updateManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateManifestURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, updateManifestMax)).Decode(&m); err != nil {
		return nil, fmt.Errorf("описание: %w", err)
	}
	m.SHA256 = strings.ToLower(strings.TrimSpace(m.SHA256))
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// start — «Обновить»: скачать, сверить, установить поверх.
func (u *updater) start() {
	u.mu.Lock()
	if u.busy || u.found == nil || (u.stage != updAvailable && u.stage != updFailed) {
		u.mu.Unlock()
		return
	}
	m := *u.found
	u.busy, u.stage, u.got, u.problem = true, updDownloading, 0, ""
	u.mu.Unlock()
	u.a.applyLocal()

	path, err := u.download(&m)
	if err == nil {
		u.mu.Lock()
		u.stage = updInstalling
		u.mu.Unlock()
		u.a.applyLocal()
		u.a.state.mu.Lock()
		reconnect := u.a.state.kind == uiConnected || u.a.state.kind == uiConnecting
		u.a.state.mu.Unlock()
		log.Printf("обновление: ставлю %s (переподключить после: %v)", m.Version, reconnect)
		// installUpdate при успехе завершает программу (перезапуск — уже
		// новой версией); вернулся — значит, не вышло.
		err = installUpdate(u.a, path, m.Version, reconnect)
	}
	u.mu.Lock()
	u.busy = false
	if err != nil {
		log.Println("обновление:", err)
		u.stage, u.problem = updFailed, err.Error()
	}
	u.mu.Unlock()
	u.a.applyLocal()
}

func updateDir() string { return filepath.Join(appSupportDir(), "update") }

func (u *updater) download(m *updateManifest) (string, error) {
	dir := updateDir()
	// Старые загрузки не копим: в каталоге только текущая.
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("каталог загрузки: %w", err)
	}
	name := filepath.Base(m.URL)
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	if name == "" || name == "." || name == "/" {
		name = "update.bin"
	}
	path := filepath.Join(dir, name)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var resp *http.Response
	if m.viaTunnel {
		resp = tunnelDownload(ctx, m)
		if resp == nil {
			return "", errors.New("скачать не удалось")
		}
	} else if mirrorEnabled() {
		resp = mirrorDownload(ctx, m)
	}
	if resp == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
		if err != nil {
			return "", err
		}
		if updateChannel == "debug" && loadSettings().UpdateManifestURL == "" {
			// Закрытый репозиторий: файл — вложение того же релиза, через API.
			if req, err = debugAssetRequest(ctx, filepath.Base(m.URL)); err != nil {
				return "", err
			}
		}
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return "", errors.New("скачать не удалось")
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("скачать не удалось (HTTP %d)", resp.StatusCode)
		}
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int64
	lastEmit := time.Now()
	body := io.LimitReader(resp.Body, m.Size+1)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return "", werr
			}
			h.Write(buf[:n])
			total += int64(n)
			u.mu.Lock()
			u.got = total
			u.mu.Unlock()
			if time.Since(lastEmit) > 300*time.Millisecond {
				lastEmit = time.Now()
				u.a.applyLocal()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return "", errors.New("загрузка оборвалась")
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if total != m.Size {
		return "", fmt.Errorf("размер не сошёлся (%d вместо %d)", total, m.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != m.SHA256 {
		_ = os.Remove(path)
		return "", errors.New("файл повреждён (sha256 не сошёлся)")
	}
	return path, nil
}

// dismiss — «×» у строки на главном экране: до следующей версии.
func (u *updater) dismiss() {
	u.mu.Lock()
	v := ""
	if u.found != nil {
		v = u.found.Version
	}
	u.afterMsg = ""
	u.mu.Unlock()
	if v != "" {
		updateSettings(func(s *settings) { s.UpdateDismissed = v })
		log.Printf("обновление: строка о %s закрыта, в настройках останется", v)
	}
	u.a.applyLocal()
}

// versionRowClick — нажатие на строку версии в настройках.
func (u *updater) versionRowClick() {
	u.mu.Lock()
	stage := u.stage
	u.mu.Unlock()
	switch stage {
	case updAvailable, updFailed:
		if u.found != nil {
			go u.start()
			return
		}
		go u.check(true)
	case updIdle, updLatest:
		go u.check(true)
	}
}

// afterUpdate — с чем запущены после обновления (аргументы от помощника).
type afterUpdate struct {
	message   string
	reconnect bool
}

func parseAfterUpdate(args []string) afterUpdate {
	var r afterUpdate
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--updated="):
			// Строку «Обновлено до …» не показываем (просьба владельца
			// 28.10, Windows; на Mac — то же, обновление идёт само): версия
			// видна в настройках. Неудачу — да.
		case strings.HasPrefix(a, "--update-failed="):
			switch code := strings.TrimPrefix(a, "--update-failed="); code {
			case "1223", "1602": // отказ в UAC / отмена
				r.message = "Обновление отменено"
			default:
				r.message = "Обновление не установлено (код " + code + ")"
			}
		case a == "--reconnect":
			r.reconnect = true
		}
	}
	return r
}

// --- бета-канал: закрытый репозиторий через API GitHub с токеном ---
//
// Как отладочный канал Android (direct/Update.kt): releases/latest/download
// у закрытого репозитория не работает, вложения отдаются только по id и с
// токеном. Предел API 60/ч тут не страшен — этим путём ходит только машина
// владельца. Перенаправление на хранилище GitHub Go выполняет сам и
// заголовок Authorization на чужой хост не переносит — так и нужно.

type ghAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

var (
	debugAssetsMu sync.Mutex
	debugAssets   map[string]ghAsset // вложения последнего релиза по имени
)

func ghRequest(ctx context.Context, rawURL, accept string) (*http.Request, error) {
	tok := getUpdateToken()
	if tok == "" {
		return nil, errors.New("нет токена закрытых сборок")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	return req, nil
}

func fetchManifestDebug() (*updateManifest, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Не releases/latest: в один репозиторий выкладывают и Windows, и
	// macOS — последний релиз может быть чужой платформы. Берём самый
	// свежий, где есть описание нашей.
	req, err := ghRequest(ctx, "https://api.github.com/repos/"+updateRepoDebug+"/releases?per_page=20", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API GitHub: HTTP %d", resp.StatusCode)
	}
	var rels []struct {
		Draft  bool      `json:"draft"`
		Assets []ghAsset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rels); err != nil {
		return nil, fmt.Errorf("API GitHub: %w", err)
	}
	var assets map[string]ghAsset
	for _, rel := range rels { // GitHub отдаёт от новых к старым
		if rel.Draft {
			continue
		}
		m := map[string]ghAsset{}
		for _, a := range rel.Assets {
			m[a.Name] = a
		}
		if _, ok := m["latest-"+updatePlatform+".json"]; ok {
			assets = m
			break
		}
	}
	if assets == nil {
		return nil, fmt.Errorf("в закрытом репозитории нет сборок для %s", updatePlatform)
	}
	debugAssetsMu.Lock()
	debugAssets = assets
	debugAssetsMu.Unlock()

	mreq, err := debugAssetRequest(ctx, "latest-"+updatePlatform+".json")
	if err != nil {
		return nil, err
	}
	mresp, err := http.DefaultClient.Do(mreq)
	if err != nil {
		return nil, err
	}
	defer mresp.Body.Close()
	if mresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("описание: HTTP %d", mresp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(mresp.Body, updateManifestMax)).Decode(&m); err != nil {
		return nil, fmt.Errorf("описание: %w", err)
	}
	m.SHA256 = strings.ToLower(strings.TrimSpace(m.SHA256))
	if err := m.validate(); err != nil {
		return nil, err
	}
	if _, ok := assets[filepath.Base(m.URL)]; !ok {
		return nil, fmt.Errorf("в релизе нет файла %s", filepath.Base(m.URL))
	}
	return &m, nil
}

// debugAssetRequest — запрос содержимого вложения name последнего релиза.
func debugAssetRequest(ctx context.Context, name string) (*http.Request, error) {
	debugAssetsMu.Lock()
	a, ok := debugAssets[name]
	debugAssetsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("в релизе нет файла %s", name)
	}
	return ghRequest(ctx, a.URL, "application/octet-stream")
}
