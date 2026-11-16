package main

import (
	"strings"
	"testing"

	"github.com/JinComputers/meridian-desktop/core"
)

// «Базовый»: белого режима нет при любом сохранённом выборе.
func TestWhiteModeBasic(t *testing.T) {
	on, off := true, false
	for _, c := range []struct {
		name  string
		st    settings
		white bool
	}{
		{"премиум, новая установка", settings{}, true},
		{"премиум, белый", settings{WhiteMode: &on}, true},
		{"премиум, чёрный", settings{WhiteMode: &off}, false},
		{"базовый, новая установка", settings{KeyTier: "basic"}, false},
		{"базовый, белый сохранён", settings{KeyTier: "basic", WhiteMode: &on}, false},
	} {
		if got := c.st.whiteMode(); got != c.white {
			t.Errorf("%s: whiteMode=%v, want %v", c.name, got, c.white)
		}
	}
}

func TestFailureTextTierRelay(t *testing.T) {
	got := failureText(core.ControlErrorPayload{Message: "шлюз: FATAL_TIER_RELAY"})
	if got != "На тарифе «Базовый» этот способ недоступен" {
		t.Errorf("FATAL_TIER_RELAY → %q", got)
	}
}

func TestNoLinksText(t *testing.T) {
	const path = "«Настройки → Обход белых списков»"
	if got := noLinksText(false, path); !strings.Contains(got, "DION") || !strings.Contains(got, "ссылок на звонки VK") || strings.Contains(strings.ToLower(got), "хеш") {
		t.Errorf("премиум: %q", got)
	}
	if got := noLinksText(true, path); strings.Contains(got, "DION") || strings.Contains(strings.ToLower(got), "хеш") {
		t.Errorf("базовый: %q", got)
	}
}
