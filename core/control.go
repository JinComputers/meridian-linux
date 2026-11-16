package core

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// control.go — протокол «интерфейс ↔ служба/помощник» (решение
// владельца 24.09, этап 2): ОДИН для macOS и Windows, здесь только
// сообщения/версия/ошибки/кадрирование — ни net.Listen, ни net.Dial
// в этом файле нет и не будет. Различается только транспорт: на
// macOS — Unix-сокет (platform/darwin), на Windows — именованный
// канал (platform/windows, кот 6); обе стороны реализуют io.Reader/
// io.Writer (net.Conn их и даёт), этого достаточно для всего ниже.

// ControlProtocolVersion — версия протокола. Поднимается НЕСОВМЕСТИМО
// вместе с именем транспорта (control.v1.sock на macOS, \\.\pipe\
// Meridian.v1 на Windows — та же цифра в обоих именах не случайно)
// при любом ломающем изменении формата сообщений ниже; совместимое
// добавление (новое необязательное поле в payload) версию не поднимает.
// PerformControlHelloAsServer сверяет её первым делом на каждом новом
// соединении — versioned-имя транспорта защищает от подключения
// СТАРОГО клиента к НОВОМУ транспорту с другим именем, но не наоборот
// и не от ошибки при самой смене имени; проверка в протоколе не
// полагается только на путь/имя канала.
const ControlProtocolVersion = 1

// ControlMessageType — какого рода сообщение несёт ControlEnvelope.
type ControlMessageType string

const (
	// Интерфейс → служба.
	ControlHello       ControlMessageType = "hello"
	ControlConnect     ControlMessageType = "connect"
	ControlDisconnect  ControlMessageType = "disconnect"
	ControlStatusQuery ControlMessageType = "status"
	// ControlLogsQuery — интерфейс → служба: прислать хвост своего лога
	// (для «Отправить в поддержку»). Ответ — ControlLogs. Лог службы
	// лежит под root/SYSTEM — интерфейс сам его не читает.
	ControlLogsQuery ControlMessageType = "logs_query"
	// ControlCaptchaResult — интерфейс → служба: итог ручной капчи VK
	// (ControlCaptchaPayload: ID запроса, Token или Reason). Совместимое
	// добавление: старая служба ответит unknown_message.
	ControlCaptchaResult ControlMessageType = "captcha_result"
	// ControlUpdateInstall — интерфейс → служба (только Windows): тихо
	// обновить программу без окна UAC (ControlUpdateInstallPayload).
	// Служба сама берёт описание релиза и файл с фиксированного адреса
	// GitHub — от интерфейса не принимает ни пути, ни хеша. Ответ —
	// ControlUpdateStatus. Старая служба ответит unknown_message.
	ControlUpdateInstall ControlMessageType = "update_install"

	// Служба → интерфейс.
	ControlHelloAck     ControlMessageType = "hello_ack"
	ControlEvent        ControlMessageType = "event"        // несёт EngineEvent как есть, без отдельного типа payload
	ControlState        ControlMessageType = "state"        // ControlStatePayload — ВСЕГДА окончательное (соединено или окончательно нет), см. RunControlConnect
	ControlReconnecting ControlMessageType = "reconnecting" // ControlReconnectingPayload — переходное, за ним обязательно последует либо новый ControlState{Connected:true}, либо ControlState{Connected:false} (сдались), либо ControlError
	ControlError        ControlMessageType = "error"        // ControlErrorPayload, в любую сторону
	// ControlCaptchaRequest — служба → интерфейс: VK требует капчу, нужен
	// человек (ControlCaptchaPayload: ID и URL; Done — запрос снят).
	ControlCaptchaRequest ControlMessageType = "captcha_request"
	ControlLogs           ControlMessageType = "logs" // ControlLogsPayload — ответ на ControlLogsQuery, только запросившему
	// ControlUpdateStatus — служба → интерфейс: как идёт тихое обновление
	// (ControlUpdateStatusPayload), только запросившему.
	ControlUpdateStatus ControlMessageType = "update_status"
)

// ControlEnvelope — единственная форма кадра на проводе в обе
// стороны: [4 байта длины, big-endian][JSON Envelope]. Тот же принцип,
// что у движка (engine/flowconn.go, 2-байтный кадр внутри туннеля) —
// другой слой, другой запас (управляющие сообщения не бывают крупнее
// одного пакета, но 4 байта, не 2, — не тот случай, где стоит экономить).
type ControlEnvelope struct {
	Type    ControlMessageType `json:"type"`
	Payload json.RawMessage    `json:"payload,omitempty"`
}

// ControlHelloPayload — первое сообщение от КАЖДОЙ стороны сразу
// после установки соединения, до любого другого сообщения (клиент —
// сторона, открывшая соединение, шлёт первой; сервер — сторона,
// принявшая, читает первой; см. PerformControlHelloAs{Client,Server}).
type ControlHelloPayload struct {
	ProtocolVersion int `json:"protocol_version"`
	// AppVersion — версия интерфейса или помощника, для лога/
	// диагностики (Console.app / Event Log), решения по ней протокол
	// не принимает — только ProtocolVersion.
	AppVersion string `json:"app_version,omitempty"`
}

// ControlConnectPayload — интерфейс → служба, запрос подключения.
//
// KeyMaterial — СОДЕРЖИМОЕ ключа подписчика, не путь к файлу: ключ
// теперь хранится в Keychain интерфейса (решение владельца 24.09,
// вариант А плана docs/plan-etap2-darwin-helper.md, раздел 3) —
// интерфейс сам его достаёт и шлёт помощнику только на время сессии,
// раз при каждом Connect. Ни при каких обстоятельствах не
// логируется целиком (то же правило, что раньше для ConnectOptions.
// KeyPath — источник сменился с файла на Keychain+канал, требование
// нет).
type ControlConnectPayload struct {
	KeyMaterial     string        `json:"key_material"`
	Gateway         string        `json:"gateway"`
	DeviceID        string        `json:"device_id"`
	CachedFallback  string        `json:"cached_fallback,omitempty"`
	PartnerRouting  bool          `json:"partner_routing,omitempty"`
	DisableAutoDion bool          `json:"disable_auto_dion,omitempty"`
	Ladder          LadderOptions `json:"ladder"`
	// DirectDomains — прямые домены белого режима (этап 3), уже
	// разобранные core.ParseDirectDomains. Совместимое добавление —
	// ControlProtocolVersion не растёт (старый помощник поле просто не
	// прочитает; новый помощник со старым интерфейсом получит пусто).
	DirectDomains []string `json:"direct_domains,omitempty"`
	// TunnelOnly/TunnelDomains — чёрный режим (core.TunnelRouter): в
	// туннель идёт ТОЛЬКО список (домены с поддоменами, адреса, сети
	// a.b.c.d/n), остальное напрямую; пустой список — в туннель ничего
	// (решение владельца 25.09). Совместимое добавление, как DirectDomains.
	TunnelOnly    bool     `json:"tunnel_only,omitempty"`
	TunnelDomains []string `json:"tunnel_domains,omitempty"`
	// RUDirect — «RU-адреса напрямую» (core.ConnectOptions.RUDirect).
	RUDirect bool `json:"ru_direct,omitempty"`
	// AntiDPI — обход DPI-блокировок, только вместе с TunnelOnly
	// (ConnectOptions.AntiDPI, AntiDPIRouter).
	AntiDPI AntiDPIGroups `json:"antidpi,omitempty"`
	// TunnelApps — приложения в туннель (ConnectOptions.TunnelApps,
	// AppSplitRouter), только вместе с TunnelOnly.
	TunnelApps []string `json:"tunnel_apps,omitempty"`
	// DionRooms/RelayPrimary — комнаты DION (slug, core.ParseDionRooms, до
	// DionMaxRooms) и основное звено ретранслятора ("vk"|"dion", как ключи
	// DION/RELAY_PRIMARY у роутеров кота 2). Совместимое добавление. В
	// лестницу движка попадут с тегом кота 3; до того помощник их не
	// применяет.
	DionRooms    []string `json:"dion_rooms,omitempty"`
	RelayPrimary string   `json:"relay_primary,omitempty"`
}

// ConnectOptions — перенос полей запроса в опции подключения, ОДИН на
// обе платформы. Кот 6 поймал у себя (этап 3): служба собирала
// ConnectOptions руками и не перенесла DirectDomains — белый режим молча
// туннелировал всё. TestControlConnectPayloadMapsEveryField падает, если
// в ControlConnectPayload появится поле, которого здесь нет.
// Платформенное (TunnelDNS, TunName, Guard, Captcha) дописывает
// вызывающий — в протоколе этого нет.
func (p ControlConnectPayload) ConnectOptions() ConnectOptions {
	return ConnectOptions{
		KeyMaterial:     p.KeyMaterial,
		Gateway:         p.Gateway,
		DeviceID:        p.DeviceID,
		CachedFallback:  p.CachedFallback,
		PartnerRouting:  p.PartnerRouting,
		DisableAutoDion: p.DisableAutoDion,
		Ladder:          p.Ladder,
		DirectDomains:   p.DirectDomains,
		TunnelOnly:      p.TunnelOnly,
		TunnelDomains:   p.TunnelDomains,
		RUDirect:        p.RUDirect,
		AntiDPI:         p.AntiDPI,
		TunnelApps:      p.TunnelApps,
		DionRooms:       p.DionRooms,
		RelayPrimary:    p.RelayPrimary,
	}
}

// ControlStatePayload — служба → интерфейс, ОКОНЧАТЕЛЬНОЕ состояние
// сессии целиком (не единичное событие движка — то отдельным
// ControlEvent, и не переходный момент разрыва — тот отдельным
// ControlReconnecting): либо только что подключились (Connected true,
// TunnelInfo заполнен), либо сессия завершена насовсем — штатно или
// после исчерпанных попыток (Connected false, TunnelInfo пуст). Кроме
// как после каждого такого изменения — сразу в ответ на
// ControlStatusQuery (не ждать следующего события, чтобы узнать
// текущее состояние).
type ControlStatePayload struct {
	Connected  bool        `json:"connected"`
	TunnelInfo *TunnelInfo `json:"tunnel_info,omitempty"`
	Reconnects int         `json:"reconnects"`
	// Winner — буква победившей ступени лестницы (Handle.Winner(); "R" —
	// релей), только при Connected. Интерфейс красит планету белой по
	// ФАКТУ пути, а не по настройке белого режима (решение владельца
	// 26.09, как whiteMode = winner == Paths.RELAY на Android). Пусто —
	// старая служба или факт неизвестен.
	Winner string `json:"winner,omitempty"`
}

// ControlCaptchaPayload — обе стороны ручной капчи VK. Токены и session_token
// в логи не пишутся.
type ControlCaptchaPayload struct {
	ID string `json:"id"`
	// URL — адрес страницы капчи (служба → интерфейс).
	URL string `json:"url,omitempty"`
	// Done — служба → интерфейс: запрос закрыт (решён, отменён, истёк), кнопку убрать.
	Done bool `json:"done,omitempty"`
	// Token — интерфейс → служба: success_token решённой капчи.
	Token string `json:"token,omitempty"`
	// Reason — интерфейс → служба, когда токена нет: closed или timeout.
	Reason string `json:"reason,omitempty"`
}

// ControlLogsPayload — служба → интерфейс, ответ на ControlLogsQuery:
// последние строки лога службы, УЖЕ обезличенные на её стороне
// (core.ScrubLog: device_id, ссылки VK, ключ, адреса). Интерфейс
// дочищает ещё раз своими опциями — двойной проход безвреден. Старая
// служба запроса не знает и ответит ControlError unknown_message.
type ControlLogsPayload struct {
	Lines []string `json:"lines"`
}

// ControlUpdateInstallPayload — интерфейс → служба, просьба тихо обновить
// программу. Что ставить, решает служба сама (последний релиз канала с
// GitHub); здесь только то, чего она знать не может.
type ControlUpdateInstallPayload struct {
	// Channel — "release" (открытый репозиторий раздачи) или "debug"
	// (бета владельца, закрытый репозиторий, нужен Token).
	Channel string `json:"channel"`
	// Token — токен GitHub на чтение закрытого репозитория (только "debug");
	// служба держит его в памяти на время запроса, в лог не пишет.
	Token string `json:"token,omitempty"`
	// CurrentVersion — версия запущенного интерфейса a.b.c; ставится только
	// более новая.
	CurrentVersion string `json:"current_version"`
	// UIPID — процесс интерфейса: установщик ждёт его выхода.
	UIPID int `json:"ui_pid"`
	// Reconnect — был подключён: новый интерфейс подключится сам.
	Reconnect bool `json:"reconnect,omitempty"`
}

// Стадии ControlUpdateStatusPayload.Stage.
const (
	UpdateStagePreparing = "preparing" // скачивает и сверяет
	UpdateStageReady     = "ready"     // проверено; интерфейс должен выйти, установка начнётся сама
	UpdateStageNone      = "none"      // новее нет
	UpdateStageFailed    = "failed"    // не вышло (Problem — человеку)
)

// ControlUpdateStatusPayload — служба → интерфейс, ответ на ControlUpdateInstall.
type ControlUpdateStatusPayload struct {
	Stage   string `json:"stage"`
	Version string `json:"version,omitempty"`
	Problem string `json:"problem,omitempty"`
	// ResultPath — при UpdateStageReady: файл итога установки (result.json) в
	// закрытой папке службы; интерфейс-помощник ждёт его после выхода.
	ResultPath string `json:"result_path,omitempty"`
}

// ControlReconnectingPayload — служба → интерфейс, сессия оборвалась
// НЕПРЕДНАМЕРЕННО (core.KeepConnected различает это от штатной
// остановки) и служба уже пробует восстановить её сама. Attempt — тот
// же счётчик, что в следующем ControlStatePayload.Reconnects, если
// восстановление удастся. Это ПЕРЕХОДНОЕ сообщение — интерфейс не
// должен путать его с окончательным ControlState{Connected:false}
// (оба возможны сразу после разрыва, различаются типом сообщения, не
// значением Connected).
type ControlReconnectingPayload struct {
	Attempt int `json:"attempt"`
}

// ControlErrorPayload — служба → интерфейс (штатный отказ, например
// ControlErrConnectFailed) или интерфейс/служба → противоположная
// сторона при разрыве по версии протокола (ControlErrProtocolVersion,
// см. ControlHelloPayload). Code — для программной обработки
// интерфейсом (какой экран/сообщение показать), Message — человеку в
// лог, не для сравнения кодом.
type ControlErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Failure — код причины от движка (engine.FailureKind(): "blocked-
	// nolinks", "blocked-relay", "silent", ...; docs/desktop-design.md кота
	// 3, раздел 4) — по нему интерфейс выбирает текст для человека.
	// Заполняет RunControlConnect, когда подключение не удалось; пусто —
	// движок причину не назвал (или тег движка ещё без FailureKind).
	// Совместимое добавление — версия протокола та же. Кроме кодов
	// движка — FailureAPIUnavailable (свой, ставит core).
	Failure string `json:"failure,omitempty"`
}

// FailureAPIUnavailable — /v1/params не ответил ни по одному адресу
// (таймауты TCP/DNS/TLS): до движка дело не дошло, у него кода нет.
// Типичная картина сети, где режут зарубежные адреса.
const FailureAPIUnavailable = "api-unavailable"

// Известные значения ControlErrorPayload.Code — не исчерпывающий
// список (помощник может прислать другой Code, интерфейс не обязан
// падать на незнакомом — показать Message как есть), но эти интерфейс
// должен уметь различать программно.
const (
	ControlErrProtocolVersion = "protocol_version_mismatch"
	ControlErrConnectFailed   = "connect_failed"
	ControlErrAlreadyRunning  = "already_running"
	ControlErrNotConnected    = "not_connected"
)

// controlMaxFrame — предел размера ОДНОГО кадра (после JSON-кодирования
// конверта целиком). С большим запасом над реальными сообщениями
// (десятки-сотни байт) — защита от повреждённого/чужого клиента на
// другом конце сокета/канала, не даёт разово раздуть память на один
// кадр до попытки его разобрать.
const controlMaxFrame = 1 << 20 // 1 МиБ

// WriteControlMessage кодирует (typ, payload) в JSON и пишет одним
// кадром [4 байта длины, big-endian][JSON] в w. payload — nil, если у
// типа сообщения нет полезной нагрузки (ControlDisconnect,
// ControlStatusQuery). Кадр собирается в памяти целиком перед одним
// вызовом w.Write — на любом транспорте здесь (Unix-сокет, именованный
// канал) частичная запись возможна, раздельная запись длины и тела
// двумя Write рискует граничным случаем гонки на другом конце,
// которого так спокойнее не иметь.
func WriteControlMessage(w io.Writer, typ ControlMessageType, payload any) error {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("WriteControlMessage(%s): %w", typ, err)
		}
		raw = b
	}
	body, err := json.Marshal(ControlEnvelope{Type: typ, Payload: raw})
	if err != nil {
		return fmt.Errorf("WriteControlMessage(%s): %w", typ, err)
	}
	if len(body) > controlMaxFrame {
		return fmt.Errorf("WriteControlMessage(%s): кадр %d байт больше предела %d", typ, len(body), controlMaxFrame)
	}
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("WriteControlMessage(%s): %w", typ, err)
	}
	return nil
}

// ReadControlMessage читает ровно один кадр из r и разбирает конверт.
// Envelope.Payload остаётся сырым JSON — вызывающий код сам решает,
// во что его распаковывать по Envelope.Type (см. DecodeControlPayload).
// Ошибка при чтении длины — как есть, включая io.EOF: вызывающий код
// отличает штатное закрытие соединения (io.EOF на границе кадра) от
// обрыва посреди кадра (io.ErrUnexpectedEOF из io.ReadFull на теле).
func ReadControlMessage(r io.Reader) (ControlEnvelope, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return ControlEnvelope{}, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n > controlMaxFrame {
		return ControlEnvelope{}, fmt.Errorf("ReadControlMessage: кадр %d байт больше предела %d", n, controlMaxFrame)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return ControlEnvelope{}, fmt.Errorf("ReadControlMessage: тело кадра: %w", err)
	}
	var env ControlEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return ControlEnvelope{}, fmt.Errorf("ReadControlMessage: %w", err)
	}
	return env, nil
}

// DecodeControlPayload разбирает env.Payload в конкретный тип
// (ControlConnectPayload, ControlStatePayload, ...), с понятной
// ошибкой, если Payload пуст там, где вызывающий его ожидал.
func DecodeControlPayload[T any](env ControlEnvelope) (T, error) {
	var v T
	if len(env.Payload) == 0 {
		return v, fmt.Errorf("DecodeControlPayload(%s): пустой payload", env.Type)
	}
	if err := json.Unmarshal(env.Payload, &v); err != nil {
		return v, fmt.Errorf("DecodeControlPayload(%s): %w", env.Type, err)
	}
	return v, nil
}

// PerformControlHelloAsClient — сторона, что ОТКРЫЛА соединение
// (интерфейс), шлёт Hello первой и ждёт hello_ack. appVersion — что
// положить в ControlHelloPayload.AppVersion (для лога на стороне
// помощника), протокол по нему решений не принимает.
func PerformControlHelloAsClient(rw io.ReadWriter, appVersion string) error {
	if err := WriteControlMessage(rw, ControlHello, ControlHelloPayload{
		ProtocolVersion: ControlProtocolVersion,
		AppVersion:      appVersion,
	}); err != nil {
		return fmt.Errorf("PerformControlHelloAsClient: %w", err)
	}
	env, err := ReadControlMessage(rw)
	if err != nil {
		return fmt.Errorf("PerformControlHelloAsClient: %w", err)
	}
	switch env.Type {
	case ControlHelloAck:
		return nil
	case ControlError:
		errPayload, decErr := DecodeControlPayload[ControlErrorPayload](env)
		if decErr != nil {
			return fmt.Errorf("PerformControlHelloAsClient: помощник отказал (payload не разобрать: %v)", decErr)
		}
		return fmt.Errorf("PerformControlHelloAsClient: помощник отказал: %s (%s)", errPayload.Message, errPayload.Code)
	default:
		return fmt.Errorf("PerformControlHelloAsClient: неожиданный первый ответ %q вместо hello_ack", env.Type)
	}
}

// PerformControlHelloAsServer — сторона, что ПРИНЯЛА соединение
// (помощник), читает Hello первой, сверяет версию и отвечает
// hello_ack, либо error и возвращает ошибку — закрытие самого
// соединения после error остаётся за вызывающим кодом (эта функция
// только решает, годится соединение или нет, транспорт не трогает).
func PerformControlHelloAsServer(rw io.ReadWriter) error {
	env, err := ReadControlMessage(rw)
	if err != nil {
		return fmt.Errorf("PerformControlHelloAsServer: %w", err)
	}
	if env.Type != ControlHello {
		_ = WriteControlMessage(rw, ControlError, ControlErrorPayload{
			Code:    ControlErrProtocolVersion,
			Message: fmt.Sprintf("ожидали hello первым сообщением, получили %q", env.Type),
		})
		return fmt.Errorf("PerformControlHelloAsServer: первое сообщение %q, не hello", env.Type)
	}
	hello, err := DecodeControlPayload[ControlHelloPayload](env)
	if err != nil {
		return fmt.Errorf("PerformControlHelloAsServer: %w", err)
	}
	if hello.ProtocolVersion != ControlProtocolVersion {
		_ = WriteControlMessage(rw, ControlError, ControlErrorPayload{
			Code:    ControlErrProtocolVersion,
			Message: fmt.Sprintf("версия протокола интерфейса %d не совпадает с версией помощника %d", hello.ProtocolVersion, ControlProtocolVersion),
		})
		return fmt.Errorf("PerformControlHelloAsServer: версия протокола интерфейса %d, у помощника %d", hello.ProtocolVersion, ControlProtocolVersion)
	}
	if err := WriteControlMessage(rw, ControlHelloAck, ControlHelloPayload{ProtocolVersion: ControlProtocolVersion}); err != nil {
		return fmt.Errorf("PerformControlHelloAsServer: %w", err)
	}
	return nil
}
