// apps.go — «Приложения в туннель» (решение владельца 27.10.2026): в чёрном
// режиме сверх списка адресов и доменов в туннель идёт трафик выбранных
// программ (core.AppSplitRouter). Список хранится в settings.json; изменение
// действует со следующего подключения.
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/JinComputers/meridian-desktop/core"
)

// maxTunnelApps — предел списка (защита от случайного «выбрать всё»).
const maxTunnelApps = 64

type tunnelApp struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func tunnelAppPaths(st settings) []string {
	paths := make([]string, 0, len(st.TunnelApps))
	for _, a := range st.TunnelApps {
		paths = append(paths, a.Path)
	}
	return core.NormalizeApps(paths)
}

func (a *app) appsNote(ok bool, msg string) {
	a.wails.Event.Emit("apps-note", map[string]any{"ok": ok, "message": msg})
}

// appsList — установленные и запущенные программы для окна выбора.
func (a *app) appsList() {
	a.wails.Event.Emit("apps-result", map[string]any{
		"installed": listInstalledApps(),
		"running":   listRunningApps(),
	})
}

// appsAdd — дописать приложение (без повторов, без учёта регистра).
func (a *app) appsAdd(name, path string) {
	path = strings.Trim(strings.TrimSpace(path), `"`)
	if !appPathValid(path) {
		a.appsNote(false, "путь не подходит: нужен существующий .exe или каталог программы")
		return
	}
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	added := false
	full := false
	updateSettings(func(s *settings) {
		for _, e := range s.TunnelApps {
			if strings.EqualFold(e.Path, path) {
				return
			}
		}
		if len(s.TunnelApps) >= maxTunnelApps {
			full = true
			return
		}
		s.TunnelApps = append(s.TunnelApps, tunnelApp{Name: strings.TrimSpace(name), Path: path})
		added = true
	})
	switch {
	case full:
		a.appsNote(false, fmt.Sprintf("в списке уже %d приложений — это предел", maxTunnelApps))
	case !added:
		a.appsNote(true, "уже в списке")
	default:
		log.Printf("приложения в туннель: добавлено (записей в списке изменилось)")
		a.appsNote(true, "добавлено; заработает при следующем подключении")
	}
	a.applyLocal()
}

func (a *app) appsRemove(path string) {
	updateSettings(func(s *settings) {
		out := s.TunnelApps[:0]
		for _, e := range s.TunnelApps {
			if !strings.EqualFold(e.Path, path) {
				out = append(out, e)
			}
		}
		s.TunnelApps = out
	})
	a.appsNote(true, "убрано; заработает при следующем подключении")
	a.applyLocal()
}

// appsBrowse — выбрать .exe через системный диалог.
func (a *app) appsBrowse() {
	path, err := a.wails.Dialog.OpenFile().
		AttachToWindow(a.window).
		SetTitle("Выберите программу").
		AddFilter("Программа (*.exe)", "*.exe").
		CanChooseFiles(true).
		PromptForSingleSelection()
	if err != nil || path == "" {
		return
	}
	a.appsAdd("", path)
}
