package main

import (
	"log"
	"sync"
	"time"

	"github.com/JinComputers/meridian-desktop/core"
)

// captcha_flow.go — ручная капча VK (решение владельца 18.10.2026: «ручную
// выдавать по кнопке, клиент решит сам»). Служба шлёт ControlCaptchaRequest,
// в окне появляется карточка с кнопкой «Решить капчу»; по нажатию открывается
// окно с капчей (captcha_window_*), итог уходит службе ControlCaptchaResult.
// Токен и адрес страницы в лог не пишутся.

// captchaState — текущий запрос от службы (один за раз).
type captchaState struct {
	mu      sync.Mutex
	id      string
	url     string
	solving bool
	expires time.Time
}

// pending — есть ли непросроченный запрос (для поля "captcha" в состоянии окна).
func (c *captchaState) pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id != "" && time.Now().Before(c.expires)
}

// captchaRequestTTL — запрос в карточке держим с запасом: служба ждёт 5 минут.
const captchaRequestTTL = 6 * time.Minute

// onCaptchaRequest — служба просит капчу (или снимает запрос).
func (a *app) onCaptchaRequest(p core.ControlCaptchaPayload) {
	a.captcha.mu.Lock()
	if p.Done {
		if a.captcha.id == p.ID {
			a.captcha.id, a.captcha.url = "", ""
		}
		a.captcha.mu.Unlock()
		a.applyLocal()
		return
	}
	if !captchaURLAllowed(p.URL) {
		a.captcha.mu.Unlock()
		log.Printf("капча VK: адрес страницы не годится — отказываю")
		a.answerCaptcha(p.ID, "", "closed")
		return
	}
	a.captcha.id, a.captcha.url = p.ID, p.URL
	a.captcha.expires = time.Now().Add(captchaRequestTTL)
	a.captcha.mu.Unlock()
	log.Printf("капча VK: нужен человек — показываю кнопку")
	time.AfterFunc(captchaRequestTTL+time.Second, a.applyLocal) // карточка гаснет сама
	a.showWindow() // без человека капча не решится — выводим окно
	a.applyLocal()
}

// solveCaptcha — человек нажал «Решить капчу».
func (a *app) solveCaptcha() {
	a.captcha.mu.Lock()
	id, url := a.captcha.id, a.captcha.url
	if id == "" || a.captcha.solving || !time.Now().Before(a.captcha.expires) {
		a.captcha.mu.Unlock()
		return
	}
	a.captcha.solving = true
	a.captcha.mu.Unlock()

	token, reason := solveCaptchaWindow(url)

	a.captcha.mu.Lock()
	a.captcha.solving = false
	still := a.captcha.id == id
	a.captcha.mu.Unlock()
	if token != "" {
		log.Printf("капча VK: решена")
	} else {
		log.Printf("капча VK: не решена (%s)", reason)
	}
	if still { // службе ответ нужен, пока запрос не снят
		a.answerCaptcha(id, token, reason)
	}
}

func (a *app) answerCaptcha(id, token, reason string) {
	if err := a.hc.send(core.ControlCaptchaResult, core.ControlCaptchaPayload{ID: id, Token: token, Reason: reason}); err != nil {
		log.Printf("капча VK: ответ службе не ушёл")
	}
	if token == "" {
		// Отказ/закрытие: карточку убираем, службе больше нечего ждать.
		a.captcha.mu.Lock()
		if a.captcha.id == id {
			a.captcha.id, a.captcha.url = "", ""
		}
		a.captcha.mu.Unlock()
		a.applyLocal()
	}
}
