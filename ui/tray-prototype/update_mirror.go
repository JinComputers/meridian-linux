// update_mirror.go — зеркало обновлений на API (кот 1, решение владельца
// 02.10): на мобильных сетях GitHub часто не отвечает. Клиент ходит сначала
// на зеркало, потом на GitHub (договор как у Android, direct/Update.kt).
//
//	GET /v1/update/<платформа>/latest.json — описание релиза байт в байт
//	GET /v1/update/<платформа>/<имя>       — файл, имя берётся из url описания
//
// Адреса и доверие те же, что у /v1/params: jinelectronics.ru:21947 и
// 138.249.246.89:21947 — только по закреплённому ключу (params.PinMain/
// PinBackup), cdn.jincomputers.win:443 — обычная проверка цепочки.
// Описание с зеркала проверяется так же, как с GitHub (sha256 и размер из
// него сверяются при скачивании); любая неудача зеркала — переход на GitHub.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"meridian/engine/params"
)

type mirrorAddr struct {
	host   string
	port   int
	pinned bool
}

// updateMirrors — те же адреса, что params.DefaultAddresses.
var updateMirrors = []mirrorAddr{
	{"jinelectronics.ru", 21947, true},
	{"138.249.246.89", 21947, true},
	{"cdn.jincomputers.win", 443, false},
}

var mirrorPins = []string{params.PinMain, params.PinBackup}

const (
	mirrorAttempt = 5 * time.Second // один адрес: соединение и заголовки
	mirrorStage   = 8 * time.Second // все адреса зеркала вместе
)

var mirrorNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// mirrorEnabled — зеркало только у открытого канала: закрытые беты лежат в
// закрытом репозитории, стенд со своим адресом описания его не использует.
func mirrorEnabled() bool {
	return updateChannel != "debug" && loadSettings().UpdateManifestURL == ""
}

// mirrorFileName — имя файла из url описания; пусто, если имя не годится
// для пути зеркала (только буквы, цифры, точка, дефис, подчёркивание).
func mirrorFileName(rawURL string) string {
	name := rawURL
	if i := strings.IndexAny(name, "?#"); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	if !mirrorNameRe.MatchString(name) || strings.Contains(name, "..") {
		return ""
	}
	return name
}

func mirrorClient(a mirrorAddr) *http.Client {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if a.pinned {
		// Доверие ТОЛЬКО по пину (сертификат самоподписанный): несовпадение
		// обрывает рукопожатие, ветки «принять любой» нет.
		cfg.InsecureSkipVerify = true
		cfg.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("сервер не прислал сертификата")
			}
			if !params.PinMatches(raw, mirrorPins) {
				return errors.New("ключ сервера не совпал с закреплённым")
			}
			return nil
		}
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: mirrorAttempt}).DialContext,
			TLSClientConfig:       cfg,
			TLSHandshakeTimeout:   mirrorAttempt,
			ResponseHeaderTimeout: mirrorAttempt,
		},
		// Редирект уводил бы мимо пина.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

// mirrorGetOne — один адрес; ответ 200 с телом (закрыть при чтении).
func mirrorGetOne(ctx context.Context, a mirrorAddr, rel string, limit time.Duration) (*http.Response, error) {
	rctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(limit, cancel)
	url := fmt.Sprintf("https://%s:%d/v1/update/%s/%s", a.host, a.port, updatePlatform, rel)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := mirrorClient(a).Do(req)
	if !timer.Stop() {
		if err == nil {
			resp.Body.Close()
		}
		cancel()
		return nil, errors.New("таймаут")
	}
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	return resp, nil
}

// mirrorGet — по адресам зеркала по очереди, не дольше mirrorStage в сумме.
func mirrorGet(ctx context.Context, rel string) (*http.Response, error) {
	deadline := time.Now().Add(mirrorStage)
	last := errors.New("зеркало не отвечает")
	for _, a := range updateMirrors {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		limit := mirrorAttempt
		if left < limit {
			limit = left
		}
		resp, err := mirrorGetOne(ctx, a, rel, limit)
		if err == nil {
			return resp, nil
		}
		last = fmt.Errorf("%s: %w", a.host, err)
	}
	return nil, last
}

// fetchMirrorManifest — latest.json с зеркала, проверенный как с GitHub.
func fetchMirrorManifest(ctx context.Context) (*updateManifest, error) {
	resp, err := mirrorGet(ctx, "latest.json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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

// mirrorDownload — файл с зеркала по имени из url описания; nil — не вышло,
// идти на GitHub.
func mirrorDownload(ctx context.Context, m *updateManifest) *http.Response {
	name := mirrorFileName(m.URL)
	if name == "" {
		return nil
	}
	resp, err := mirrorGet(ctx, name)
	if err != nil {
		log.Printf("обновление: зеркало недоступно (%v), берём с GitHub", err)
		return nil
	}
	log.Printf("обновление: файл с зеркала (%s)", resp.Request.URL.Host)
	return resp
}

// --- третья ступень: через туннель (поручение владельца 04.10, кот 2) ---
//
// Зеркало и GitHub не ответили, а туннель поднят: описание и файл берутся
// с шлюза внутри туннеля. Канал http без TLS (адрес приватный, трафик идёт
// по туннелю); sha256 приходит оттуда же, где файл, так что это защита от
// битой загрузки, не от подмены внутри туннеля.
//
//	GET http://10.77.77.1:8080/apps/<платформа>/latest.json
//	{"version":"0.5.28","build":528,"file":"MeridianSetup-0.5.28.exe","sha256":"…","size":N}
//	GET http://10.77.77.1:8080/apps/<платформа>/<file>

var tunnelAppsBase = "http://10.77.77.1:8080/apps/" + updatePlatform + "/"

const (
	tunnelFirstWait = 5 * time.Second // сколько ждём обычный канал при поднятом туннеле
	tunnelAttempt   = 5 * time.Second // соединение и заголовки на шлюзе
)

// tunnelIsUp — туннель поднят (задаётся из main: состояние интерфейса).
var tunnelIsUp = func() bool { return false }

func tunnelClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: tunnelAttempt}).DialContext,
			ResponseHeaderTimeout: tunnelAttempt,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func fetchTunnelManifest(ctx context.Context) (*updateManifest, error) {
	rctx, cancel := context.WithTimeout(ctx, tunnelAttempt)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, tunnelAppsBase+"latest.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := tunnelClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var in struct {
		Version string `json:"version"`
		File    string `json:"file"`
		SHA256  string `json:"sha256"`
		Size    int64  `json:"size"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, updateManifestMax)).Decode(&in); err != nil {
		return nil, fmt.Errorf("описание: %w", err)
	}
	if !mirrorNameRe.MatchString(in.File) || strings.Contains(in.File, "..") {
		return nil, fmt.Errorf("имя файла %q не годится", in.File)
	}
	m := &updateManifest{
		Version:   in.Version,
		URL:       tunnelAppsBase + in.File,
		SHA256:    strings.ToLower(strings.TrimSpace(in.SHA256)),
		Size:      in.Size,
		viaTunnel: true,
	}
	if _, ok := parseVersion(m.Version); !ok {
		return nil, fmt.Errorf("версия %q не a.b.c", m.Version)
	}
	if len(m.SHA256) != 64 {
		return nil, errors.New("длина sha256 не та")
	}
	if m.Size <= 0 || m.Size > updateMaxSize {
		return nil, fmt.Errorf("размер %d вне пределов", m.Size)
	}
	return m, nil
}

// tunnelDownload — файл с шлюза по url из описания (только базовый путь
// туннеля); nil — не вышло.
func tunnelDownload(ctx context.Context, m *updateManifest) *http.Response {
	if !strings.HasPrefix(m.URL, tunnelAppsBase) {
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(tunnelAttempt, cancel)
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, m.URL, nil)
	if err != nil {
		cancel()
		return nil
	}
	resp, err := tunnelClient().Do(req)
	if !timer.Stop() || err != nil || resp.StatusCode != http.StatusOK {
		if err == nil {
			resp.Body.Close()
		}
		cancel()
		log.Printf("обновление: файл через туннель не получен (%v)", err)
		return nil
	}
	resp.Body = cancelOnClose{resp.Body, cancel}
	log.Printf("обновление: файл через туннель")
	return resp
}
