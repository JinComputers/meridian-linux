// lists.go — «Режимы туннеля» (решение владельца 25.09): один
// переключатель, два списка.
//   - Чёрный режим (по умолчанию): в туннель идут ТОЛЬКО домены/адреса
//     списка tunnel-domains.txt, остальное напрямую; пустой список — в
//     туннель ничего (core.TunnelRouter).
//   - Белый режим: только VK-релей, домены/адреса списка direct-domains.txt
//     идут напрямую (core.DirectRouter).
//
// Импорт/экспорт — обычный .txt, по записи в строке. Импорт ДОПИСЫВАЕТ к
// текущему списку (без повторов), а не заменяет: случайный импорт не
// должен молча стереть собранное руками.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/JinComputers/meridian-desktop/core"
)

const defaultBasicRoutes = `console.anthropic.com
aistudio.google.com
suno-data-uploads.s3.amazonaws.com
googleusercontent.com
auth0.openai.com
auth.openai.com
makersuite.google.com
deepmind.google
livekit.cloud
anthropic.com
platform.claude.com
gemini.google.com
openai.com
oaistatic.com
cdn.oaistatic.com
generativelanguage.googleapis.com
docs.anthropic.com
support.claude.com
bard.google.com
chatgpt.com
statsigapi.net
claudeusercontent.com
claude.com
deepmind.com
oaiusercontent.com
claude.ai
telegram.me
tutamail.com
patreon.com
web.whatsapp.com
canva.com
tuta.com
t.me
binance.com
payoneer.com
interactivebrokers.com
gcore.com
sonarworks.com
featuregates.org
my.amnezia.host
meridianvpn.org
play.google.com
speedtest.net
`

func readTunnelDomains() string {
	data, err := os.ReadFile(tunnelDomainsPath())
	if err == nil {
		return string(data)
	}
	if os.IsNotExist(err) && loadSettings().basic() {
		return defaultBasicRoutes
	}
	return ""
}

func tunnelDomainsPath() string { return filepath.Join(appSupportDir(), "tunnel-domains.txt") }

func readList(mode string) string {
	if mode == "black" {
		return readTunnelDomains()
	}
	return readFile(listPath(mode))
}

// listPath — файл списка режима: "black" / "white".
func listPath(mode string) string {
	if mode == "white" {
		return directDomainsPath()
	}
	return tunnelDomainsPath()
}

func listFileName(mode string) string {
	if mode == "white" {
		return "meridian-belyj-rezhim.txt"
	}
	return "meridian-chernyj-rezhim.txt"
}

func (a *app) listResult(mode string, ok bool, msg string) {
	a.wails.Event.Emit("list-result", map[string]any{"mode": mode, "ok": ok, "message": msg})
}

// importList — выбрать .txt и дописать к списку режима.
func (a *app) importList(mode string) {
	// AttachToWindow: на macOS (ActivationPolicyAccessory) диалог без
	// окна-хозяина может открыться позади других окон — прикреплённый
	// выходит листом из нашего окна.
	path, err := a.wails.Dialog.OpenFile().
		AttachToWindow(a.window).
		SetTitle("Импорт списка").
		AddFilter("Текстовый файл (*.txt)", "*.txt").
		AddFilter("Все файлы", "*.*").
		CanChooseFiles(true).
		PromptForSingleSelection()
	if err != nil || path == "" {
		return // отмена
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.listResult(mode, false, "файл не прочитался")
		return
	}
	if len(data) > 4<<20 {
		a.listResult(mode, false, "файл слишком большой")
		return
	}
	cur := core.ParseDirectDomains(readList(mode))
	seen := map[string]bool{}
	for _, d := range cur {
		seen[d] = true
	}
	added := 0
	for _, d := range core.ParseDirectDomains(string(data)) {
		if !seen[d] {
			seen[d] = true
			cur = append(cur, d)
			added++
		}
	}
	writeFile(listPath(mode), strings.Join(cur, "\n")+"\n")
	log.Printf("списки: импорт %s → %s, добавлено %d", filepath.Base(path), mode, added)
	a.listResult(mode, true, fmt.Sprintf("добавлено %d, всего %d", added, len(cur)))
	a.applyLocal()
}

// exportList — сохранить список режима в .txt (по записи в строке).
func (a *app) exportList(mode string) {
	list := core.ParseDirectDomains(readList(mode))
	if len(list) == 0 {
		a.listResult(mode, false, "список пуст — сохранять нечего")
		return
	}
	path, err := a.wails.Dialog.SaveFile().
		AttachToWindow(a.window).
		SetFilename(listFileName(mode)).
		AddFilter("Текстовый файл (*.txt)", "*.txt").
		PromptForSingleSelection()
	if err != nil || path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".txt"
	}
	if err := os.WriteFile(path, []byte(strings.Join(list, "\r\n")+"\r\n"), 0o644); err != nil {
		a.listResult(mode, false, "не сохранилось: "+err.Error())
		return
	}
	a.listResult(mode, true, fmt.Sprintf("сохранено %d в %s", len(list), filepath.Base(path)))
}
