package main

import (
	"testing"
	"time"
)

// Цвет кнопки доступа: зелёный — идёт, жёлтый — кончается за неделю,
// красный — кончилась, пусто — срок неизвестен.
func TestAccessTone(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) settings { return settings{KeyExpiresAt: now.Add(d).Unix()} }
	for _, c := range []struct {
		name string
		st   settings
		want string
	}{
		{"месяц впереди", at(30 * 24 * time.Hour), "ok"},
		{"ровно 8 дней", at(8 * 24 * time.Hour), "ok"},
		{"ровно неделя", at(7 * 24 * time.Hour), "warn"},
		{"завтра", at(24 * time.Hour), "warn"},
		{"через минуту", at(time.Minute), "warn"},
		{"только что кончилась", at(-time.Minute), "bad"},
		{"давно кончилась", at(-90 * 24 * time.Hour), "bad"},
		{"пробный, 2 дня впереди", trialAt(now, 48*time.Hour, 48*time.Hour), "ok"},
		{"пробный, ровно сутки", trialAt(now, 24*time.Hour, 24*time.Hour), "warn"},
		{"пробный, кончился", trialAt(now, -time.Minute, 72*time.Hour), "bad"},
		{"пробный продлён на месяц — уже обычный, зелёный", trialAt(now, 30*24*time.Hour, 72*time.Hour), "ok"},
		{"бессрочный", settings{KeyPermanent: true}, "ok"},
		{"срок неизвестен", settings{KeyConfirmed: true}, ""},
		{"ключ только принят", settings{}, ""},
	} {
		if got := accessTone(c.st, now); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// trialAt — пробный ключ: left до конца сейчас, выдан со сроком granted от now.
func trialAt(now time.Time, left, granted time.Duration) settings {
	return settings{KeyTrial: true, KeyExpiresAt: now.Add(left).Unix(), KeyTrialEnd: now.Add(granted).Unix()}
}

// Пробный период: в строке доступа точное время, у обычного ключа — только дата;
// продлённый пробный ключ перестаёт быть пробным (кнопка «Купить ключ» уходит).
func TestAccessTextTrial(t *testing.T) {
	end := time.Date(2026, 10, 24, 15, 42, 0, 0, time.Local)
	trial := settings{KeyTrial: true, KeyExpiresAt: end.Unix(), KeyTrialEnd: end.Unix()}
	if got, want := accessText(trial), "Пробный период до 24.10.26 15:42"; got != want {
		t.Errorf("пробный: %q, want %q", got, want)
	}
	if !trial.trial() {
		t.Error("свежий пробный ключ должен быть пробным")
	}
	paid := settings{KeyExpiresAt: end.Unix()}
	if got, want := accessText(paid), "Доступ до 24.10.26"; got != want {
		t.Errorf("обычный: %q, want %q", got, want)
	}
	extended := trial
	extended.KeyExpiresAt = end.Add(30 * 24 * time.Hour).Unix()
	if extended.trial() {
		t.Error("продлённый на месяц ключ не должен считаться пробным")
	}
	if got := accessText(extended); got != "Доступ до 23.11.26" {
		t.Errorf("продлённый: %q", got)
	}
	if (settings{KeyExpiresAt: end.Unix()}).trial() {
		t.Error("ключ без метки пробного не может быть пробным")
	}
	// служба молчала, срока нет: метка остаётся, кнопка покупки нужна
	if !(settings{KeyTrial: true}).trial() {
		t.Error("пробный без срока: метка должна держаться")
	}
}
