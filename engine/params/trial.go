package params

import (
	"context"
	"net/http"
	"strings"
)

// TrialAnswer — итог POST /v1/trial (контракт — docs/api-trial-unbind.md, кот 2).
//
// Это ОТВЕТ СЛУЖБЫ, а не сбой: «пробные ключи кончились» и «слишком часто»
// приходят сюда с Limited/Temporary, ошибкой (*Error) бывает только молчание
// всех адресов и чужой договор. Ключ в лог не пишется нигде.
type TrialAnswer struct {
	// Status — "answered" или "unavailable" (служба ответила без вердикта).
	Status  string
	Verdict string // ok, expired, deactivated, unknown… — как прислала служба
	// Key — выданный пробный ключ; пусто, если не выдан.
	Key       string
	ExpiresAt int64 // unix-секунды, 0 — не прислано
	// Repeat — ключ выдан РАНЕЕ этому устройству, а не новый.
	Repeat  bool
	Message string // текст службы для человека

	Code       int  // код HTTP
	RetryAfter int  // секунды, 0 — не прислано
	Limited    bool // 429: предел выдачи, повторить позже
	// Temporary — 503/«unavailable»: пробные ключи временно недоступны. Это НЕ
	// отказ устройству: пробный период не потрачен, спросить позже.
	Temporary bool
}

// Granted — ключ выдан.
func (a *TrialAnswer) Granted() bool { return a != nil && a.Key != "" }

// Ended — пробный период был и закончился: служба ответила по существу без
// ключа (verdict expired или deactivated). Не «временно» и не «слишком часто»:
// повторять бесполезно, человеку нужен платный ключ. Message — текст службы
// для человека, ключа в нём нет.
func (a *TrialAnswer) Ended() bool {
	if a == nil || a.Key != "" || a.Limited || a.Temporary {
		return false
	}
	switch strings.ToLower(a.Verdict) {
	case "expired", "deactivated":
		return true
	}
	return false
}

// StartTrial — POST /v1/trial?device_id=<id> на те же адреса и пины, что и
// остальные ручки; без тела и без токена.
func (c *Client) StartTrial(ctx context.Context, deviceID string) (*TrialAnswer, error) {
	switch r := c.CallMethod(ctx, http.MethodPost, "/v1/trial",
		map[string]string{"device_id": deviceID}).(type) {
	case Ok:
		return trialFromBody(r.Body, r.Code), nil
	case Refused:
		// status != answered или код отказа: 429 — предел, остальное
		// (503, «unavailable») — временно. Ключа нет в любом случае.
		a := &TrialAnswer{
			Status:     "unavailable",
			Message:    r.Err,
			Code:       r.Code,
			RetryAfter: r.RetryAfter,
			Limited:    r.Code == http.StatusTooManyRequests,
		}
		a.Temporary = !a.Limited
		return a, nil
	case Incompatible:
		return nil, &Error{Kind: KindIncompatible, Msg: "договор «" + r.Theirs + "»"}
	case Unavailable:
		return nil, &Error{Kind: KindUnavailable, Msg: r.Why}
	}
	return nil, &Error{Kind: KindUnavailable, Msg: "неизвестный исход обращения"}
}

// trialFromBody разбирает тело ответа по существу. Чистая функция.
func trialFromBody(m map[string]interface{}, code int) *TrialAnswer {
	a := &TrialAnswer{Status: "answered", Code: code}
	a.Verdict, _ = m["verdict"].(string)
	a.Key, _ = m["key"].(string)
	a.Message, _ = m["message"].(string)
	a.Repeat, _ = m["repeat"].(bool)
	if e, ok := m["expires_at"].(float64); ok {
		a.ExpiresAt = int64(e)
	}
	if r, ok := m["retry_after"].(float64); ok {
		a.RetryAfter = int(r)
	}
	a.Limited = code == http.StatusTooManyRequests
	return a
}
