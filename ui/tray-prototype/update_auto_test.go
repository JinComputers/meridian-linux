package main

import (
	"testing"
	"time"
)

func TestAutoInstallAllowed(t *testing.T) {
	cases := []struct {
		name      string
		connected bool
		idle      time.Duration
		fullscr   bool
		want      bool
	}{
		{"только что нажимал", false, 10 * time.Second, false, false},
		{"4 минуты тишины, не подключён", false, 4 * time.Minute, false, false},
		{"5 минут тишины, не подключён", false, 5 * time.Minute, false, true},
		{"9 минут, подключён — рано", true, 9 * time.Minute, false, false},
		{"10 минут, подключён", true, 10 * time.Minute, false, true},
		{"долго нет, но идёт игра/показ", false, 3 * time.Hour, true, false},
		{"ночь, экран погас", true, 6 * time.Hour, false, true},
	}
	for _, c := range cases {
		if got := autoInstallAllowed(c.connected, c.idle, c.fullscr); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.name, got, c.want)
		}
	}
}

func TestAutoFailedRecently(t *testing.T) {
	now := time.Now()
	at := now.Add(-2 * time.Hour).Unix()
	if !autoFailedRecently("0.5.61", at, "0.5.61", now) {
		t.Error("та же версия 2 часа назад — ждать")
	}
	if autoFailedRecently("0.5.61", at, "0.5.62", now) {
		t.Error("другая версия — пробуем")
	}
	if autoFailedRecently("0.5.61", now.Add(-25*time.Hour).Unix(), "0.5.61", now) {
		t.Error("прошли сутки — пробуем снова")
	}
	if autoFailedRecently("", 0, "0.5.61", now) {
		t.Error("записи нет — пробуем")
	}
	// часы ушли назад: запись «из будущего» не должна блокировать навсегда
	if autoFailedRecently("0.5.61", now.Add(48*time.Hour).Unix(), "0.5.61", now) {
		t.Error("запись из будущего")
	}
}
