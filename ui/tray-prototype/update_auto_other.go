//go:build !windows && !darwin

package main

import "time"

// Тихая установка — только Windows (update_auto.go).
const autoInstallSupported = false

func autoInstallPossible() bool  { return false }
func userIdleFor() time.Duration { return 0 }
func userFullscreenBusy() bool   { return true }
func autoInstallViaService(a *app, version string, reconnect bool) (bool, error) {
	return false, nil
}
