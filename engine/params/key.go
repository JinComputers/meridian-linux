package params

import (
	"context"
	"strings"
)

// KeyState — вердикт службы о ключе. Слова — из Api.kt (VERDICTS).
type KeyState int

const (
	KeyActive       KeyState = iota // ok: жив и не истёк, пускаем
	KeyExpired                      // срок закончился — продлевать
	KeyRevoked                      // доступ отозван, вышел из платной группы
	KeyDeleted                      // удалён администратором
	KeyDeactivated                  // отключён администратором, не удалён
	KeyRotated                      // заменён при переносе на другое устройство
	KeyUnknown                      // служба ключа не знает
	KeyUnrecognised                 // вердикт непонятен клиенту: ключ НЕ хоронить
)

var verdicts = map[string]KeyState{
	"ok":          KeyActive,
	"expired":     KeyExpired,
	"revoked":     KeyRevoked,
	"deleted":     KeyDeleted,
	"deactivated": KeyDeactivated,
	"rotated":     KeyRotated,
	"unknown":     KeyUnknown,
}

// Dead — ключ пускать не должен: истёк, отозван, удалён, отключён или
// заменён. Непонятный вердикт (KeyUnrecognised) и «не знаю» сюда НЕ входят:
// непонятое слово не повод хоронить ключ, проверит подключение.
func (s KeyState) Dead() bool {
	switch s {
	case KeyExpired, KeyRevoked, KeyDeleted, KeyDeactivated, KeyRotated:
		return true
	}
	return false
}

// KeyAnswer — итог проверки ключа.
type KeyAnswer struct {
	State KeyState
	// NoVerdict — служба ответила, но вердикта нет (status: unavailable
	// или иное): «не смогли проверить» не должно выглядеть приговором
	// ключу. Единственное место договора, где отсутствие поля осмысленно.
	NoVerdict bool
	Why       string
	Bound     string // "this", "other" или пусто
	ExpiresAt int64  // unix-секунды, 0 — не прислано
	// Tier — тариф ключа: "basic" или пусто (премиум). У премиума поля нет;
	// всё незнакомое — тоже пусто, то есть премиум: незнакомое значение не
	// должно отнимать у человека белый режим и Dion (кот 5, 16.10).
	Tier    string
	Partner *PartnerInfo
}

// PartnerInfo is trusted metadata returned by /v1/key. Absence means the
// ordinary key contract; it must never be inferred from the key string.
type PartnerInfo struct {
	Managed               bool
	TrafficKnown          bool
	TrafficLimitBytes     int64
	TrafficUsedBytes      int64
	TrafficRemainingBytes *int64
	BlockedReason         string
}

// TierBasic — тариф «Базовый»: без белого режима и Dion.
const TierBasic = "basic"

// CheckKey — /v1/key. Ошибки обращения — как у Fetch (*Error).
func (c *Client) CheckKey(ctx context.Context, key string) (*KeyAnswer, error) {
	switch r := c.Call(ctx, "/v1/key", map[string]string{"key": key}).(type) {
	case Ok:
		return parseKey(r.Body), nil
	case Refused:
		return nil, &Error{Kind: KindRefused, Msg: r.Err, RetryAfter: r.RetryAfter, Code: r.Code}
	case Incompatible:
		return nil, &Error{Kind: KindIncompatible, Msg: "договор «" + r.Theirs + "»"}
	case Unavailable:
		return nil, &Error{Kind: KindUnavailable, Msg: r.Why}
	}
	return nil, &Error{Kind: KindUnavailable, Msg: "неизвестный исход обращения"}
}

func parseKey(m map[string]interface{}) *KeyAnswer {
	v, has := m["verdict"].(string)
	if !has {
		why, _ := m["message"].(string)
		if why == "" {
			why = "вердикта в ответе нет"
		}
		return &KeyAnswer{NoVerdict: true, Why: why}
	}
	a := &KeyAnswer{}
	if st, ok := verdicts[strings.ToLower(v)]; ok {
		a.State = st
	} else {
		a.State = KeyUnrecognised
		a.Why = "вердикт «" + v + "» неизвестен"
	}
	a.Bound, _ = m["bound"].(string)
	if tier, _ := m["tier"].(string); strings.ToLower(tier) == TierBasic {
		a.Tier = TierBasic
	}
	if e, ok := m["expires_at"].(float64); ok {
		a.ExpiresAt = int64(e)
	}
	if p, ok := m["partner"].(map[string]interface{}); ok {
		managed, _ := p["managed"].(bool)
		if managed {
			info := &PartnerInfo{Managed: true}
			if n, ok := p["traffic_limit_bytes"].(float64); ok && n >= 0 && n < 9223372036854775808 {
				info.TrafficLimitBytes = int64(n)
				info.TrafficKnown = true
			}
			if n, ok := p["traffic_used_bytes"].(float64); ok && n >= 0 {
				info.TrafficUsedBytes = int64(n)
			}
			if n, ok := p["traffic_remaining_bytes"].(float64); ok && n >= 0 {
				v := int64(n)
				info.TrafficRemainingBytes = &v
			}
			info.BlockedReason, _ = p["blocked_reason"].(string)
			a.Partner = info
		}
	}
	return a
}
