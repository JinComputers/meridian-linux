package params

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	"meridian/engine"
)

// Node — один узел из ответа службы. ID — устойчивое имя записи, ТОЛЬКО оно
// годится для памяти путей: место в списке служба может переставить.
type Node struct {
	ID        string
	Host      string
	Port      int
	DTLS      bool
	Transport string // "udp" (умолчание), "tcp" или "tls"
	SNI       string // для tls-ступени
}

// Relay — объект relay ответа. Target — только IPv4-литерал без порта
// (пусто — поля нет): ЗАПАСНОЙ конечный адрес релея, сначала идёт адрес из
// резолва edge (решение владельца 24.09).
type Relay struct {
	Enabled bool
	Target  string
	Port    int
	DTLS    bool
}

// Ladder — настройка лестницы от сервера. Каждое поле уже прошло проверку
// на вменяемость: вне пределов или пропущено — умолчание (нынешнее
// поведение), так что ошибка на сервере ничего не выключит.
type Ladder struct {
	SecondRoundMs           int
	DirectSkipAfterFails    int
	DirectProbeEveryAttempt int
}

// Answer — разобранный ответ /v1/params.
type Answer struct {
	RoutingScope           string
	Gateway                string
	GatewayPublicKey       string
	GatewayDTLSFingerprint string
	GatewayTLSFingerprints []string
	Nodes                  []Node
	Relay                  Relay
	Ladder                 Ladder
	TTLSeconds             int // 0 — не прислана, решает клиент
	Generation             int
	Raw                    []byte // сырой ответ: клиент хранит его нетронутым (правило 2)
}

// ErrNoNodes — в ответе нет ни одного узла. Это НЕ пустой список, а
// отсутствие ответа: сохранить пустой список значит оставить без связи всех
// одной опечаткой на сервере. Отказы службы (503/429) приходят без entries.
var ErrNoNodes = errors.New("в ответе нет ни одного узла")

const (
	secondRoundDefault = 3000
	skipAfterDefault   = 5
	probeEveryDefault  = 4
)

// ParseAnswer разбирает тело /v1/params. Лишние ключи (в том числе внутри
// relay) игнорируются: разбор не строгий, новое поле выпущенные версии не
// ломает.
func ParseAnswer(raw []byte) (*Answer, error) {
	var m struct {
		RoutingScope           string   `json:"routing_scope"`
		Gateway                string   `json:"gateway"`
		GatewayPublicKey       string   `json:"gateway_public_key"`
		GatewayDTLSFingerprint string   `json:"gateway_dtls_fingerprint"`
		GatewayTLSFingerprints []string `json:"gateway_tls_fingerprints"`
		Entries                []struct {
			ID        string `json:"id"`
			Host      string `json:"host"`
			Port      int    `json:"port"`
			DTLS      bool   `json:"dtls"`
			Transport string `json:"transport"`
			SNI       string `json:"sni"`
		} `json:"entries"`
		Relay      map[string]interface{} `json:"relay"`
		Ladder     map[string]interface{} `json:"ladder"`
		TTL        float64                `json:"ttl"`
		Generation float64                `json:"generation"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("ответ не JSON: %w", err)
	}
	if len(m.Entries) == 0 {
		return nil, ErrNoNodes
	}

	a := &Answer{
		RoutingScope:           m.RoutingScope,
		Gateway:                m.Gateway,
		GatewayPublicKey:       m.GatewayPublicKey,
		GatewayDTLSFingerprint: m.GatewayDTLSFingerprint,
		GatewayTLSFingerprints: m.GatewayTLSFingerprints,
		Raw:                    append([]byte(nil), raw...),
		TTLSeconds:             int(m.TTL),
		Generation:             int(m.Generation),
		// Умолчание — ДА: ответ без поля relay не должен отнимать
		// запасной путь. Отнять его служба может только сказав это прямо.
		Relay: Relay{Enabled: true},
	}
	for _, e := range m.Entries {
		if e.Host == "" || e.Port <= 0 {
			continue
		}
		n := Node{ID: e.ID, Host: e.Host, Port: e.Port, DTLS: e.DTLS, Transport: e.Transport, SNI: e.SNI}
		if n.ID == "" {
			n.ID = fmt.Sprintf("%s:%d", e.Host, e.Port)
		}
		if n.Transport == "" {
			// Запись без transport — старая udp-запись.
			n.Transport = "udp"
		}
		a.Nodes = append(a.Nodes, n)
	}
	if len(a.Nodes) == 0 {
		return nil, ErrNoNodes
	}

	if en, ok := m.Relay["enabled"].(bool); ok {
		a.Relay.Enabled = en
	}
	if t, ok := m.Relay["target"].(string); ok {
		a.Relay.Target = strings.TrimSpace(t)
	}
	if m.RoutingScope == "partner" {
		var wire struct {
			Entries []map[string]json.RawMessage `json:"entries"`
		}
		if json.Unmarshal(raw, &wire) != nil || len(wire.Entries) != len(m.Entries) {
			return nil, errors.New("некорректные входы партнёра")
		}
		ip := net.ParseIP(m.Gateway)
		if ip == nil || ip.To4() == nil || ip.String() != m.Gateway {
			return nil, errors.New("некорректный адрес партнёрского шлюза")
		}
		if _, ok := m.Relay["enabled"].(bool); !ok {
			return nil, errors.New("нет настройки релея партнёра")
		}
		port, ok := m.Relay["port"].(float64)
		if !ok || port < 1 || port > 65535 || port != float64(int(port)) {
			return nil, errors.New("некорректный порт релея партнёра")
		}
		dtls, ok := m.Relay["dtls"].(bool)
		if !ok {
			return nil, errors.New("нет настройки DTLS релея партнёра")
		}
		a.Relay.Port, a.Relay.DTLS = int(port), dtls
		if a.Relay.Target != "" {
			ip := net.ParseIP(a.Relay.Target)
			if ip == nil || ip.To4() == nil || ip.String() != a.Relay.Target {
				return nil, errors.New("некорректный адрес релея партнёра")
			}
		}
		needsDTLS, needsTLS := dtls, false
		if len(a.Nodes) != len(m.Entries) {
			return nil, errors.New("некорректные входы партнёра")
		}
		for i, n := range a.Nodes {
			var transport string
			if json.Unmarshal(wire.Entries[i]["transport"], &transport) != nil || transport != n.Transport {
				return nil, errors.New("не указан транспорт партнёра")
			}
			if transport == "udp" {
				var rawDTLS *bool
				if json.Unmarshal(wire.Entries[i]["dtls"], &rawDTLS) != nil || rawDTLS == nil {
					return nil, errors.New("не указана настройка DTLS входа партнёра")
				}
			}
			ip := net.ParseIP(n.Host)
			if ip == nil || ip.To4() == nil || ip.String() != n.Host || n.Port > 65535 {
				return nil, errors.New("некорректный вход партнёра")
			}
			switch n.Transport {
			case "udp":
				needsDTLS = needsDTLS || n.DTLS
			case "tcp":
			case "tls":
				needsTLS = true
				if strings.TrimSpace(n.SNI) == "" || strings.ContainsAny(n.SNI, " /:?#\t\r\n") {
					return nil, errors.New("некорректное TLS имя партнёра")
				}
			default:
				return nil, errors.New("неизвестный транспорт партнёра")
			}
		}
		if needsDTLS && m.GatewayDTLSFingerprint == "" {
			return nil, errors.New("нет DTLS отпечатка партнёра")
		}
		if needsTLS && len(m.GatewayTLSFingerprints) == 0 {
			return nil, errors.New("нет TLS отпечатков партнёра")
		}
		pins, _ := json.Marshal(m.GatewayTLSFingerprints)
		if m.GatewayTLSFingerprints == nil {
			pins = []byte("[]")
		}
		if err := engine.NewLadder().SetGatewayIdentity(m.GatewayPublicKey, m.GatewayDTLSFingerprint, string(pins)); err != nil {
			return nil, errors.New("некорректная идентификация партнёрского шлюза")
		}
	}

	a.Ladder = Ladder{
		SecondRoundMs:           ladderInt(m.Ladder, "second_round_ms", secondRoundDefault, 500, 15000),
		DirectSkipAfterFails:    ladderInt(m.Ladder, "direct_skip_after_fails", skipAfterDefault, 1, 1000),
		DirectProbeEveryAttempt: ladderInt(m.Ladder, "direct_probe_every_attempts", probeEveryDefault, 1, 1000),
	}
	return a, nil
}

func ladderInt(l map[string]interface{}, name string, def, min, max int) int {
	f, ok := l[name].(float64)
	if !ok {
		return def
	}
	if v := int(f); v >= min && v <= max {
		return v
	}
	return def
}

// Fetch — /v1/params. key — ключ подписчика (пусто — анонимный запрос;
// узлы reserve по анонимному не отдаются). Объявляем, что умеем: без
// transports служба отдаёт только udp-записи.
//
// Ошибки: Refused/Incompatible/Unavailable приходят как *Error, пустой
// ответ — ErrNoNodes.
func (c *Client) Fetch(ctx context.Context, key string) (*Answer, error) {
	q := map[string]string{"transports": "udp,tcp,tls", "routing_contract": "partner-v1"}
	if key != "" {
		q["key"] = key
	}
	switch r := c.Call(ctx, "/v1/params", q).(type) {
	case Ok:
		a, err := ParseAnswer(r.Raw)
		if err != nil {
			return nil, err
		}
		if a.RoutingScope != "ordinary" && a.RoutingScope != "partner" {
			return nil, &Error{Kind: KindUnavailable, Msg: "служба не подтвердила тип маршрута"}
		}
		return a, nil
	case Refused:
		return nil, &Error{Kind: KindRefused, Msg: r.Err, RetryAfter: r.RetryAfter, Code: r.Code, RoutingScope: r.RoutingScope}
	case Incompatible:
		return nil, &Error{Kind: KindIncompatible, Msg: "договор «" + r.Theirs + "»"}
	case Unavailable:
		return nil, &Error{Kind: KindUnavailable, Msg: r.Why}
	}
	return nil, errors.New("неизвестный исход обращения")
}

// ErrorKind — род ошибки обращения.
type ErrorKind int

const (
	KindRefused ErrorKind = iota + 1
	KindIncompatible
	KindUnavailable
)

// Error — ошибка обращения к службе.
type Error struct {
	Kind         ErrorKind
	Msg          string
	RetryAfter   int
	Code         int
	RoutingScope string
}

func (e *Error) Error() string { return e.Msg }
