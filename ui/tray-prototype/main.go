// Meridian VPN — общий интерфейс для macOS и Windows (Wails v3).
// Оформление — docs/desktop-design.md кота 3 (планета-кнопка, чёрная
// тема, тексты Android дословно). Говорит с помощником (macOS) или
// службой (Windows) по core/control.go через dialControl (host_*.go) —
// сеть, движок и TUN не трогает, это пульт и индикатор.
package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"meridian/engine/params"

	"github.com/JinComputers/meridian-desktop/core"
)

// Платформенное (host_darwin.go / host_windows.go): appSupportDir,
// deviceIDPrefix, dialControl, has/get/setSubscriberKey,
// hostSettingsMenuLabel/openHostSettings, launchedAtLogin,
// ensureServices/refreshAutostartItem/toggleAutostart/unregisterServices,
// autostartState, setTrayIcon, hostUnavailableText.

//go:embed frontend
var assets embed.FS

// Силуэт строки меню/трея — design/tray-mono.svg кота 3, растрирован
// scripts/svg2png.swift: «полный» — связь есть, «тусклый» (40%) — нет.
// Чёрный — template image macOS, белый — для тёмной панели Windows.
var (
	//go:embed frontend/tray/tray-full-black.png
	trayFullBlack []byte
	//go:embed frontend/tray/tray-dim-black.png
	trayDimBlack []byte
	//go:embed frontend/tray/tray-full-white.png
	trayFullWhite []byte
	//go:embed frontend/tray/tray-dim-white.png
	trayDimWhite []byte
)

// gateway — реальный шлюз; выбор шлюза человеку не открыт.
const gateway = "edge.jincomputers.win"

// uiKind — состояние для экрана: планета отображает off/busy/connected
// (ошибка — та же «off» плюс строка под планетой, design §1).
type uiKind string

const (
	uiDisconnected uiKind = "disconnected"
	uiConnecting   uiKind = "connecting"
	uiConnected    uiKind = "connected"
	uiError        uiKind = "error"
)

type uiState struct {
	mu                sync.Mutex
	kind              uiKind
	address           string // внешний адрес выхода — в подсказку значка
	notice            string // строка под планетой (design §4 Notice)
	noticeBad         bool   // синяя — плохая новость, серая — новость
	helperUnavailable bool   // notice относится к потерянной связи со службой
	hasKey            bool
	whiteMode         bool
	// sessionWhite — режим, с которым поднята ТЕКУЩАЯ сессия: сменили
	// переключатель во время сессии — «применится при следующем».
	sessionWhite bool
	// tunnelMTU — MTU туннеля последнего подключения (для лога), 0 — не было.
	tunnelMTU int
	// winner — ступень, на которой стоит текущая сессия ("R" — релей),
	// пусто — не подключены или старая служба (ControlStatePayload.Winner).
	winner string
}

func vkLinksPath() string       { return filepath.Join(appSupportDir(), "vk-links.txt") }
func directDomainsPath() string { return filepath.Join(appSupportDir(), "direct-domains.txt") }
func settingsPath() string      { return filepath.Join(appSupportDir(), "settings.json") }
func deviceIDPath() string      { return filepath.Join(appSupportDir(), "device-id.txt") }

// settings — выбор человека (всё пишется сразу, без «Сохранить»).
type settings struct {
	// WhiteMode — nil (новая установка, выбора не было) = белый режим
	// (решение владельца 27.09); сохранённый выбор прежних установок не трогаем.
	WhiteMode *bool `json:"white_mode,omitempty"`
	// AutostartOff — пользователь выключил автозапуск (Windows: запись Run
	// общая, из установщика, — отметка личная; host_windows.go).
	AutostartOff bool `json:"autostart_off,omitempty"`
	// KeyExpiresAt — срок ключа из /v1/key (unix-с; 0 — не прислан).
	// KeyConfirmed — с этим ключом туннель уже вставал. Строка доступа
	// в настройках (Access.keyText на Android).
	KeyExpiresAt int64 `json:"key_expires_at,omitempty"`
	KeyConfirmed bool  `json:"key_confirmed,omitempty"`
	// KeyPermanent — служба С ВЕРДИКТОМ прислала expires_at=0: бессрочный
	// ключ (кот 2: поле приходит всегда, 0 = бессрочный; Android Access.kt
	// так же). Нет вердикта (служба молчала) — не ставится: «срок неизвестен».
	KeyPermanent bool `json:"key_permanent,omitempty"`
	// KeyTrial — ключ выдан кнопкой «Начать 3 дня бесплатно» (сервер в ответе
	// /v1/key это не говорит, клиент помнит сам). KeyTrialEnd — срок на момент
	// выдачи: если потом срок заметно больше (ключ продлили), ключ уже не пробный.
	KeyTrial    bool  `json:"key_trial,omitempty"`
	KeyTrialEnd int64 `json:"key_trial_end,omitempty"`
	// WinX/WinY — куда человек перетащил окно (WinSaved); не перетаскивал —
	// окно открывается у значка (просьба владельца 24.09).
	WinSaved bool `json:"win_saved,omitempty"`
	WinX     int  `json:"win_x,omitempty"`
	WinY     int  `json:"win_y,omitempty"`
	// Обновления (update.go): переключателя «проверять» в интерфейсе нет
	// (решение владельца 27.09) — проверка идёт всегда. UpdateDismissed —
	// версия, строку о которой закрыли на главном экране; UpdateLastCheck —
	// последняя удачная проверка (unix-с); UpdateManifestURL — свой адрес
	// описания для стенда (в интерфейсе нет).
	UpdateDismissed   string `json:"update_dismissed,omitempty"`
	UpdateLastCheck   int64  `json:"update_last_check,omitempty"`
	UpdateManifestURL string `json:"update_manifest_url,omitempty"`
	// Тихая установка (update_auto_windows.go): версия, которую не удалось
	// поставить сама, и когда — до конца суток её сама не пробуем (кнопка
	// «Обновить» работает всегда).
	AutoUpdateFailVersion string `json:"auto_update_fail_version,omitempty"`
	AutoUpdateFailAt      int64  `json:"auto_update_fail_at,omitempty"`

	// AntiDPI*Off — обход DPI-блокировок в чёрном режиме (решение
	// владельца 27.09, разбор и данные кота 1, platform/windows/antidpi):
	// по умолчанию (поле отсутствует/false) ВСЕ группы включены — как на
	// роутере кота 1 («нет файла — включены все»), без выбора техники
	// обхода человеку. Тот же принцип omitempty, что у RUDirectOff/
	// AutostartOff выше.
	AntiDPIYouTubeOff    bool `json:"antidpi_youtube_off,omitempty"`
	AntiDPIDiscordOff    bool `json:"antidpi_discord_off,omitempty"`
	AntiDPICloudflareOff bool `json:"antidpi_cloudflare_off,omitempty"`
	AntiDPISocialOff     bool `json:"antidpi_social_off,omitempty"`
	AntiDPIOtherOff      bool `json:"antidpi_other_off,omitempty"`

	// TunnelApps — программы, трафик которых идёт в туннель в чёрном режиме
	// (apps.go, core.AppSplitRouter; решение владельца 27.10.2026).
	TunnelApps []tunnelApp `json:"tunnel_apps,omitempty"`

	// RelayPrimaryDion — основное звено ретранслятора DION вместо VK
	// (core.RelayPrimaryDion/VK, решение владельца 28.09 через кота 1).
	// По умолчанию false — VK основной, DION запасной. Действует только
	// при непустых комнатах (core.EffectiveRelayPrimary решает это, не
	// здесь) — держать этот факт снаружи settings, не задваивать правило.
	RelayPrimaryDion bool `json:"relay_primary_dion,omitempty"`

	// KeyTier — тариф ключа из /v1/key (core.KeyTier): "basic" — «Базовый»,
	// пусто — премиум. Последнее известное значение: служба молчит — не
	// трогаем (решение владельца через кота 1, 16.10).
	KeyTier        string             `json:"key_tier,omitempty"`
	Partner        *core.PartnerUsage `json:"partner,omitempty"`
	RoutingKeyHash string             `json:"routing_key_hash,omitempty"`
	RoutingScope   string             `json:"routing_scope,omitempty"`
}

// basic — тариф «Базовый»: белого режима нет совсем, DION не передаём.
func (st settings) basic() bool { return core.KeyTier(st.KeyTier).Basic() }

// trial — ключ пробный и ещё не продлён: срок не ушёл заметно дальше выданного
// (час запаса на округление сервера). Продлили — это уже обычный платный ключ.
func (st settings) trial() bool {
	return st.KeyTrial && (st.KeyTrialEnd == 0 || st.KeyExpiresAt <= st.KeyTrialEnd+3600)
}

func (st settings) antiDPIGroups() core.AntiDPIGroups {
	return core.AntiDPIGroups{
		YouTube:    !st.AntiDPIYouTubeOff,
		Discord:    !st.AntiDPIDiscordOff,
		Cloudflare: !st.AntiDPICloudflareOff,
		Social:     !st.AntiDPISocialOff,
		Other:      !st.AntiDPIOtherOff,
	}
}

func (st settings) whiteMode() bool {
	return !st.basic() && (st.WhiteMode == nil || *st.WhiteMode)
}

func loadSettings() settings {
	var st settings
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func saveSettings(st settings) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(appSupportDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0o600)
}

func updateSettings(f func(*settings)) {
	st := loadSettings()
	f(&st)
	if err := saveSettings(st); err != nil {
		log.Println("saveSettings:", err)
	}
}

func writeFile(path, text string) {
	if err := os.MkdirAll(appSupportDir(), 0o700); err != nil {
		log.Println(err)
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		log.Println(err)
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// vkLinks — ссылки на звонки, по одной в строке файла.
func vkLinks() []string {
	var out []string
	for _, l := range strings.Split(readFile(vkLinksPath()), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func saveVKLinks(links []string) {
	writeFile(vkLinksPath(), strings.Join(links, "\n")+"\n")
}

// loadOrCreateDeviceID — устойчивый id устройства для Envelope /v1/params.
func loadOrCreateDeviceID() (string, error) {
	// Сначала — от железа (hostDeviceID, хеш, не сам идентификатор): иначе
	// удаление/переустановка давали бы новый id и новый пробный период.
	if id := hostDeviceID(); id != "" {
		return id, nil
	}
	if data, err := os.ReadFile(deviceIDPath()); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	id := fmt.Sprintf("%s%d", deviceIDPrefix, time.Now().UnixNano())
	if err := os.MkdirAll(appSupportDir(), 0o700); err != nil {
		return "", err
	}
	return id, os.WriteFile(deviceIDPath(), []byte(id), 0o600)
}

// helperConn — соединение с помощником/службой, мьютекс на запись.
type helperConn struct {
	mu   sync.Mutex
	conn net.Conn
}

func (h *helperConn) send(typ core.ControlMessageType, payload any) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return fmt.Errorf("нет соединения с помощником")
	}
	return core.WriteControlMessage(h.conn, typ, payload)
}

type app struct {
	wails      *application.App
	window     *application.WebviewWindow
	tray       *application.SystemTray
	menuToggle *application.MenuItem
	menuAuto   *application.MenuItem
	menuUpdate *application.MenuItem

	// keyLink — ключ из ссылки meridian://key/, ждёт «Проверить» (или «Отмена»).
	keyLinkMu sync.Mutex
	keyLink   string

	// placedX/Y — куда окно поставили МЫ при последнем показе; при скрытии
	// другое положение значит «человек перетащил» — запоминаем.
	posMu            sync.Mutex
	placedX, placedY int

	// helperHint — почему помощник/служба недоступны (ждёт одобрения в
	// Настройках, не установлена…) — вместо голого "connection refused".
	hintMu     sync.Mutex
	helperHint string

	state uiState
	hc    helperConn
	// A cleanup failure is offered once until the helper confirms a clean state.
	networkRecoveryOffered atomic.Bool
	quitWarningOpen        atomic.Bool
	recoveryAvailable      atomic.Bool
	recoveryMode           atomic.Bool
	recoveryTemporary      atomic.Bool
	recoveryKeyMu          sync.Mutex
	recoveryKey            string

	deviceID string

	// connectGen — какая попытка подключения сейчас «текущая» (armSlowHint):
	// новая попытка отменяет подсказку «долго» от прошлой, даже если её
	// пятисекундный таймер ещё не сработал.
	connectGen atomic.Uint64

	// autoConnectPending — запущены автозапуском при входе: подключиться
	// один раз по первому ControlState (handleEnvelope).
	autoConnectPending atomic.Bool
	// keyInfoAsked — ключ в этом запуске уже читали (было подключение или
	// «Проверить»): на macOS только после этого окно перечитывает тариф.
	keyInfoAsked atomic.Bool
	// trialGrant — идёт проверка ключа, выданного кнопкой пробного периода
	// (startTrial): checkKey помечает его пробным; любой другой ключ — нет.
	trialGrant atomic.Bool
	// keyInfoAt — когда последний раз звали refreshKeyInfo (unix-с): окно и
	// подключение перечитывают тариф не чаще раза в 5 минут (refreshKeyInfoSoon).
	keyInfoAt atomic.Int64

	upd  *updater
	vk   vkChecker
	dion dionChecker

	// logsCh — ответ службы на ControlLogsQuery (logs.go).
	logsCh chan []string

	// captcha — ручная капча VK (captcha_flow.go).
	captcha captchaState
}

func main() {
	// -unregister-services — снять регистрации и выйти (удаление).
	if len(os.Args) == 2 && os.Args[1] == "-unregister-services" {
		os.Exit(unregisterServices())
	}
	// -captcha-window — окно ручной капчи VK (captcha_window_windows.go).
	if len(os.Args) == 2 && os.Args[1] == "-captcha-window" {
		os.Exit(runCaptchaWindow())
	}
	// -set-update-token — токен бета-канала из stdin (одна строка) в
	// хранилище (DPAPI/Keychain); не печатается. Пустая строка — удалить.
	// TrimPrefix BOM: `$tok | exe.exe -set-update-token` в Windows
	// PowerShell 5.1 пишет в stdin дочернего процесса UTF-8 с BOM (найдено
	// 25.09 — сохранённый токен не проходил проверку GitHub, 401), TrimSpace
	// сам BOM (U+FEFF) не убирает.
	if len(os.Args) == 2 && os.Args[1] == "-set-update-token" {
		line, _ := bufio.NewReader(os.Stdin).ReadString(10)
		line = strings.TrimPrefix(line, string(rune(0xFEFF)))
		if err := setUpdateToken(strings.TrimSpace(line)); err != nil {
			fmt.Fprintln(os.Stderr, "токен не сохранён:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	// Помощник обновления (host_*.go): ждёт установщик и запускает новую
	// версию — отдельным процессом, до Wails и до «одного экземпляра».
	if code, handled := runUpdateHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	after := parseAfterUpdate(os.Args[1:])
	setupUILog()

	deviceID, err := loadOrCreateDeviceID()
	if err != nil {
		log.Println("loadOrCreateDeviceID:", err)
		showStartupError("Не удалось сохранить идентификатор устройства. Проверьте доступ к каталогу настроек и запустите Meridian снова.")
		return
	}

	var window *application.WebviewWindow
	var theApp *app
	wapp := application.New(application.Options{
		// Один экземпляр: автозапуск и ручной запуск не дают два значка.
		// Второй ручной запуск показывает окно первого, второй «-login» /
		// «--autostart» — ничего.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "org.meridianvpn.app",
			OnSecondInstanceLaunch: func(d application.SecondInstanceData) {
				for _, arg := range d.Args {
					if arg == "-login" || arg == "--autostart" {
						return
					}
					// Windows: ссылка при уже запущенном приложении приходит
					// аргументом второго экземпляра, не ApplicationLaunchedWithUrl.
					if strings.Contains(arg, "://") && theApp != nil {
						theApp.openKeyLink(arg)
						return
					}
				}
				if theApp != nil {
					theApp.showWindow()
				}
			},
		},
		Name:        "Meridian VPN",
		Description: "Meridian VPN",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		Mac:         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		// Данные WebView2 — в наш каталог (удаление программы стирает его),
		// а не в %APPDATA%\meridian-ui.exe по умолчанию: тот оставался бы
		// следом после удаления. На macOS поле не используется.
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true, WebviewUserDataPath: filepath.Join(appSupportDir(), "WebView2")},
	})

	window = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Meridian VPN",
		Width:            360,
		Height:           720, // с карточкой «Базового» настройки влезают без прокрутки (владелец 17.10)
		Frameless:        false,
		Hidden:           false,
		DisableResize:    true,
		BackgroundColour: application.NewRGB(0, 0, 0),
		URL:              "/",
		Windows:          application.WindowsWindow{HiddenOnTaskbar: true},
	})

	// Keep the window reachable in the task switcher even without a tray.
	// A native close request must not leave a running VPN without an UI.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		window.Minimise()
	})

	tray := wapp.SystemTray.New()
	menu := wapp.NewMenu()
	toggleItem := menu.Add("Подключить")
	// «Обновить до …» — только когда есть новее (update.go).
	updateItem := menu.Add("Обновить")
	updateItem.SetHidden(true)
	menu.AddSeparator()
	settingsItem := menu.Add("Настройки…")
	autoItem := menu.AddCheckbox("Запускать при входе", false)
	if hostSettingsMenuLabel != "" {
		menu.Add(hostSettingsMenuLabel).OnClick(func(ctx *application.Context) { openHostSettings() })
	}
	menu.AddSeparator()
	quitItem := menu.Add("Выйти")
	tray.SetMenu(menu)
	// НЕ AttachWindow: тот каждый раз ставит окно у значка, а владелец
	// просит открывать там, куда окно перетащили (showWindow/hideWindow).
	tray.OnClick(func() {
		if window.IsVisible() && !window.IsMinimised() {
			theApp.hideWindow()
		} else {
			theApp.showWindow()
		}
	})

	a := &app{wails: wapp, window: window, tray: tray, menuToggle: toggleItem, menuAuto: autoItem, menuUpdate: updateItem, deviceID: deviceID}
	tunnelIsUp = func() bool {
		a.state.mu.Lock()
		defer a.state.mu.Unlock()
		return a.state.kind == uiConnected
	}
	a.upd = newUpdater(a)
	// Ссылка meridian://key/<ключ> (Мак — Apple Event, Windows — аргумент):
	// ключ подставляется в форму, применяется только по «Проверить».
	wapp.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		a.openKeyLink(e.Context().URL())
	})
	a.logsCh = make(chan []string, 1)
	theApp = a
	a.state.kind = uiDisconnected
	a.state.whiteMode = loadSettings().whiteMode()
	_, keyErr := getSubscriberKey()
	a.state.hasKey = keyErr == nil
	if keyErr != nil && !errors.Is(keyErr, errKeyAbsent) {
		a.state.notice = "Системное хранилище ключей недоступно. Проверьте Secret Service."
		a.state.noticeBad = true
	}
	a.autoConnectPending.Store(launchedAtLogin() || after.reconnect)
	a.upd.afterMsg = after.message

	toggleItem.OnClick(func(ctx *application.Context) { a.toggle() })
	settingsItem.OnClick(func(ctx *application.Context) { a.showWindow(); wapp.Event.Emit("show-settings", nil) })
	autoItem.OnClick(func(ctx *application.Context) { a.toggleAutostart(); a.applyLocal() })
	quitItem.OnClick(func(ctx *application.Context) { a.quitFromTray() })
	updateItem.SetHidden(true)

	on := func(name string, f func(data any)) {
		wapp.Event.On(name, func(ev *application.CustomEvent) { f(ev.Data) })
	}
	on("tray:toggle", func(any) { a.toggle() })
	on("tray:hide-window", func(any) { a.hideWindow() })
	on("tray:request-config", func(any) { a.applyLocal() })
	on("tray:set-white-mode", func(d any) { v, _ := d.(bool); a.setWhiteMode(v) })
	on("antidpi:set", func(d any) {
		m, _ := d.(map[string]any)
		group, _ := m["group"].(string)
		on, _ := m["on"].(bool)
		updateSettings(func(s *settings) {
			off := !on
			switch group {
			case "youtube":
				s.AntiDPIYouTubeOff = off
			case "discord":
				s.AntiDPIDiscordOff = off
			case "cloudflare":
				s.AntiDPICloudflareOff = off
			case "social":
				s.AntiDPISocialOff = off
			case "other":
				s.AntiDPIOtherOff = off
			}
		})
		a.applyLocal()
	})
	// Комнаты DION — одна кнопка «Добавить и проверить» (договор кота 2):
	// пустой ввод — проверить сохранённые, непустой — добавить, сохранить,
	// проверить только что добавленную (как у VK, addVKLink).
	on("dion:go", func(d any) {
		raw, _ := d.(string)
		raw = strings.TrimSpace(raw)
		if raw == "" {
			go a.checkDion(nil)
			return
		}
		a.addDionRoom(raw)
	})
	on("dion:delete", func(d any) { a.deleteDionRoom(d) })
	on("dion:set-primary", func(d any) {
		primary, _ := d.(string)
		updateSettings(func(s *settings) { s.RelayPrimaryDion = primary == core.RelayPrimaryDion })
		a.applyLocal()
	})
	on("trial:start", func(any) { go a.startTrial() })
	on("key:recover", func(any) { a.recoveryMode.Store(true) })
	on("key:recover-cancel", func(any) { a.recoveryMode.Store(false) })
	on("key:check", func(d any) { k, _ := d.(string); a.takeKeyLink(); go a.checkKey(strings.TrimSpace(k)) })
	on("keylink:get", func(any) { a.emitKeyLink() })
	on("keylink:cancel", func(any) { a.takeKeyLink() })
	on("vk:add", func(d any) { s, _ := d.(string); a.addVKLink(s) })
	on("vk:delete", func(d any) { a.deleteVKLink(d) })
	on("direct:set", func(d any) { s, _ := d.(string); writeFile(directDomainsPath(), s); a.applyLocal() })
	on("list:set", func(d any) {
		m, _ := d.(map[string]any)
		mode, _ := m["mode"].(string)
		text, _ := m["text"].(string)
		writeFile(listPath(mode), text)
		a.applyLocal()
	})
	on("list:import", func(d any) { mode, _ := d.(string); go a.importList(mode) })
	on("list:export", func(d any) { mode, _ := d.(string); go a.exportList(mode) })
	on("apps:list", func(any) { go a.appsList() })
	on("apps:add", func(d any) {
		m, _ := d.(map[string]any)
		name, _ := m["name"].(string)
		path, _ := m["path"].(string)
		a.appsAdd(name, path)
	})
	on("apps:remove", func(d any) { path, _ := d.(string); a.appsRemove(path) })
	on("apps:browse", func(any) { go a.appsBrowse() })
	on("vk:check", func(any) { go a.checkVK(nil) })
	on("logs:get", func(any) { go a.logsPreview() })
	on("logs:save", func(any) { go a.saveLogs() })
	on("logs:share", func(any) { go a.shareLogs() })
	on("logs:copy", func(any) { go a.copyLogs() })
	on("autostart:set", func(d any) {
		want, _ := d.(bool)
		if _, enabled := autostartState(); enabled != want {
			a.toggleAutostart()
		}
		a.applyLocal()
	})
	on("captcha:solve", func(any) { go a.solveCaptcha() })
	on("update:check", func(any) {})
	on("update:start", func(any) {})
	on("update:dismiss", func(any) { a.upd.dismiss() })
	// «Повысить до Премиума»: ссылка считается здесь — ключ в окно не уходит.
	on("upgrade:open", func(any) {
		if !loadSettings().basic() {
			return
		}
		if key, err := a.currentSubscriberKey(); err == nil && key != "" {
			// Сначала приложение Telegram, нет его — браузер.
			if !hasTelegramHandler() || wapp.Browser.OpenURL(core.UpgradeAppURL(key)) != nil {
				_ = wapp.Browser.OpenURL(core.UpgradeURL(key))
			}
		}
	})
	on("open-url", func(d any) {
		if u, _ := d.(string); strings.HasPrefix(u, "https://") || u == "mailto:support@meridianvpn.org" {
			_ = wapp.Browser.OpenURL(u)
		}
	})

	a.ensureServices()
	setTrayIcon(tray, false)
	a.applyLocal()
	go a.trayLoop()
	go a.connectLoop()
	// Linux updates are not yet supported.

	if keyInfoAtStart {
		go func() { time.Sleep(5 * time.Second); a.refreshKeyInfo() }()
	}
	if after.message != "" {
		// Перезапуск после обновления — показать, чем кончилось.
		go func() { time.Sleep(time.Second); a.showWindow() }()
	}

	if err := wapp.Run(); err != nil {
		log.Fatal(err)
	}
}

// connectLoop — держит соединение с помощником/службой: dial, чтение,
// при обрыве — пауза и снова. Недоступен — строка под планетой, не падаем.
func helperDialFailureCategory(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "сокет отсутствует"
	case errors.Is(err, os.ErrPermission):
		return "нет доступа к сокету"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "сокет не принимает соединения"
	default:
		return "ошибка соединения или рукопожатия"
	}
}

func (a *app) connectLoop() {
	lastFailure := ""
	for {
		conn, err := dialControl()
		if err != nil {
			category := helperDialFailureCategory(err)
			if category != lastFailure {
				log.Printf("помощник: канал недоступен: %s", category)
				lastFailure = category
			}
			a.hintMu.Lock()
			hint := a.helperHint
			a.hintMu.Unlock()
			if runtime.GOOS == "darwin" || hint == "" {
				hint = hostUnavailableText(err)
			}
			a.setHelperUnavailable(hint)
			time.Sleep(3 * time.Second)
			continue
		}
		log.Print("помощник: канал и рукопожатие установлены")
		lastFailure = ""
		a.setHint("") // A successful connection makes any old startup hint obsolete.
		a.hc.mu.Lock()
		a.hc.conn = conn
		a.hc.mu.Unlock()
		// Сеанс VPN живёт у помощника/службы независимо от интерфейса —
		// сразу спросить состояние. Ответ заодно запускает автоподключение.
		if err := a.hc.send(core.ControlStatusQuery, nil); err != nil {
			log.Println("StatusQuery:", err)
		}
		a.readLoop(conn)
		log.Print("помощник: канал закрыт, повторяю соединение")
		a.hc.mu.Lock()
		a.hc.conn = nil
		a.hc.mu.Unlock()
		conn.Close()
		time.Sleep(2 * time.Second)
	}
}

func (a *app) readLoop(conn net.Conn) {
	for {
		env, err := core.ReadControlMessage(conn)
		if err != nil {
			if err != io.EOF {
				log.Println("ReadControlMessage:", err)
			}
			return
		}
		a.handleEnvelope(env)
	}
}

func (a *app) handleEnvelope(env core.ControlEnvelope) {
	switch env.Type {
	case core.ControlState:
		st, err := core.DecodeControlPayload[core.ControlStatePayload](env)
		if err != nil {
			return
		}
		if !st.Connected {
			a.networkRecoveryOffered.Store(false)
		}
		a.state.mu.Lock()
		a.state.clearHelperUnavailable()
		wasError := a.state.kind == uiError
		// Подсказка «долго» живёт только пока идёт подъём (как Android:
		// Notice.clear() на успехе) — подключились или нет, её снимаем,
		// а таймер прошлой попытки гасим, чтобы не поставил её позже.
		a.connectGen.Add(1)
		if a.state.notice == slowHintText {
			a.state.notice = ""
		}
		if st.TunnelInfo != nil {
			a.state.tunnelMTU = int(st.TunnelInfo.MTU)
		}
		a.state.winner = ""
		switch {
		case st.Connected:
			a.state.kind = uiConnected
			a.state.winner = st.Winner
			// Строка-ошибка исчезает при следующем удачном подключении.
			if a.state.noticeBad {
				a.state.notice, a.state.noticeBad = "", false
			}
		case wasError:
			// Помощник подтверждает «не подключены» после ошибки — ошибку
			// не затираем, её человек должен прочитать.
		default:
			a.state.kind = uiDisconnected
			a.state.address = ""
			if a.state.notice == "Отключение…" {
				a.state.notice = ""
			}
		}
		a.state.mu.Unlock()
		if st.Connected {
			updateSettings(func(s *settings) { s.KeyConfirmed = true })
			go a.refreshExitAddress()
			// Тариф — после каждого подключения (после оплаты повышения
			// карточка «Базовый» уходит сама). Ключ к этому моменту прочитан.
			a.keyInfoAsked.Store(true)
			a.refreshKeyInfoSoon()
		}
		a.applyLocal()
		if a.autoConnectPending.CompareAndSwap(true, false) && !st.Connected {
			a.state.mu.Lock()
			hasKey := a.state.hasKey
			a.state.mu.Unlock()
			if hasKey {
				go a.sendConnect()
			}
		}
	case core.ControlReconnecting:
		a.state.mu.Lock()
		a.state.kind = uiConnecting
		a.state.address = ""
		a.state.winner = ""
		a.state.mu.Unlock()
		a.armSlowHint()
		a.applyLocal()
	case core.ControlError:
		ep, err := core.DecodeControlPayload[core.ControlErrorPayload](env)
		if err != nil {
			return
		}
		// Старая служба не знает запроса логов — это не отказ подключения.
		if ep.Code == "unknown_message" {
			if ep.Message == string(core.ControlLogsQuery) {
				a.deliverLogs(nil)
			}
			return
		}
		log.Printf("отказ: code=%s failure=%s: %s", ep.Code, ep.Failure, ep.Message)
		a.setNotice(uiError, withConflictHint(connectFailureText(ep), ep), true)
		if ep.Code == "cleanup_failed" {
			a.offerNetworkRecovery()
		}
	case core.ControlCaptchaRequest:
		if cp, err := core.DecodeControlPayload[core.ControlCaptchaPayload](env); err == nil {
			a.onCaptchaRequest(cp)
		}
	case core.ControlLogs:
		lp, err := core.DecodeControlPayload[core.ControlLogsPayload](env)
		if err != nil {
			return
		}
		a.deliverLogs(lp.Lines)
	}
}

// retryHint — отказ из-за сервера или его адреса: повторное нажатие на планету
// заново запрашивает /v1/params, и Кот 1 мог уже подменить мёртвый вход (24.10).
const retryHint = "Нажмите на планету ещё раз — адрес сервера мог обновиться. "

// noLinksText — нечем поднять обход: нет ссылок на звонки VK и комнат DION
// (текст по правке Кота 3 от 19.10; слово «хеш» людям не показываем). На
// «Базовом» DION недоступен — о нём не говорим.
func noLinksText(basic bool, path string) string {
	if basic {
		return "Нет ссылок на звонки VK: добавьте их в " + path
	}
	return "Нет ссылок на звонки VK или комнат DION: добавьте их в " + path
}

// failureText — строка под планетой при отказе (design §4): по коду
// движка (engine.FailureKind через ControlErrorPayload.Failure), иначе
// общее «Не удалось подключиться: <причина>».
func failureText(ep core.ControlErrorPayload) string {
	if strings.Contains(ep.Message, "physical_route_tunnel") {
		return "Сеть использует другой туннель. Проверьте другие VPN и сетевые фильтры; если они включены, отключите их и попробуйте снова."
	}
	const path = "«Настройки → Обход белых списков»"
	const blocked = "Сервер недоступен из этой сети напрямую — похоже, оператор включил белые списки. "
	switch ep.Failure {
	case "blocked-nolinks":
		return blocked + noLinksText(loadSettings().basic(), path)
	case "blocked-linksdead":
		return blocked + "Все ссылки на ВК-звонки больше не работают: добавьте ссылку на звонок ВКонтакте в " + path + ", свежую"
	case "blocked-captcha":
		// На Android — «решите её из уведомления»; решателя капчи на
		// компьютере нет — формулировка на утверждение владельцу.
		return blocked + "ВКонтакте просит капчу: подключитесь позже или добавьте ссылку на звонок ВКонтакте в " + path + ", другую"
	case "blocked-links":
		return blocked + "ВКонтакте не дал доступ по ссылкам на звонки: повторите позже или добавьте ссылку на звонок ВКонтакте в " + path + ", другую"
	case "blocked-relay":
		return "Сервер недоступен из этой сети ни напрямую, ни через ВК-звонок. " + retryHint + "Не помогло — попробуйте другую сеть (например, Wi-Fi) или повторите позже"
	case "blocked":
		return "Сервер недоступен из этой сети. " + retryHint + "Не помогло — попробуйте другую сеть (например, Wi-Fi) или повторите позже"
	case "silent":
		return "Сервер не узнал ключ. Если ключ получен только что — подождите минуту и подключитесь снова"
	}
	if ep.Code == core.ControlErrAlreadyRunning {
		return ""
	}
	switch ep.Failure {
	case core.FailureSystemDNSUnavailable:
		return "Не удалось безопасно настроить DNS. Проверьте, что systemd-resolved запущен и системный DNS использует 127.0.0.53; затем повторите подключение."
	case core.FailureAPIUnavailable:
		return "Не удалось связаться с серверами Meridian. Возможно, сеть или провайдер блокирует соединение. Попробуйте другую сеть (например, раздачу с телефона) или укажите в настройках сети DNS 1.1.1.1"
	case "resolve":
		return "Адрес сервера не находится, проверьте интернет"
	}
	// Коды network/refused/unknown и ошибки до движка — строка с причиной
	// по маркерам текста, как Android humanReason (Paths.kt, те же
	// маркеры и слова). Причина не узнана — просто «Не удалось
	// подключиться», без повтора того же другими словами.
	m := ep.Message
	if strings.Contains(m, "FATAL_TIER_RELAY") {
		return "На тарифе «Базовый» этот способ недоступен"
	}
	reason := ""
	switch {
	case strings.Contains(m, "FATAL_POOL_FULL"):
		reason = "сервер сейчас переполнен. " + strings.TrimSpace(retryHint)
	case strings.Contains(m, "FATAL_BOUND_OTHER"):
		reason = "ключ уже привязан к другому устройству. Один ключ работает только на одном устройстве. Если вы сменили устройство — обратитесь в поддержку"
	case strings.Contains(m, "FATAL_AUTH"):
		reason = "ключ не принят сервером"
	case strings.Contains(m, "FATAL_DEACTIVATED"):
		reason = "ключ отключён — напишите в поддержку"
	case strings.Contains(m, "шлюз не ответил"):
		reason = "сервер не ответил. " + strings.TrimSpace(retryHint)
	case strings.Contains(m, "нет ключа доступа"):
		reason = "нет ключа доступа"
	case strings.Contains(m, "сеть"):
		reason = "сеть сменилась во время подключения"
	case strings.Contains(m, "не остановилась"):
		reason = "прежнее подключение не завершилось"
	}
	if reason == "" {
		return "Не удалось подключиться"
	}
	return "Не удалось подключиться: " + reason
}

// noNetworkText — отказ при отсутствии сети: называть прямо (Android,
// баг 29.08: «подключиться не удалось» врал про причину).
const noNetworkText = "Нет интернета"

// connectFailureText — failureText, но сначала проверка сети: сети нет —
// так и пишем, какой бы код ни прислала служба.
func connectFailureText(ep core.ControlErrorPayload) string {
	if ep.Code == "cleanup_failed" {
		return "Не удалось восстановить сеть. Обратитесь в поддержку; не удаляйте журнал Meridian."
	}
	if ep.Code != core.ControlErrAlreadyRunning && networkKind() == "нет" {
		return noNetworkText
	}
	return failureText(ep)
}

// setNotice — состояние + строка под планетой.
func (a *app) setNotice(kind uiKind, text string, bad bool) {
	a.state.mu.Lock()
	a.state.kind = kind
	a.state.notice, a.state.noticeBad = text, bad
	a.state.helperUnavailable = false
	if kind != uiConnected {
		a.state.address = ""
	}
	a.state.mu.Unlock()
	a.applyLocal()
}

// Clear only the service-unavailable notice once a fresh status arrives.
func (s *uiState) clearHelperUnavailable() {
	if !s.helperUnavailable {
		return
	}
	s.helperUnavailable = false
	s.notice, s.noticeBad = "", false
	if s.kind == uiError {
		s.kind = uiDisconnected
	}
}

func (a *app) setHelperUnavailable(text string) {
	a.state.mu.Lock()
	a.state.kind, a.state.notice, a.state.noticeBad = uiError, text, true
	a.state.helperUnavailable = true
	a.state.address = ""
	a.state.mu.Unlock()
	a.applyLocal()
}

// slowHintDelay/slowHintText — тот же срок и текст, что на Android
// (MeridianVpnService.kt: SLOW_HINT_MS/SLOW_HINT), чтобы люди на разных
// платформах не путались в формулировках.
const slowHintDelay = 5 * time.Second
const slowHintText = "Подключение занимает больше обычного — пробую запасные пути"

// armSlowHint — вызывать сразу после того, как состояние стало
// uiConnecting (первое подключение, sendConnect; незапланированный
// разрыв, core.ControlReconnecting). gen отличает эту попытку от
// следующей: подключились/отказали/нажали «Отключить»/начался новый
// разрыв — прежний таймер эту попытку уже не трогает, даже сработав позже.
func (a *app) armSlowHint() {
	gen := a.connectGen.Add(1)
	time.AfterFunc(slowHintDelay, func() {
		a.state.mu.Lock()
		show := a.state.kind == uiConnecting && a.state.notice == "" && a.connectGen.Load() == gen
		if show {
			a.state.notice = slowHintText
		}
		a.state.mu.Unlock()
		if show {
			a.applyLocal()
		}
	})
}

func (a *app) setHint(h string) {
	a.hintMu.Lock()
	a.helperHint = h
	a.hintMu.Unlock()
}

// refreshExitAddress — внешний адрес выхода (1.1.1.1/cdn-cgi/trace,
// поле ip=) — в подсказку значка; на главном экране его нет (design §1:
// на экране ровно планета, доступ, шестерёнка).
func (a *app) refreshExitAddress() {
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Get("https://1.1.1.1/cdn-cgi/trace")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, line := range strings.Split(string(body), "\n") {
		if ip, ok := strings.CutPrefix(line, "ip="); ok {
			a.state.mu.Lock()
			if a.state.kind == uiConnected {
				a.state.address = ip
			}
			a.state.mu.Unlock()
			a.applyLocal()
			return
		}
	}
}

// toggle — нажатие на планету и пункт меню: выключено → подключить,
// подключение → остановить, подключено → отключить (design §1).
func (a *app) toggle() {
	a.state.mu.Lock()
	kind, hasKey := a.state.kind, a.state.hasKey
	a.state.mu.Unlock()
	switch kind {
	case uiConnected, uiConnecting:
		// Do not claim the network is restored until the helper confirms it.
		a.setNotice(uiConnecting, "Отключение…", false)
		if err := a.hc.send(core.ControlDisconnect, nil); err != nil {
			a.setNotice(uiError, "Служба Meridian не отвечает. Проверьте подключение и восстановление сети.", true)
		}
	default:
		if !hasKey {
			_, err := getSubscriberKey()
			if err != nil && !errors.Is(err, errKeyAbsent) {
				a.setNotice(uiError, "Системное хранилище ключей недоступно. Проверьте Secret Service.", true)
				return
			}
			if err == nil {
				a.state.mu.Lock()
				a.state.hasKey = true
				a.state.mu.Unlock()
				hasKey = true
			}
		}
		if !hasKey {
			a.showWindow()
			a.wails.Event.Emit("show-key-form", nil)
			return
		}
		a.sendConnect()
	}
}

var keychainStatusPattern = regexp.MustCompile(`OSStatus (-?[0-9]+)`)

func keychainErrorCode(err error) string {
	if err == nil {
		return "unknown"
	}
	m := keychainStatusPattern.FindStringSubmatch(err.Error())
	if len(m) != 2 {
		return "unknown"
	}
	return m[1]
}

// Only allow fixed, non-sensitive labels from the native Keychain bridge.
func keychainDefaultStatus(err error) string {
	if err != nil && keychainErrorCode(err) == "-25293" {
		for _, state := range []string{"locked", "unlocked", "unknown", "unavailable"} {
			if strings.Contains(err.Error(), "default-keychain="+state) {
				return state
			}
		}
	}
	return "unknown"
}

func keychainErrorDetails(err error) (string, string) {
	record, operation := "unknown", "unknown"
	if err == nil {
		return record, operation
	}
	msg := err.Error()
	for _, candidate := range []string{"legacy", "recovery", "selected"} {
		if strings.Contains(msg, "record="+candidate) {
			record = candidate
			break
		}
	}
	for _, candidate := range []string{"SecItemCopyMatching", "SecItemAdd", "SecItemUpdate"} {
		if strings.Contains(msg, candidate) {
			operation = candidate
			break
		}
	}
	return record, operation
}

func (a *app) currentSubscriberKey() (string, error) {
	a.recoveryKeyMu.Lock()
	key := a.recoveryKey
	a.recoveryKeyMu.Unlock()
	if key != "" {
		return key, nil
	}
	return getSubscriberKey()
}
func (a *app) setRecoveryKey(key string) {
	a.recoveryKeyMu.Lock()
	a.recoveryKey = key
	a.recoveryKeyMu.Unlock()
}

func shouldBlockOfflineConnect(network string) bool { return network == "нет" }

func (a *app) sendConnect() {
	log.Print("подключение: запрос пользователя")
	if shouldBlockOfflineConnect(networkKind()) {
		log.Print("подключение: macOS сообщает, что сеть недоступна")
		a.setNotice(uiError, noNetworkText, true)
		return
	}
	key, err := a.currentSubscriberKey()
	if err != nil {
		if errors.Is(err, errKeyAbsent) {
			a.setNotice(uiError, "Ключ не сохранён.", true)
		} else {
			a.setNotice(uiError, "Системное хранилище ключей недоступно. Проверьте Secret Service.", true)
		}
		return
	}
	st := loadSettings()
	if st.RoutingKeyHash != core.KeyFingerprint(key) || st.RoutingScope != "ordinary" || st.Partner != nil {
		platform := map[string]string{"darwin": "macos"}[runtime.GOOS]
		if platform == "" {
			platform = runtime.GOOS
		}
		client := core.NewParamsClient(core.ParamsConfig{Envelope: map[string]string{
			"device_id": a.deviceID, "platform": platform, "app_version": appVersion, "app_code": "0",
		}})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		ans, partner, err := core.CheckKeyWithPartner(ctx, client, key)
		ordinary := err == nil && ans != nil && !ans.NoVerdict && ans.State == params.KeyActive && partner == nil && core.OrdinaryRoutingConfirmed(ctx, client, key)
		partnerReady := err == nil && ans != nil && !ans.NoVerdict && ans.State == params.KeyActive && partner != nil && partner.BlockedReason == ""
		cancel()
		if ordinary || partnerReady {
			log.Print("подключение: тип маршрута подтверждён")
			if partnerReady {
				updateSettings(func(s *settings) {
					s.RoutingKeyHash = core.KeyFingerprint(key)
					s.RoutingScope = "partner"
					s.Partner = partner
				})
			} else {
				updateSettings(func(s *settings) {
					s.RoutingKeyHash = core.KeyFingerprint(key)
					s.RoutingScope = "ordinary"
					s.Partner = nil
				})
			}
		} else {
			if partner != nil {
				updateSettings(func(s *settings) {
					s.RoutingKeyHash = core.KeyFingerprint(key)
					s.RoutingScope = "partner"
					s.Partner = partner
				})
			}
			log.Print("подключение: тип маршрута не подтверждён или партнёрский маршрут недоступен")
			msg := "Тип маршрута ключа не подтверждён или партнёрский шлюз ещё не настроен"
			if partner != nil && partner.BlockedReason != "" {
				msg = partnerUsageText(partner)
			}
			a.setNotice(uiDisconnected, msg, true)
			return
		}
	}
	// Мёртвые ссылки (проверка: «мёртвый») на подключении не пробуем.
	hashes := connectHashes(core.ParseHashes(readFile(vkLinksPath())), loadVKStatuses())
	var dionSlugs []string
	for _, u := range dionRooms() {
		if s, bad := core.ParseDionRooms(u); len(bad) == 0 && len(s) == 1 {
			dionSlugs = append(dionSlugs, s[0])
		}
	}
	relayPrimary := core.RelayPrimaryVK
	if loadSettings().RelayPrimaryDion {
		relayPrimary = core.RelayPrimaryDion
	}
	// «Базовый»: DION шлюз не пускает (FATAL_TIER_RELAY) — комнаты не
	// передаём вовсе, запасной путь только VK.
	if loadSettings().basic() {
		dionSlugs, relayPrimary = nil, core.RelayPrimaryVK
	}
	payload := core.ControlConnectPayload{
		KeyMaterial:     key,
		PartnerRouting:  loadSettings().RoutingScope == "partner",
		DisableAutoDion: loadSettings().basic(),
		Gateway:         gateway,
		DeviceID:        a.deviceID,
		DionRooms:       dionSlugs,
		RelayPrimary:    connectRelayPrimary(relayPrimary, dionSlugs, dionStatusTexts(dionRooms())),
		// Слоты релея: 48 сразу, потолок 48, без автоподбора (решение
		// владельца 26.09, engine-v1.8.0) — на телефоне подняло релей с
		// 8–15 до ~80 Мбит/с. Шлюз держит потолок 50 (кот 1), клиентский
		// ниже на 2. Применяется только к релейной ступени — в чёрном
		// режиме без релея в лестнице эти поля ни на что не влияют.
		Ladder: core.LadderOptions{Hashes: hashes, SlotsStart: 48, SlotsMax: 48, NoAutoSlots: true},
	}
	// Linux currently supports a full tunnel only. Split-tunnel options are
	// deliberately omitted until the service implements and verifies them.
	a.state.mu.Lock()
	a.state.sessionWhite = true
	a.state.mu.Unlock()
	a.setNotice(uiConnecting, "", false)
	a.armSlowHint()
	if err := a.hc.send(core.ControlConnect, payload); err != nil {
		log.Print("подключение: команда не доставлена помощнику")
		a.setNotice(uiError, "Служба MeridianVPN не отвечает, повторите через несколько секунд", true)
	} else {
		log.Print("подключение: команда отправлена помощнику")
	}
}

// checkKey — «Проверить» (design §4, Android AccessScreen.applyVerdict):
// спросить /v1/key. Годен — сохранить и подключиться; служба отказала по
// ключу — сказать почему, не сохранять; служба молчит — сохранить и
// проверить подключением (молчание сервера не повод отказать человеку).
// persistRecoveredKey accepts persistence only after reading the same key back.
func persistRecoveredKey(key string, save func(string) error, load func() (string, error)) bool {
	if err := save(key); err != nil {
		record, operation := keychainErrorDetails(err)
		log.Printf("Keychain: восстановление, сохранение не удалось; запись=%s операция=%s OSStatus=%s связка-по-умолчанию=%s", record, operation, keychainErrorCode(err), keychainDefaultStatus(err))
		return false
	}
	got, err := load()
	if err != nil {
		record, operation := keychainErrorDetails(err)
		log.Printf("Keychain: восстановление, проверка чтением не удалась; запись=%s операция=%s OSStatus=%s связка-по-умолчанию=%s", record, operation, keychainErrorCode(err), keychainDefaultStatus(err))
		return false
	}
	if got != key {
		log.Print("Keychain: восстановление, проверка чтением не прошла: данные не совпадают")
		return false
	}
	log.Print("Keychain: восстановление, сохранение и проверка чтением успешны")
	return true
}

func canUseTemporaryKey(ans *params.KeyAnswer, partner *core.PartnerUsage, err error) bool {
	return err == nil && ans != nil && !ans.NoVerdict && ans.State == params.KeyActive && ans.Bound != "other" && (partner == nil || partner.BlockedReason == "")
}

func (a *app) checkKey(key string) {
	reply := func(ok bool, msg string) {
		a.wails.Event.Emit("key-result", map[string]any{"ok": ok, "error": msg})
	}
	k, ok := core.ParseKeyInput(key)
	if !ok {
		if strings.Contains(key, "://") {
			reply(false, "это не ссылка на ключ Meridian (нужна ссылка meridian://…)")
		} else {
			reply(false, "ключ слишком короткий")
		}
		return
	}
	key = k
	platform := map[string]string{"darwin": "macos"}[runtime.GOOS]
	if platform == "" {
		platform = runtime.GOOS
	}
	client := core.NewParamsClient(core.ParamsConfig{Envelope: map[string]string{
		"device_id": a.deviceID, "platform": platform, "app_version": appVersion, "app_code": "0",
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ans, partner, err := core.CheckKeyWithPartner(ctx, client, key)
	if a.recoveryMode.Load() && partner != nil && partner.BlockedReason != "" {
		reply(false, partnerUsageText(partner))
		return
	}

	notice, expires := "", int64(0)
	switch {
	case err != nil || ans == nil || ans.NoVerdict:
		notice = "сервер недоступен — проверяю подключением"
	case partner != nil && partner.BlockedReason != "":
		if e := setSubscriberKey(key); e != nil {
			reply(false, "ключ не сохранился")
			return
		}
		updateSettings(func(s *settings) {
			s.Partner = partner
			s.RoutingKeyHash = core.KeyFingerprint(key)
			s.RoutingScope = "partner"
			s.KeyExpiresAt = ans.ExpiresAt
		})
		a.state.mu.Lock()
		a.state.hasKey = true
		a.state.mu.Unlock()
		reply(true, "")
		a.setNotice(uiDisconnected, partnerUsageText(partner), true)
		a.applyLocal()
		return
	case ans.State == params.KeyRotated:
		reply(false, "этот ключ заменён при переносе на другое устройство — возьмите новый в боте")
		return
	case ans.Bound == "other":
		reply(false, "этот ключ уже используется на другом устройстве")
		return
	case ans.State.Dead() || ans.State == params.KeyUnknown:
		reply(false, keyProblemText(ans))
		return
	default:
		notice, expires = "Ключ принят", ans.ExpiresAt
	}
	if a.recoveryMode.Load() {
		if !canUseTemporaryKey(ans, partner, err) {
			reply(false, "Не удалось подтвердить ключ. Проверьте интернет и попробуйте ещё раз.")
			return
		}
		stored := persistRecoveredKey(key, setRecoveredSubscriberKey, getSubscriberKey)
		a.recoveryTemporary.Store(!stored)
		if stored {
			a.setRecoveryKey("")
		} else {
			a.setRecoveryKey(key)
			log.Print("Keychain: сохранение для восстановления не подтверждено")
		}
		if tier, ok := core.KeyTierOf(ans); ok {
			a.applyKeyTier(tier)
		}
		a.recoveryMode.Store(false)
		a.recoveryAvailable.Store(false)
		a.state.mu.Lock()
		a.state.hasKey = true
		a.state.mu.Unlock()
		reply(true, "")
		if stored {
			a.setNotice(uiDisconnected, "Ключ сохранён в Связке ключей.", false)
		} else {
			a.setNotice(uiDisconnected, "Ключ используется до закрытия приложения: сохранить в Связке ключей не удалось.", true)
		}
		a.sendConnect()
		return
	}
	if err := setSubscriberKey(key); err != nil {
		reply(false, "ключ не сохранился: "+err.Error())
		return
	}
	a.setRecoveryKey("")
	a.recoveryTemporary.Store(false)
	// Бессрочный — ТОЛЬКО вердикт ok и срок 0 (кот 2, checkapi.go:229–256:
	// у не-ok вердиктов срок просто не заполнен и тоже 0).
	permanent := err == nil && ans != nil && !ans.NoVerdict && ans.State == params.KeyActive && ans.ExpiresAt == 0
	trial := a.trialGrant.Load()
	updateSettings(func(s *settings) {
		s.KeyExpiresAt, s.KeyConfirmed, s.KeyPermanent = expires, false, permanent
		s.KeyTrial, s.KeyTrialEnd = trial, 0 // новый ключ — пробный только если выдан кнопкой
	})
	// Тариф — до подключения: новый ключ «Базового» не должен подняться в
	// белом режиме. Служба молчит — последнее известное значение.
	if t, ok := core.KeyTierOf(ans); err == nil && ok {
		a.applyKeyTier(t)
	}
	if err == nil && ans != nil && !ans.NoVerdict {
		updateSettings(func(s *settings) { s.Partner = partner })
	}
	if err == nil && ans != nil && !ans.NoVerdict {
		scope := "unknown"
		if partner != nil {
			scope = "partner"
		} else if core.OrdinaryRoutingConfirmed(ctx, client, key) {
			scope = "ordinary"
		}
		updateSettings(func(s *settings) { s.RoutingKeyHash = core.KeyFingerprint(key); s.RoutingScope = scope })
	}

	a.keyInfoAsked.Store(true)
	a.keyInfoAt.Store(time.Now().Unix())
	a.state.mu.Lock()
	a.state.hasKey = true
	a.state.mu.Unlock()
	reply(true, "")
	a.setNotice(uiDisconnected, notice, false)
	a.sendConnect()
}

// startTrial — «Начать 3 дня бесплатно»: POST /v1/trial с device_id; выдан
// ключ — тот же путь, что у «Проверить». Ключ в лог не пишем.
func (a *app) startTrial() {
	reply := func(msg string) {
		a.wails.Event.Emit("trial-result", map[string]any{"error": msg})
	}
	platform := map[string]string{"darwin": "macos"}[runtime.GOOS]
	if platform == "" {
		platform = runtime.GOOS
	}
	client := core.NewParamsClient(core.ParamsConfig{Envelope: map[string]string{
		"device_id": a.deviceID, "platform": platform, "app_version": appVersion, "app_code": "0",
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ans, err := client.StartTrial(ctx, a.deviceID)
	switch {
	case err != nil || ans == nil:
		log.Printf("Пробный период: сервер не ответил")
		reply("Сервер не отвечает. Проверьте интернет и повторите через минуту.")
	case ans.Granted():
		log.Printf("Пробный период: ключ выдан (повтор: %v)", ans.Repeat)
		a.trialGrant.Store(true)
		a.checkKey(ans.Key)
		a.trialGrant.Store(false)
		// Срок пробного: из /v1/key; служба молчала — из ответа на выдачу.
		updateSettings(func(s *settings) {
			if s.KeyTrial {
				if s.KeyExpiresAt == 0 && ans.ExpiresAt > 0 {
					s.KeyExpiresAt = ans.ExpiresAt
				}
				s.KeyTrialEnd = s.KeyExpiresAt
			}
		})
		a.applyLocal()
		a.wails.Event.Emit("trial-result", map[string]any{"error": ""})
	case ans.Limited:
		reply("Слишком много запросов. Попробуйте позже.")
	case ans.Temporary:
		reply("Пробные ключи временно закончились, попробуйте позже.")
	case ans.Ended():
		reply("Пробный период на этом устройстве уже был и закончился. Купить ключ можно в боте @jincomputers_bot.")
	default:
		log.Printf("Пробный период: отказ, код %d", ans.Code)
		reply("Не удалось получить пробный ключ. Попробуйте позже или напишите в бота @jincomputers_bot.")
	}
}

// refreshKeyInfo — тихо переспросить /v1/key про сохранённый ключ (срок,
// бессрочность) — при запуске. Служба молчит или отвечает не вердиктом —
// ничего не меняем; ключ не удаляем и строк под планетой не пишем: это
// справка для строки доступа, а не проверка человека.
func (a *app) refreshKeyInfo() {
	a.keyInfoAt.Store(time.Now().Unix())
	key, err := a.currentSubscriberKey()
	if err != nil || key == "" {
		return
	}
	platform := map[string]string{"darwin": "macos"}[runtime.GOOS]
	if platform == "" {
		platform = runtime.GOOS
	}
	client := core.NewParamsClient(core.ParamsConfig{Envelope: map[string]string{
		"device_id": a.deviceID, "platform": platform, "app_version": appVersion, "app_code": "0",
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ans, partner, err := core.CheckKeyWithPartner(ctx, client, key)
	if err != nil || ans == nil || ans.NoVerdict {
		return
	}
	if partner != nil {
		updateSettings(func(s *settings) {
			s.Partner = partner
			if ans.ExpiresAt > 0 {
				s.KeyExpiresAt = ans.ExpiresAt
			}
		})
		a.applyLocal()
		if ans.State != params.KeyActive || partner.BlockedReason != "" {
			return
		}
	} else if ans.State != params.KeyActive {
		return
	}
	updateSettings(func(s *settings) {
		s.KeyExpiresAt, s.KeyPermanent = ans.ExpiresAt, ans.ExpiresAt == 0
		s.Partner = partner
	})
	if t, ok := core.KeyTierOf(ans); ok {
		a.applyKeyTier(t)
	}
	a.applyLocal()
}

// refreshKeyInfoSoon — перечитать тариф/срок при открытии окна и после
// подключения, не чаще раза в 5 минут. Где keyInfoAtStart=false (macOS:
// чтение Keychain до первого подключения — системный диалог), окно
// перечитывает только когда ключ в этом запуске уже читали.
func (a *app) refreshKeyInfoSoon() {
	if !keyInfoAtStart && !a.keyInfoAsked.Load() {
		return
	}
	now := time.Now().Unix()
	last := a.keyInfoAt.Load()
	if now-last < 300 || !a.keyInfoAt.CompareAndSwap(last, now) {
		return
	}
	go a.refreshKeyInfo()
}

// applyKeyTier — запомнить тариф. На «Базовом» сохранённый белый режим
// (и умолчание новой установки — nil) один раз переписывается в чёрный:
// при переходе на премиум переключатель вернётся выключенным, белый
// человек включит сам. Идущую сессию не рвём — применится со следующей.
func (a *app) applyKeyTier(t core.KeyTier) {
	updateSettings(func(s *settings) {
		s.KeyTier = string(t)
		if t.Basic() && (s.WhiteMode == nil || *s.WhiteMode) {
			off := false
			s.WhiteMode = &off
		}
	})
	white := loadSettings().whiteMode()
	a.state.mu.Lock()
	a.state.whiteMode = white
	a.state.mu.Unlock()
	a.applyLocal()
}

func keyProblemText(ans *params.KeyAnswer) string {
	date := ""
	if ans.ExpiresAt > 0 {
		date = " " + time.Unix(ans.ExpiresAt, 0).Format("02.01.06")
	}
	switch ans.State {
	case params.KeyExpired:
		return "подписка кончилась" + date + " — нужно продлить"
	case params.KeyRevoked:
		return "доступ отозван" + date + " — напишите в поддержку"
	case params.KeyDeleted:
		return "ключ удалён" + date + " — напишите в поддержку"
	case params.KeyDeactivated:
		return "ключ отключён" + date + " — напишите в поддержку"
	case params.KeyUnknown:
		return "такого ключа нет — проверьте, что ввели"
	}
	return "ключ занят другим устройством"
}

// accessText — строка доступа в настройках (Android Access.keyText):
// дата / «Бессрочный ключ» (служба с вердиктом прислала 0) / «срок
// неизвестен» (служба молчала, ключ подтвердил туннель).
func accessText(st settings) string {
	switch {
	case st.KeyExpiresAt > 0 && st.trial():
		// Пробный: точное время конца, не только дата (просьба владельца 21.10).
		return "Пробный период до " + time.Unix(st.KeyExpiresAt, 0).Format("02.01.06 15:04")
	case st.KeyExpiresAt > 0:
		return "Доступ до " + time.Unix(st.KeyExpiresAt, 0).Format("02.01.06")
	case st.KeyPermanent:
		return "Бессрочный ключ"
	case st.KeyConfirmed:
		return "Ключ работает, срок неизвестен"
	}
	return "Ключ принят, проверяю подключением"
}

// accessTone — цвет кнопки со строкой доступа (просьба владельца 21.10):
// «ok» — подписка идёт и до конца больше недели (или ключ бессрочный),
// «warn» — кончается в ближайшие 7 дней, «bad» — кончилась, «» — срок
// неизвестен, красить нечем (серая кнопка).
func accessTone(st settings, now time.Time) string {
	switch {
	case st.KeyExpiresAt > 0:
		left := time.Unix(st.KeyExpiresAt, 0).Sub(now)
		// У пробного (3 дня) «неделя» — весь срок, жёлтым он был бы с первой
		// минуты: жёлтый для него — последние сутки.
		warn := 7 * 24 * time.Hour
		if st.trial() {
			warn = 24 * time.Hour
		}
		switch {
		case left <= 0:
			return "bad"
		case left <= warn:
			return "warn"
		}
		return "ok"
	case st.KeyPermanent:
		return "ok"
	}
	return ""
}

func (a *app) addVKLink(link string) {
	link = strings.TrimSpace(link)
	h := core.NormalizeHash(link)
	if h == "" {
		a.wails.Event.Emit("vk-result", map[string]any{"ok": false, "error": "это не ссылка на звонок ВКонтакте"})
		return
	}
	links := vkLinks()
	for _, l := range links {
		if core.NormalizeHash(l) == h {
			a.wails.Event.Emit("vk-result", map[string]any{"ok": false, "error": "эта ссылка уже есть"})
			return
		}
	}
	saveVKLinks(append(links, link))
	a.wails.Event.Emit("vk-result", map[string]any{"ok": true})
	a.applyLocal()
	// Только что добавленную — сразу проверить (просьба владельца 25.09).
	go a.checkVK([]string{h})
}

func (a *app) deleteVKLink(d any) {
	i, ok := d.(float64) // JSON-число из JS
	links := vkLinks()
	if !ok || int(i) < 0 || int(i) >= len(links) {
		return
	}
	saveVKLinks(append(links[:int(i)], links[int(i)+1:]...))
	a.applyLocal()
}

// addDionRoom — «Добавить и проверить» с непустым полем (договор кота 2):
// разбор/лимит через core.ParseDionRooms/DionMaxRooms, не свои правила.
func (a *app) addDionRoom(raw string) {
	slugs, bad := core.ParseDionRooms(raw)
	if len(bad) > 0 || len(slugs) != 1 {
		a.wails.Event.Emit("dion-result", map[string]any{"ok": false, "error": "Не подошло: " + raw})
		return
	}
	slug := slugs[0]
	rooms := dionRooms()
	for _, u := range rooms {
		if s2, b2 := core.ParseDionRooms(u); len(b2) == 0 && len(s2) == 1 && s2[0] == slug {
			a.wails.Event.Emit("dion-result", map[string]any{"ok": false, "error": "эта комната уже есть"})
			return
		}
	}
	if len(rooms) >= core.DionMaxRooms {
		a.wails.Event.Emit("dion-result", map[string]any{"ok": false,
			"error": fmt.Sprintf("Не подошло: %s (можно не больше %d комнат)", raw, core.DionMaxRooms)})
		return
	}
	saveDionRooms(append(rooms, core.DionRoomURL(slug)))
	// Добавленная комната — сразу основной обход (владелец 06.10, как Android
	// 0.1.243); вернуть VK можно переключателем VK|DION.
	updateSettings(func(s *settings) { s.RelayPrimaryDion = true })
	a.wails.Event.Emit("dion-result", map[string]any{"ok": true})
	a.applyLocal()
	// Только что добавленную — сразу проверить (тот же приём, что у VK).
	go a.checkDion([]string{slug})
}

func (a *app) deleteDionRoom(d any) {
	i, ok := d.(float64)
	rooms := dionRooms()
	if !ok || int(i) < 0 || int(i) >= len(rooms) {
		return
	}
	newRooms := append(rooms[:int(i)], rooms[int(i)+1:]...)
	saveDionRooms(newRooms)
	if len(newRooms) == 0 {
		// Комнат не осталось — DION основным быть не может (договор кота 2).
		updateSettings(func(s *settings) { s.RelayPrimaryDion = false })
	}
	a.applyLocal()
}

// setWhiteMode — применяется при СЛЕДУЮЩЕМ подключении: идущую сессию не рвём.
func (a *app) setWhiteMode(on bool) {
	if on && loadSettings().basic() {
		a.applyLocal() // переключатель скрыт; вернуть окну правду
		return
	}
	a.state.mu.Lock()
	a.state.whiteMode = on
	a.state.mu.Unlock()
	updateSettings(func(s *settings) { s.WhiteMode = &on })
	a.applyLocal()
}

// applyLocal — единственное место, которое трогает меню/окно: снимок
// состояния → подпись пункта меню, подсказка значка, событие "state".
// Сам значок — trayLoop (мигает во время подключения).
func (a *app) applyLocal() {
	a.state.mu.Lock()
	s := struct {
		kind                         uiKind
		address, notice              string
		noticeBad, hasKey, white, sw bool
		winner                       string
	}{a.state.kind, a.state.address, a.state.notice, a.state.noticeBad, a.state.hasKey, a.state.whiteMode, a.state.sessionWhite, a.state.winner}
	a.state.mu.Unlock()

	tooltip, label := "Meridian VPN — отключено", "Подключить"
	switch s.kind {
	case uiConnecting:
		tooltip, label = "Meridian VPN — подключаюсь", "Остановить подключение"
	case uiConnected:
		tooltip, label = "Meridian VPN — туннель активен", "Отключить"
	case uiError:
		tooltip = "Meridian VPN — " + s.notice
	}
	a.tray.SetTooltip(tooltip)
	a.menuToggle.SetLabel(label)

	ev := map[string]any{}
	a.upd.snapshot(ev)
	if v, _ := ev["update_version"].(string); v != "" && ev["update_stage"] == string(updAvailable) {
		a.menuUpdate.SetLabel("Обновить до " + v)
		a.menuUpdate.SetHidden(false)
	} else {
		a.menuUpdate.SetHidden(true)
	}

	st := loadSettings()
	black := readTunnelDomains()
	links := vkLinks()
	a.vkSnapshot(ev, links)
	rooms := dionRooms()
	a.dionSnapshot(ev, rooms)
	avail, auto := autostartState()
	relayPrimary := core.RelayPrimaryVK
	if st.RelayPrimaryDion {
		relayPrimary = core.RelayPrimaryDion
	}
	for k, v := range map[string]any{
		"state":               string(s.kind),
		"notice":              s.notice,
		"notice_bad":          s.noticeBad,
		"key_recovery":        a.recoveryAvailable.Load(),
		"key_temporary":       a.recoveryTemporary.Load(),
		"has_key":             s.hasKey,
		"white_mode":          s.white,
		"basic":               st.basic(),
		"captcha":             a.captcha.pending(),
		"white_pending":       (s.kind == uiConnected || s.kind == uiConnecting) && s.white != s.sw,
		"winner":              s.winner,
		"vk_links":            links,
		"relay_primary":       core.EffectiveRelayPrimary(relayPrimary, rooms),
		"relay_primary_want":  relayPrimary, // выбор человека, до применения правила «нет комнат — VK»
		"antidpi_youtube":     !st.AntiDPIYouTubeOff,
		"antidpi_discord":     !st.AntiDPIDiscordOff,
		"antidpi_cloudflare":  !st.AntiDPICloudflareOff,
		"antidpi_social":      !st.AntiDPISocialOff,
		"antidpi_other":       !st.AntiDPIOtherOff,
		"black_domains":       black,
		"tunnel_apps":         st.TunnelApps,
		"black_count":         len(core.ParseDirectDomains(black)),
		"autostart":           auto,
		"autostart_available": avail,
		"access_text":         accessText(st),
		"access_tone":         accessTone(st, time.Now()),
		"apps_supported":      appsSupported,
		"key_trial":           st.trial(),
		"partner_usage":       partnerUsageWithExpiry(st),
		"partner_managed":     st.Partner != nil,
		"version":             appVersion,
	} {
		ev[k] = v
	}
	a.wails.Event.Emit("state", ev)
}

// trayLoop — значок строки меню/трея: силуэт design/tray-mono.svg,
// полный — связь есть, тусклый — нет; во время подключения мигает
// (аналог быстрого спутника на планете). setTrayIcon — host_*.go.
func (a *app) trayLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	var last int = -1
	blink := false
	for range tick.C {
		a.state.mu.Lock()
		kind := a.state.kind
		a.state.mu.Unlock()
		full := kind == uiConnected
		if kind == uiConnecting {
			blink = !blink
			full = blink
		}
		cur := 0
		if full {
			cur = 1
		}
		if cur != last {
			setTrayIcon(a.tray, full)
			last = cur
		}
	}
}

// showWindow — у сохранённой точки, если она на существующем экране (монитор
// могли отключить); иначе у значка, как раньше.
func (a *app) showWindow() {
	st := loadSettings()
	if st.WinSaved && a.pointOnScreen(st.WinX, st.WinY) {
		a.window.SetPosition(st.WinX, st.WinY)
	} else {
		_ = a.tray.PositionWindow(a.window, 4)
	}
	x, y := a.window.Position()
	a.posMu.Lock()
	a.placedX, a.placedY = x, y
	a.posMu.Unlock()
	a.window.UnMinimise()
	a.window.Show().Focus()
	a.refreshKeyInfoSoon()
}

// Only the explicitly authorized recovery operation may remove the firewall.
func (a *app) offerNetworkRecovery() {
	if !a.networkRecoveryOffered.CompareAndSwap(false, true) {
		return
	}
	go func() {
		recovered, err := promptAndRecoverNetwork(runRecoveryCommand)
		if err != nil {
			a.setNotice(uiError, "Восстановление сети не завершено. Обратитесь в поддержку; журнал Meridian сохранён.", true)
			return
		}
		if recovered {
			a.setNotice(uiDisconnected, "Сеть восстановлена без VPN.", false)
		}
	}()
}

func (a *app) hideWindow() {
	a.window.Minimise()
}

// Keep a visible way to disconnect before explicitly quitting the GUI.
func (a *app) quitFromTray() {
	a.state.mu.Lock()
	kind, unavailable := a.state.kind, a.state.helperUnavailable
	a.state.mu.Unlock()
	if kind == uiConnected || kind == uiConnecting {
		a.showWindow()
		a.setNotice(kind, "Перед выходом отключите VPN нажатием на планету.", false)
		return
	}
	if unavailable || a.networkRecoveryOffered.Load() {
		if !a.quitWarningOpen.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer a.quitWarningOpen.Store(false)
			if confirmQuitUncertain() {
				a.rememberWindowPos()
				a.wails.Quit()
			}
		}()
		return
	}
	a.rememberWindowPos()
	a.wails.Quit()
}

// rememberWindowPos — окно видно и стоит не там, куда его поставили мы, —
// значит, его перетащили: запомнить.
func (a *app) rememberWindowPos() {
	if !a.window.IsVisible() {
		return
	}
	x, y := a.window.Position()
	a.posMu.Lock()
	moved := x != a.placedX || y != a.placedY
	a.posMu.Unlock()
	if moved {
		updateSettings(func(s *settings) { s.WinSaved, s.WinX, s.WinY = true, x, y })
	}
}

// pointOnScreen — верхний левый угол окна (с запасом 40 px — чтобы за окно
// можно было ухватиться) лежит на одном из подключённых экранов.
func (a *app) pointOnScreen(x, y int) bool {
	for _, sc := range a.wails.Screen.GetAll() {
		b := sc.Bounds
		if x+40 >= b.X && x+40 < b.X+b.Width && y+40 >= b.Y && y+40 < b.Y+b.Height {
			return true
		}
	}
	return false
}

// openKeyLink — открыта ссылка meridian://key/<ключ>: ключ ждёт подтверждения
// человеком (молча подменять ключ по чужой ссылке нельзя). Ключ в лог не идёт.
func (a *app) openKeyLink(raw string) {
	k, ok := core.ParseKeyInput(raw)
	if !ok || !strings.Contains(raw, "://") {
		log.Println("ссылка: не ссылка на ключ Meridian — пропускаю")
		return
	}
	a.keyLinkMu.Lock()
	a.keyLink = k
	a.keyLinkMu.Unlock()
	log.Println("ссылка с ключом получена, жду подтверждения")
	a.showWindow()
	a.emitKeyLink()
}

// emitKeyLink — отдать окну ключ из ссылки, если он ждёт (окно при загрузке
// спрашивает само: при запуске по ссылке событие приходит раньше окна).
func (a *app) emitKeyLink() {
	a.keyLinkMu.Lock()
	k := a.keyLink
	a.keyLinkMu.Unlock()
	if k != "" {
		a.wails.Event.Emit("key-link", map[string]any{"key": k, "masked": core.MaskKey(k)})
	}
}

func (a *app) takeKeyLink() {
	a.keyLinkMu.Lock()
	a.keyLink = ""
	a.keyLinkMu.Unlock()
}

func partnerUsageText(p *core.PartnerUsage) string {
	if p == nil {
		return ""
	}
	if p.BlockedReason == "traffic_limit" {
		return "Лимит трафика исчерпан — обратитесь к партнёру"
	}
	if p.BlockedReason == "expired" {
		return "Срок партнёрского доступа истёк — обратитесь к партнёру"
	}
	if p.BlockedReason != "" {
		return "Партнёрский доступ отключён — обратитесь к партнёру"
	}
	if p.Limit == 0 {
		return fmt.Sprintf("Использовано %.2f ГБ · без лимита", float64(p.Used)/1e9)
	}
	remaining := p.Limit - p.Used
	if remaining < 0 {
		remaining = 0
	}
	if p.Remaining != nil {
		remaining = *p.Remaining
	}
	return fmt.Sprintf("Использовано %.2f из %.2f ГБ · осталось %.2f ГБ", float64(p.Used)/1e9, float64(p.Limit)/1e9, float64(remaining)/1e9)
}

func partnerUsageWithExpiry(st settings) string {
	if st.Partner == nil {
		return ""
	}
	usage := partnerUsageText(st.Partner)
	if st.Partner.Limit > 0 && st.Partner.BlockedReason != "" {
		remaining := st.Partner.Limit - st.Partner.Used
		if remaining < 0 {
			remaining = 0
		}
		if st.Partner.Remaining != nil {
			remaining = *st.Partner.Remaining
		}
		usage += fmt.Sprintf(" · осталось %.2f ГБ", float64(remaining)/1e9)
	}
	return usage
}
