package core

import (
	"strings"
	"testing"
)

// Вектор кота 2: stendKEY1 → 9eeca08fd33ef2d2.
func TestUpgradeLink(t *testing.T) {
	if got := UpgradeID("stendKEY1"); got != "9eeca08fd33ef2d2" {
		t.Errorf("UpgradeID = %q", got)
	}
	u := UpgradeURL("stendKEY1")
	if u != "https://t.me/jincomputers_bot?start=up_9eeca08fd33ef2d2" {
		t.Errorf("UpgradeURL = %q", u)
	}
	if a := UpgradeAppURL("stendKEY1"); a != "tg://resolve?domain=jincomputers_bot&start=up_9eeca08fd33ef2d2" {
		t.Errorf("UpgradeAppURL = %q", a)
	}
	if strings.Contains(u, "stendKEY1") {
		t.Error("ключ попал в ссылку")
	}
}
