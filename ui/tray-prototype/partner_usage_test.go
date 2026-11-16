package main

import (
	"github.com/JinComputers/meridian-desktop/core"
	"strings"
	"testing"
)

func TestPartnerUsageAndExpiry(t *testing.T) {
	remaining := int64(0)
	st := settings{KeyExpiresAt: 1798761600, Partner: &core.PartnerUsage{Managed: true, Limit: 1000000000, Used: 1000000000, Remaining: &remaining, BlockedReason: "traffic_limit"}}
	got := partnerUsageWithExpiry(st)
	if !strings.Contains(got, "Лимит трафика исчерпан") || strings.Contains(got, "Доступ до ") || !strings.Contains(got, "осталось 0.00 ГБ") {
		t.Fatal(got)
	}
}

func TestUsageDisplaysAmountWithoutPartnerOrExpiry(t *testing.T) {
	for _, tc := range []struct {
		limit int64
		want  string
	}{{5000000000, "Использовано 1.00 из 5.00 ГБ"}, {0, "Использовано 1.00 ГБ · без лимита"}} {
		got := partnerUsageWithExpiry(settings{KeyExpiresAt: 1798761600, Partner: &core.PartnerUsage{Used: 1000000000, Limit: tc.limit}})
		if !strings.Contains(got, tc.want) || strings.Contains(got, "Партнёрский ключ") || strings.Contains(got, "Доступ до") || strings.Contains(got, "Срок доступа") {
			t.Fatalf("unexpected: %q", got)
		}
	}
}
