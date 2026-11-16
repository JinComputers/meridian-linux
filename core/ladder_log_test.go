package core

import "testing"

func TestLadderFailureReason(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{"dial tcp: context deadline exceeded", "нет ответа за отведённое время"},
		{"context canceled", "попытка отменена"},
		{"connection refused", "соединение отклонено"},
		{"secret password", "причина не распознана"},
	} {
		if got := ladderFailureReason(c.input); got != c.want {
			t.Errorf("%q: %q", c.input, got)
		}
	}
	for _, s := range []string{"A: транспорт не поднялся: context deadline exceeded", "ступень C: AUTH не прошёл: context canceled", "ступень R — неудача за 2s: unknown"} {
		if ladderFailure.FindStringSubmatch(s) == nil {
			t.Errorf("не распознал %q", s)
		}
	}
}
