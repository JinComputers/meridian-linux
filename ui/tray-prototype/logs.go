// logs.go — логи для поддержки (просьба владельца 25.09): журнал
// интерфейса в файл, сборка отчёта (сводка + лог интерфейса + лог
// службы/помощника), обезличивание core.ScrubLog (правила — с котом 3),
// «Сохранить» / «Скопировать». Отправки в поддержку нет (решение
// владельца 01.10: присылали логи, а от кого — понять было нельзя).
package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/JinComputers/meridian-desktop/core"
)

const (
	uiLogMax      = 1 << 20 // больше — ui.log → ui.log.1
	reportUILines = 1500
	reportSvcLine = 1500
	previewLines  = 300
)

func logsDir() string   { return filepath.Join(appSupportDir(), "logs") }
func uiLogPath() string { return filepath.Join(logsDir(), "ui.log") }

// rotatingFile — ui.log с одной старой копией.
type rotatingFile struct {
	mu sync.Mutex
	f  *os.File
	n  int64
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return len(p), nil
	}
	if r.n+int64(len(p)) > uiLogMax {
		r.f.Close()
		_ = os.Rename(uiLogPath(), uiLogPath()+".1")
		f, err := os.OpenFile(uiLogPath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			r.f = nil
			return len(p), nil
		}
		r.f, r.n = f, 0
	}
	n, err := r.f.Write(p)
	r.n += int64(n)
	return n, err
}

// setupUILog — журнал интерфейса в logs/ui.log (и в stderr, если он есть).
// На Windows (-H windowsgui) и на Mac из Finder stderr никуда не идёт —
// без файла лог интерфейса терялся.
func setupUILog() {
	if err := os.MkdirAll(logsDir(), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(uiLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	st, _ := f.Stat()
	rf := &rotatingFile{f: f}
	if st != nil {
		rf.n = st.Size()
	}
	// BOM — только на пустой (только что созданный) файл, как у service.log
	// на Windows: без него текстовые редакторы иногда угадывают не UTF-8 и
	// показывают кириллицу кракозябрами (просьба владельца 27.09).
	if rf.n == 0 {
		if n, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err == nil {
			rf.n += int64(n)
		}
	}
	// Файл — ПЕРВЫМ. io.MultiWriter останавливается на первом Write,
	// вернувшем ошибку, и до следующих писателей не доходит: у сборки
	// -H windowsgui (Windows) и при запуске из Finder (macOS) стандартный
	// stderr не подключён, запись в него падает — со stderr первым файл
	// вообще не получал бы ни строки (найдено на этой сборке 25.09: пустой
	// ui.log при рабочем логе службы).
	log.SetOutput(redactWriter{io.MultiWriter(rf, os.Stderr)})
	log.Printf("=== Meridian VPN %s (%s/%s) запущен ===", appVersion, runtime.GOOS, runtime.GOARCH)
}

// tailLines — последние n строк файла (читает не больше 4 МБ с конца).
func tailLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	const max = 4 << 20
	off := int64(0)
	if st.Size() > max {
		off = st.Size() - max
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	data, _ := io.ReadAll(f)
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n")
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // первая — обрезок
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// exportScrub — обезличивание для «Сохранить» /
// «Скопировать». Только в выпускном канале (updateChannel == "release" —
// то, что уходит абонентам): в бета-канале (личная сборка владельца,
// update.go, build-msi.ps1 -Channel) пропускается — решение владельца
// 25.09, «логи в [выпускных] версиях должны отличаться от моей [бета],
// которая как дебаг по сути»: ему для разбора своих же отчётов нужны
// настоящие адреса/id, а не a.b.x.x. Ключ в лог не попадает НИКОГДА,
// независимо от канала — это отдельное, более строгое правило (CLAUDE.md).
func exportScrub(text string, o core.ScrubOptions) string {
	if updateChannel != "release" {
		return text
	}
	return core.ScrubLog(text, o)
}

// scrubOptions — что интерфейс знает для обезличивания.
func (a *app) scrubOptions() core.ScrubOptions {
	o := core.ScrubOptions{DeviceIDs: []string{a.deviceID}}
	for _, l := range vkLinks() {
		if h := core.NormalizeHash(l); h != "" {
			o.Hashes = append(o.Hashes, h)
		}
	}
	if h, err := os.Hostname(); err == nil {
		o.Names = append(o.Names, h)
	}
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		o.Names = append(o.Names, name)
	}
	// Ключ — вычеркнуть по значению, если его можно прочитать без вопроса
	// системы (на Mac чтение Keychain может спросить разрешения — там
	// ключ в лог и так не пишется).
	if keyInfoAtStart && hasSubscriberKey() {
		if k, err := getSubscriberKey(); err == nil {
			o.Secrets = append(o.Secrets, k)
		}
	}
	return o
}

func platformName() string {
	if runtime.GOOS == "darwin" {
		return "macOS"
	}
	if runtime.GOOS == "windows" {
		return "Windows"
	}
	return runtime.GOOS
}

// summary — сводка (для начала отчёта и тела письма).
func (a *app) summary() []string {
	a.state.mu.Lock()
	kind, notice, white, mtu := a.state.kind, a.state.notice, a.state.whiteMode, a.state.tunnelMTU
	a.state.mu.Unlock()
	st := loadSettings()
	mode := "чёрный (в туннель только список)"
	if white {
		mode = "белый (в туннель всё, кроме RU-адресов — напрямую)"
	}
	out := []string{
		fmt.Sprintf("Meridian VPN %s, %s %s", appVersion, platformName(), runtime.GOARCH),
		"Время: " + time.Now().Format("2006-01-02 15:04:05 -0700"),
		"Состояние: " + string(kind),
		"Режим: " + mode,
		"Доступ: " + accessText(st),
	}
	if nk := networkKind(); nk != "" {
		out = append(out, "Сеть: "+nk)
	}
	if mtu > 0 {
		out = append(out, fmt.Sprintf("MTU туннеля: %d", mtu))
	}
	if notice != "" {
		out = append(out, "Последнее сообщение: "+notice)
	}
	return out
}

// report — отчёт целиком, обезличенный.
func (a *app) report() string {
	var b strings.Builder
	for _, l := range a.summary() {
		b.WriteString(l + "\n")
	}
	if updateChannel == "release" {
		b.WriteString("Лог обезличен: ключ, ссылки ВК, id устройства, внешние адреса, имена пользователя и компьютера скрыты.\n")
	} else {
		b.WriteString("Бета-сборка (личная, не для отправки в поддержку абонентами): лог НЕ обезличен.\n")
	}
	b.WriteString("\n======== интерфейс ========\n")
	for _, l := range tailLines(uiLogPath(), reportUILines) {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n======== диагностика помощника ========\n")
	a.hc.mu.Lock()
	connected := a.hc.conn != nil
	a.hc.mu.Unlock()
	if connected {
		b.WriteString("Канал интерфейса: соединение есть на момент проверки\n")
	} else {
		b.WriteString("Канал интерфейса: соединения нет на момент проверки\n")
	}
	svc, queryStatus := a.serviceLog(reportSvcLine)
	b.WriteString("Запрос журнала службы: " + queryStatus + "\n")
	for _, line := range serviceStartupDiagnostics() {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n======== служба ========\n")
	if len(svc) == 0 {
		b.WriteString("(лог службы недоступен или пуст)\n")
	}
	for _, l := range svc {
		b.WriteString(l + "\n")
	}

	return exportScrub(b.String(), a.scrubOptions())
}

// serviceLog — хвост лога службы/помощника: сначала по протоколу
// (ControlLogsQuery → ControlLogs, служба сама обезличивает), до 3 с; не
// ответила (старая служба, нет связи) — файл, если платформа даёт его
// прочитать (serviceLogPath, host_*.go).
func (a *app) serviceLog(n int) ([]string, string) {
	select { // discard an old response
	case <-a.logsCh:
	default:
	}
	status := "не удалось отправить запрос"
	if err := a.hc.send(core.ControlLogsQuery, nil); err == nil {
		status = "ответ не получен за 3 секунды"
		select {
		case lines := <-a.logsCh:
			status = "служба ответила"
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}
			return lines, status
		case <-time.After(3 * time.Second):
		}
	}
	if p := serviceLogPath(); p != "" {
		return tailLines(p, n), status
	}
	return nil, status
}

// deliverLogs — ответ службы на ControlLogsQuery (из readLoop).
func (a *app) deliverLogs(lines []string) {
	select {
	case a.logsCh <- lines:
	default:
	}
}

func (a *app) logsResult(ok bool, msg string) {
	a.wails.Event.Emit("logs-result", map[string]any{"ok": ok, "message": msg})
}

// logsPreview — хвост отчёта для экрана «Логи».
func (a *app) logsPreview() {
	lines := strings.Split(a.report(), "\n")
	if len(lines) > previewLines {
		lines = append([]string{fmt.Sprintf("… показаны последние %d строк; в файл уходит больше", previewLines)}, lines[len(lines)-previewLines:]...)
	}
	a.wails.Event.Emit("logs-text", strings.Join(lines, "\n"))
}

func logStamp() string { return time.Now().Format("20060102-150405") }

func (a *app) saveLogs() { a.saveLogsWithMessage(false) }

func (a *app) saveLogsWithMessage(share bool) {
	path, err := a.wails.Dialog.SaveFile().
		AttachToWindow(a.window).
		SetFilename("meridian-log-"+logStamp()+".txt").
		AddFilter("Текстовый файл (*.txt)", "*.txt").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".txt"
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(a.report(), "\n", "\r\n")), 0o644); err != nil {
		a.logsResult(false, "не сохранилось: "+err.Error())
		return
	}
	if share {
		a.logsResult(true, "Отчёт сохранён: "+filepath.Base(path)+". Прикрепите его в Telegram или письме.")
	} else {
		a.logsResult(true, "сохранено: "+filepath.Base(path))
	}
}

// shareLogs prepares a scrubbed file for the user's chosen messenger or mail app.
func (a *app) shareLogs() { a.saveLogsWithMessage(true) }

func (a *app) copyLogs() {
	if a.wails.Clipboard.SetText(a.report()) {
		a.logsResult(true, "скопировано в буфер обмена")
	} else {
		a.logsResult(false, "скопировать не вышло")
	}
}

// keyInURL — ключ абонента в адресе запроса к API (строка ошибки
// url.Error несёт адрес целиком; 26.09 так ключ попал в ui.log).
var keyInURL = regexp.MustCompile(`key=[^&"\s]*`)

// redactWriter — затирает ключ в каждой строке журнала до записи.
type redactWriter struct{ w io.Writer }

func (r redactWriter) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("key=")) {
		if _, err := r.w.Write(keyInURL.ReplaceAll(p, []byte("key=REDACTED"))); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return r.w.Write(p)
}
