//go:build !windows

package main

// apps_other.go — на этой платформе выбор приложений для туннеля ещё не
// сделан (Windows — apps_windows.go; Mac — Кот 5): список всегда пуст, путь
// не принимается.

// appsSupported — на Mac приложения в туннель не делаем (решение владельца
// 27.10: без Network Extension надёжного способа нет); блок скрыт в интерфейсе.
const appsSupported = false

type appInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func listInstalledApps() []appInfo { return nil }
func listRunningApps() []appInfo   { return nil }
func appPathValid(string) bool     { return false }
