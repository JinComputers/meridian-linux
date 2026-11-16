package core

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner выполняет внешние команды (route, ifconfig, scutil, …).
// Абстракция нужна для тестирования кода маршрутов/DNS без root: в
// тестах подменяется фейковым исполнителем, который только записывает,
// что было бы вызвано, реальные команды не выполняются.
type CommandRunner interface {
	// Run выполняет команду и возвращает её вывод (stdout+stderr вместе —
	// so для диагностики видно всё). Ошибка включает этот же вывод.
	Run(ctx context.Context, name string, args ...string) (output string, err error)

	// RunWithInput — то же самое, но с текстом на стандартном вводе.
	// Нужен для команд, которые не принимают путь к файлу-скрипту
	// аргументом, только stdin (scutil — сверено с `man scutil`: в
	// синопсисе нет формы с файлом, только чтение команд со стандартного
	// ввода; первая попытка передать их файлом-аргументом молча повисала
	// на EOF пустого stdin — баг, пойманный живым прогоном, не тестами,
	// потому что тесты стояли на фейковом исполнителе).
	RunWithInput(ctx context.Context, name string, input string, args ...string) (output string, err error)
}

// ExecRunner — боевой исполнитель поверх os/exec. Требует тех прав,
// которые нужны самой команде (route/ifconfig/scutil — root).
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}
	return string(out), nil
}

func (ExecRunner) RunWithInput(ctx context.Context, name string, input string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v (stdin %d байт): %w: %s", name, args, len(input), err, out)
	}
	return string(out), nil
}
