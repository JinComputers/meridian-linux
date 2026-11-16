package main

import (
	"strings"
	"testing"

	"github.com/JinComputers/meridian-desktop/core"
)

// Жалоба владельца 26.09: «Не удалось подключиться: подключиться не
// удалось» — одно и то же дважды. Причина не узнана — короткая строка;
// служба (/v1/params) недоступна — так и сказать.
func TestFailureTextNoRepeat(t *testing.T) {
	cases := []struct {
		ep   core.ControlErrorPayload
		want string
	}{
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Connect: движок: что-то"}, "Не удалось подключиться"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Failure: "unknown"}, "Не удалось подключиться"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Connect: /v1/params: все адреса: i/o timeout", Failure: core.FailureAPIUnavailable}, "Не удалось связаться с серверами Meridian. Возможно, сеть или провайдер блокирует соединение. Попробуйте другую сеть (например, раздачу с телефона) или укажите в настройках сети DNS 1.1.1.1"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Failure: "resolve"}, "Адрес сервера не находится, проверьте интернет"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Connect: движок: лестница пройдена без успеха, последняя ошибка: сервер не подтвердил подлинность, пароль не отправлен", Failure: "blocked-relay"}, "Сервер недоступен из этой сети ни напрямую, ни через ВК-звонок. " + retryHint + "Не помогло — попробуйте другую сеть (например, Wi-Fi) или повторите позже"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Connect: движок: лестница пройдена без успеха, последняя ошибка: шлюз не ответил на проверку подлинности", Failure: "silent"}, "Сервер не узнал ключ. Если ключ получен только что — подождите минуту и подключитесь снова"},
		{core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "FATAL_AUTH", Failure: "refused"}, "Не удалось подключиться: ключ не принят сервером"},
	}
	for _, c := range cases {
		got := failureText(c.ep)
		if got != c.want {
			t.Errorf("failureText(%+v) = %q, want %q", c.ep, got, c.want)
		}
		if strings.Count(strings.ToLower(got), "не удалось") > 1 {
			t.Errorf("повтор в %q", got)
		}
	}
}

// Отказ из-за сервера или его адреса — просим нажать на планету ещё раз
// (повторное подключение заново запрашивает /v1/params); отказы по ключу и
// ссылкам этой просьбы не получают — там повтор не поможет.
func TestFailureTextAsksRetry(t *testing.T) {
	ask := func(ep core.ControlErrorPayload) bool {
		return strings.Contains(failureText(ep), "Нажмите на планету ещё раз")
	}
	for name, ep := range map[string]core.ControlErrorPayload{
		"blocked":       {Code: core.ControlErrConnectFailed, Failure: "blocked"},
		"blocked-relay": {Code: core.ControlErrConnectFailed, Failure: "blocked-relay"},
		"переполнен":    {Code: core.ControlErrConnectFailed, Message: "FATAL_POOL_FULL", Failure: "refused"},
		"шлюз молчит":   {Code: core.ControlErrConnectFailed, Message: "шлюз не ответил на проверку", Failure: "network"},
	} {
		if !ask(ep) {
			t.Errorf("%s: нет просьбы нажать ещё раз: %q", name, failureText(ep))
		}
	}
	for name, ep := range map[string]core.ControlErrorPayload{
		"ключ не принят":  {Code: core.ControlErrConnectFailed, Message: "FATAL_AUTH", Failure: "refused"},
		"нет ссылок":      {Code: core.ControlErrConnectFailed, Failure: "blocked-nolinks"},
		"ключ неизвестен": {Code: core.ControlErrConnectFailed, Failure: "silent"},
	} {
		if ask(ep) {
			t.Errorf("%s: просьба нажать ещё раз лишняя: %q", name, failureText(ep))
		}
	}
}

func TestFailureTextForTunnelDefaultRoute(t *testing.T) {
	got := failureText(core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Snapshot: physical_route_tunnel: системный туннель"})
	if !strings.Contains(got, "VPN") || !strings.Contains(got, "туннель") {
		t.Fatalf("missing actionable tunnel diagnosis: %q", got)
	}
}
