// update_auto.go — обновление «само»: пока человек не за компьютером, новая
// версия ставится тихо, без нажатия на строку версии (решение владельца
// 27.10). Находит версию по-прежнему update.go (тихая проверка раз в сутки);
// здесь — когда ставить. Саму установку делает служба (platform/windows/
// selfupdate), потому что установщику нужны права администратора, а окно
// UAC посреди работы человека — не «само». Только Windows; на macOS
// autoInstallSupported == false и всё остаётся как было (строка и кнопка).
//
// Когда ставим (предложено владельцу, цифры — константы ниже):
//   - нет ни одного нажатия клавиши/движения мыши 5 минут (10, если туннель
//     подключён: установка перезапускает службу, туннель рвётся на несколько
//     секунд и поднимается сам, но лучше, чтобы за это время человек точно не
//     смотрел видео через него);
//   - нет полноэкранной программы (игра, показ) и режима «не беспокоить
//     презентацией»; экран заблокирован/заставка — лучший момент;
//   - перед самым запуском проверяем это снова, а в разгар ввода не ставим.
//
// Не вышло — молчим (человеку это не нужно), ту же версию сами пробуем не
// раньше чем через сутки; строка и кнопка «Обновить» остаются как были.
package main

import (
	"log"
	"time"
)

const (
	autoPoll             = time.Minute
	autoIdleDisconnected = 5 * time.Minute
	autoIdleConnected    = 10 * time.Minute
	autoFailBackoff      = 24 * time.Hour
)

// autoInstallAllowed — можно ли сейчас ставить сами. Чистая функция: все
// условия приходят параметрами.
func autoInstallAllowed(connected bool, idle time.Duration, fullscreenBusy bool) bool {
	if fullscreenBusy {
		return false
	}
	need := autoIdleDisconnected
	if connected {
		need = autoIdleConnected
	}
	return idle >= need
}

// autoFailedRecently — эту версию уже пробовали ставить сами и не вышло.
func autoFailedRecently(failVersion string, failAt int64, version string, now time.Time) bool {
	if failVersion == "" || failVersion != version {
		return false
	}
	t := time.Unix(failAt, 0)
	return now.After(t) && now.Sub(t) < autoFailBackoff
}

// autoLoop — раз в минуту: есть найденная новая версия и можно ли ставить.
func (u *updater) autoLoop() {
	if !autoInstallSupported {
		return
	}
	time.Sleep(2 * time.Minute) // после старта — не сразу
	for {
		u.autoTick()
		time.Sleep(autoPoll)
	}
}

func (u *updater) autoTick() {
	u.mu.Lock()
	if u.busy || u.stage != updAvailable || u.found == nil {
		u.mu.Unlock()
		return
	}
	version := u.found.Version
	u.mu.Unlock()

	st := loadSettings()
	if autoFailedRecently(st.AutoUpdateFailVersion, st.AutoUpdateFailAt, version, time.Now()) {
		return
	}
	if !autoInstallPossible() {
		return
	}
	u.a.state.mu.Lock()
	connected := u.a.state.kind == uiConnected || u.a.state.kind == uiConnecting
	u.a.state.mu.Unlock()
	if !autoInstallAllowed(connected, userIdleFor(), userFullscreenBusy()) {
		return
	}

	// Занять updater (как start): ручное нажатие в это время ничего не запустит.
	u.mu.Lock()
	if u.busy || u.stage != updAvailable || u.found == nil || u.found.Version != version {
		u.mu.Unlock()
		return
	}
	u.busy = true
	u.mu.Unlock()

	log.Printf("обновление: человека нет за компьютером, ставлю %s само (подключён: %v)", version, connected)
	started, err := autoInstallViaService(u.a, version, connected)

	u.mu.Lock()
	u.busy = false
	u.mu.Unlock()
	switch {
	case err != nil:
		log.Printf("обновление: само поставить не вышло (%v); строка и кнопка остаются", err)
		updateSettings(func(s *settings) { s.AutoUpdateFailVersion, s.AutoUpdateFailAt = version, time.Now().Unix() })
	case !started:
		log.Printf("обновление: служба не нашла новее, чем у нас (%s)", appVersion)
	}
	// started == true: интерфейс уже завершается (autoInstallViaService).
}
