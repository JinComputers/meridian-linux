//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/JinComputers/meridian-desktop/core"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/unix"
)

const deviceIDPrefix = "meridian-linux-"
const hostSettingsMenuLabel = ""
const keyInfoAtStart = false
const updatePlatform = "linux"
const controlSocketPath = "/run/meridian/control.sock"

func appSupportDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		panic("не задан каталог настроек пользователя")
	}
	return filepath.Join(d, "Meridian")
}
func showStartupError(message string) {
	_ = exec.Command("zenity", "--error", "--title=Meridian VPN", "--text="+message).Run()
}

func hostUnavailableText(err error) string {
	return fmt.Sprintf("Служба Meridian недоступна: %v", err)
}
func dialControl() (net.Conn, error) {
	c, err := net.Dial("unix", controlSocketPath)
	if err != nil {
		return nil, err
	}
	// The peer must be the privileged service; a user-owned socket is not trusted.
	raw, err := c.(*net.UnixConn).SyscallConn()
	if err != nil {
		c.Close()
		return nil, err
	}
	var cred *unix.Ucred
	var peerErr error
	err = raw.Control(func(fd uintptr) { cred, peerErr = unix.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, unix.SO_PEERCRED) })
	if err != nil || peerErr != nil || cred == nil || cred.Uid != 0 {
		c.Close()
		return nil, errors.New("не удалось проверить владельца службы")
	}
	if err := core.PerformControlHelloAsClient(c, appVersion); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

var errKeyAbsent = errors.New("ключ не сохранён")
var errKeyStoreUnavailable = errors.New("системное хранилище ключей недоступно")

// secret-tool returns status 1 without diagnostics when no item matches.
// Any diagnostic or other failure is treated as a storage error, not absence.
func classifyKeyLookup(out []byte, err error) (string, error) {
	if err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) && e.ExitCode() == 1 && len(e.Stderr) == 0 {
			return "", errKeyAbsent
		}
		return "", errKeyStoreUnavailable
	}
	key := strings.TrimSuffix(string(out), "\n")
	if key == "" {
		return "", errKeyAbsent
	}
	return key, nil
}
func getSubscriberKey() (string, error) {
	out, err := exec.Command("secret-tool", "lookup", "application", "meridian-vpn", "kind", "subscriber-key").Output()
	return classifyKeyLookup(out, err)
}
func hasSubscriberKey() bool { k, err := getSubscriberKey(); return err == nil && k != "" }
func setSubscriberKey(key string) error {
	cmd := exec.Command("secret-tool", "store", "--label=Meridian VPN", "application", "meridian-vpn", "kind", "subscriber-key")
	cmd.Stdin = bytes.NewBufferString(key)
	if err := cmd.Run(); err != nil {
		return errors.New("не удалось сохранить ключ в системном хранилище")
	}
	return nil
}
func setRecoveredSubscriberKey(key string) error { return setSubscriberKey(key) }
func openHostSettings()                          {}
func launchedAtLogin() bool                      { return false }
func (a *app) ensureServices()                   { a.menuAuto.SetEnabled(false) }
func (a *app) refreshAutostartItem()             { a.menuAuto.SetEnabled(false) }
func (a *app) toggleAutostart()                  {}
func unregisterServices() int                    { return 0 }
func autostartState() (bool, bool)               { return false, false }
func setTrayIcon(tray *application.SystemTray, full bool) {
	if full {
		tray.SetIcon(trayFullWhite)
	} else {
		tray.SetIcon(trayDimWhite)
	}
}
func hostDeviceID() string                { return "" }
func serviceLogPath() string              { return "" }
func serviceStartupDiagnostics() []string { return nil }
func getUpdateToken() string              { return "" }
func setUpdateToken(string) error {
	return errors.New("обновления для Linux пока недоступны")
}
func networkKind() string      { return "" }
func hasTelegramHandler() bool { return false }
func revealFile(path string)   { _ = exec.Command("xdg-open", filepath.Dir(path)).Start() }
func installUpdate(a *app, path, version string, reconnect bool) error {
	return errors.New("обновления для Linux пока недоступны")
}
func runUpdateHelper(args []string) (int, bool) { return 0, false }
func platformProcessNames() []string            { return nil }
func runCaptchaWindow() int                     { return 1 }
func solveCaptchaWindow(target string) (string, string) {
	return "", "Ручная проверка VK пока недоступна в Linux"
}
