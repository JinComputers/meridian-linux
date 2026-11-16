// vkcheck.go — проверка ссылок на звонки ВК: живая или нет.
//
// Как на Android (HashScreen.kt checkAll, кот 3): engine.CheckHash — ПОЛНЫЙ
// проход цепочки VK, то есть настоящий расход ссылки; по одной, с паузой
// 3–6 с между ссылками (то же ограничение VK, что в движке между добычей
// кредов). Protector nil — запросы идут обычным путём этого процесса
// (интерфейс не сидит в туннеле: чёрный режим пускает в туннель только
// список, белый — ВК и так напрямую). Решателя капчи на компьютере нет —
// капча даёт статус «капча».
//
// Отличие от Android по просьбе владельца (25.09): только что добавленная
// ссылка проверяется сразу.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"meridian/engine"

	"github.com/JinComputers/meridian-desktop/core"
)

const (
	vkThrottleMin = 3 * time.Second
	vkThrottleMax = 6 * time.Second
)

func vkStatusPath() string { return filepath.Join(appSupportDir(), "vk-status.json") }

type vkStatus struct {
	S  string `json:"s"`  // живой / мёртвый / капча / релея нет / сбой… / ошибка: …
	At int64  `json:"at"` // unix-с
}

var vkStatusMu sync.Mutex

func loadVKStatuses() map[string]vkStatus {
	vkStatusMu.Lock()
	defer vkStatusMu.Unlock()
	m := map[string]vkStatus{}
	_ = json.Unmarshal([]byte(readFile(vkStatusPath())), &m)
	return m
}

func setVKStatus(hash, s string) {
	vkStatusMu.Lock()
	defer vkStatusMu.Unlock()
	m := map[string]vkStatus{}
	_ = json.Unmarshal([]byte(readFile(vkStatusPath())), &m)
	m[hash] = vkStatus{S: s, At: time.Now().Unix()}
	// Статусы удалённых ссылок не копим.
	keep := map[string]bool{}
	for _, l := range vkLinks() {
		keep[core.NormalizeHash(l)] = true
	}
	for h := range m {
		if !keep[h] {
			delete(m, h)
		}
	}
	data, _ := json.Marshal(m)
	writeFile(vkStatusPath(), string(data))
}

// vkStatusTexts — строка статуса для каждой ссылки списка (по порядку).
func vkStatusTexts(links []string) []string {
	st := loadVKStatuses()
	out := make([]string, len(links))
	for i, l := range links {
		s, ok := st[core.NormalizeHash(l)]
		switch {
		case !ok || s.S == "":
			out[i] = "не проверялась"
		default:
			out[i] = s.S + " · " + time.Unix(s.At, 0).Format("02.01 15:04")
		}
	}
	return out
}

type engineLog struct{}

func (engineLog) Log(line string) { log.Println(line) }

// vkChecker — одна проверка за раз (ограничение VK общее на всё
// приложение).
type vkChecker struct {
	mu      sync.Mutex
	busy    bool
	current string // хеш, который проверяется сейчас
	note    string // строка хода: «проверяю 2 из 5…»
}

func (a *app) vkNote(s string) {
	a.vk.mu.Lock()
	a.vk.note = s
	a.vk.mu.Unlock()
	a.applyLocal()
}

// checkVK — проверить ссылки (hashes пусто — все) по очереди.
func (a *app) checkVK(hashes []string) {
	a.vk.mu.Lock()
	if a.vk.busy {
		a.vk.mu.Unlock()
		return
	}
	a.vk.busy = true
	a.vk.mu.Unlock()
	defer func() {
		a.vk.mu.Lock()
		a.vk.busy, a.vk.current = false, ""
		a.vk.mu.Unlock()
		a.applyLocal()
	}()

	if len(hashes) == 0 {
		for _, l := range vkLinks() {
			if h := core.NormalizeHash(l); h != "" {
				hashes = append(hashes, h)
			}
		}
	}
	if len(hashes) == 0 {
		a.vkNote("проверять нечего")
		return
	}
	alive := 0
	for i, h := range hashes {
		a.vk.mu.Lock()
		a.vk.current = h
		a.vk.mu.Unlock()
		if len(hashes) > 1 {
			a.vkNote(fmt.Sprintf("проверяю %d из %d, осталось %d", i+1, len(hashes), len(hashes)-i-1))
		} else {
			a.vkNote("проверяю…")
		}
		s := engine.CheckHash(h, a.deviceID, nil, nil, engineLog{})
		log.Printf("ВК: ссылка …%s — %s", tail(h, 6), s)
		if s == "живой" {
			alive++
		}
		setVKStatus(h, s)
		if i < len(hashes)-1 {
			pause := vkThrottleMin + time.Duration(rand.Int63n(int64(vkThrottleMax-vkThrottleMin)))
			a.vkNote(fmt.Sprintf("проверено %d из %d, пауза %d с", i+1, len(hashes), int(pause.Seconds())))
			time.Sleep(pause)
		}
	}
	if len(hashes) > 1 {
		a.vkNote(fmt.Sprintf("готово: живых %d из %d", alive, len(hashes)))
	} else {
		a.vkNote("")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// vkSnapshot — поля для события "state".
func (a *app) vkSnapshot(m map[string]any, links []string) {
	a.vk.mu.Lock()
	busy, cur, note := a.vk.busy, a.vk.current, a.vk.note
	a.vk.mu.Unlock()
	texts := vkStatusTexts(links)
	for i, l := range links {
		if busy && core.NormalizeHash(l) == cur {
			texts[i] = "проверяю…"
		}
	}
	m["vk_status"] = texts
	m["vk_busy"] = busy
	m["vk_note"] = note
}

// connectHashes — какие ссылки VK отдать на подключение: ссылки, которые
// проверка признала мёртвыми («Invalid join link», код 954), не пробуются
// каждый раз. Если живых не осталось, отдаём все: пусть движок сам скажет,
// что с ними, а белый режим не остаётся без ссылок вовсе.
func connectHashes(hashes []string, st map[string]vkStatus) []string {
	var live []string
	for _, h := range hashes {
		if s, ok := st[h]; ok && strings.HasPrefix(s.S, "мёртвый") {
			continue
		}
		live = append(live, h)
	}
	if len(live) == 0 {
		return hashes
	}
	return live
}
