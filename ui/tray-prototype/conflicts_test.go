package main

import (
	"reflect"
	"testing"

	"github.com/JinComputers/meridian-desktop/core"
)

func TestConflictingPrograms(t *testing.T) {
	got := conflictingPrograms([]string{"launchd", "tpws", "nfqws", "Sing-Box", "WinWS.exe", "meridian-helper", "Safari", ""})
	want := []string{"Zapret", "sing-box"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, хочу %v", got, want)
	}
	if got := conflictingPrograms([]string{"Safari", "meridian-vpn"}); len(got) != 0 {
		t.Fatalf("лишнее: %v", got)
	}
}

func TestWithConflictHint(t *testing.T) {
	old := runningProcessNames
	defer func() { runningProcessNames = old }()
	runningProcessNames = func() []string { return []string{"goodbyedpi.exe", "OpenVPN"} }

	blocked := core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Failure: "blocked-relay"}
	got := withConflictHint("Сервер недоступен из этой сети", blocked)
	want := "Сервер недоступен из этой сети. Запущено, что может мешать: GoodbyeDPI, OpenVPN. Выключите на время подключения"
	if got != want {
		t.Fatalf("%q", got)
	}

	for _, ep := range []core.ControlErrorPayload{
		{Code: core.ControlErrConnectFailed, Failure: "silent"},
		{Code: core.ControlErrConnectFailed, Message: "FATAL_AUTH", Failure: "refused"},
		{Code: core.ControlErrConnectFailed, Message: "FATAL_DEACTIVATED"},
		{Code: core.ControlErrConnectFailed, Message: "FATAL_BOUND_OTHER", Failure: "refused"},
	} {
		if got := withConflictHint("Не удалось подключиться: ключ не принят сервером", ep); got != "Не удалось подключиться: ключ не принят сервером" {
			t.Fatalf("подсказка при отказе по ключу: %q (%+v)", got, ep)
		}
	}
	if got := withConflictHint(noNetworkText, blocked); got != noNetworkText {
		t.Fatalf("%q", got)
	}
	if got := withConflictHint("", blocked); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestWithConflictHintNothingRunning(t *testing.T) {
	old := runningProcessNames
	defer func() { runningProcessNames = old }()
	runningProcessNames = func() []string { return []string{"Safari"} }
	if got := withConflictHint("Не удалось подключиться", core.ControlErrorPayload{}); got != "Не удалось подключиться" {
		t.Fatalf("%q", got)
	}
}

func TestConflictHintAppliesToConnectFailuresButNotKeyProblems(t *testing.T) {
	ep := core.ControlErrorPayload{Failure: "unreachable", Message: "all failed"}
	for _, text := range []string{
		"Не удалось подключиться",
		"Не удалось подключиться: сервер недоступен из этой сети",
	} {
		if !conflictHintApplies(ep, text) {
			t.Errorf("%q: подсказка должна применяться (в «подключиться» нет слова «ключ»)", text)
		}
	}
	for _, text := range []string{
		"ключ отключён — напишите в поддержку",
		"Не удалось подключиться: нет ключа доступа",
		"Ключ не подошёл",
	} {
		if conflictHintApplies(ep, text) {
			t.Errorf("%q: про ключ, посторонняя программа ни при чём", text)
		}
	}
}

func TestFailureTextKeyBoundToOtherDevice(t *testing.T) {
	ep := core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Failure: "refused", Message: "FATAL_BOUND_OTHER"}
	want := "Не удалось подключиться: ключ уже привязан к другому устройству. Один ключ работает только на одном устройстве. Если вы сменили устройство — обратитесь в поддержку"
	if got := failureText(ep); got != want {
		t.Fatalf("%q", got)
	}
	// Если движок ещё не знает причину, он отдаёт её как общий отказ по ключу:
	// текст не должен подменяться на «ключ не принят».
	ep.Message = "FATAL_AUTH: пароль неверный или истёк"
	if got := failureText(ep); got == want {
		t.Fatalf("общий отказ принят за привязку: %q", got)
	}
}
