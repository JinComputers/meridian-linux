// dioncheck.go — проверка комнат DION: живая или нет. Тот же приём, что
// у vkcheck.go (кот 3, HashScreen.kt): полный проход, то есть настоящий
// расход попытки, по одной с паузой между комнатами. Проверка —
// engine.CheckDionRoom (кот 3, тег engine-v1.9.0, 29.09.2026). Protector
// nil — как у VK: с экрана проверяют, когда туннель погашен.
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"meridian/engine"

	"github.com/JinComputers/meridian-desktop/core"
)

const (
	dionThrottleMin = 2 * time.Second
	dionThrottleMax = 4 * time.Second
)

func dionStatusPath() string { return filepath.Join(appSupportDir(), "dion-status.json") }

type dionRoomStatus struct {
	S  string `json:"s"`  // живая / мёртвая: комнаты нет или она закрыта / не удалось проверить (сеть или таймаут) — ссылка может быть живой / ошибка: … / капча
	At int64  `json:"at"` // unix-с
}

var dionStatusMu sync.Mutex

func loadDionStatuses() map[string]dionRoomStatus {
	dionStatusMu.Lock()
	defer dionStatusMu.Unlock()
	m := map[string]dionRoomStatus{}
	_ = json.Unmarshal([]byte(readFile(dionStatusPath())), &m)
	return m
}

func setDionStatus(slug, s string) {
	dionStatusMu.Lock()
	defer dionStatusMu.Unlock()
	m := map[string]dionRoomStatus{}
	_ = json.Unmarshal([]byte(readFile(dionStatusPath())), &m)
	m[slug] = dionRoomStatus{S: s, At: time.Now().Unix()}
	// Статусы удалённых комнат не копим.
	keep := map[string]bool{}
	for _, u := range dionRooms() {
		if s, bad := core.ParseDionRooms(u); len(bad) == 0 && len(s) == 1 {
			keep[s[0]] = true
		}
	}
	for k := range m {
		if !keep[k] {
			delete(m, k)
		}
	}
	data, _ := json.Marshal(m)
	writeFile(dionStatusPath(), string(data))
}

// dionStatusTexts — строка статуса на каждую сохранённую комнату (по
// порядку rooms, канонические ссылки).
func dionStatusTexts(rooms []string) []string {
	st := loadDionStatuses()
	out := make([]string, len(rooms))
	for i, u := range rooms {
		slugs, _ := core.ParseDionRooms(u)
		if len(slugs) != 1 {
			out[i] = "не проверялась"
			continue
		}
		s, ok := st[slugs[0]]
		switch {
		case !ok || s.S == "":
			out[i] = "не проверялась"
		default:
			out[i] = s.S + " · " + time.Unix(s.At, 0).Format("02.01 15:04")
		}
	}
	return out
}

// checkDionRoomFn — engine.CheckDionRoom (кот 3, engine-v1.9.0): полный
// проход до выдачи TURN, сразу выход. «живая» / «мёртвая» / «сеть» /
// «ошибка: …» (терпкий код, см. dionDisplayStatus). Protector nil — см.
// шапку файла.
var checkDionRoomFn = func(slug string) string {
	return engine.CheckDionRoom(slug, nil, engineLog{})
}

// dionDisplayStatus — код движка → подпись человеку (договор кота 2,
// экран «Обход белых списков»).
func dionDisplayStatus(raw string) string {
	switch raw {
	case "живая":
		return raw
	case "мёртвая":
		return "мёртвая: комнаты нет или она закрыта"
	case "сеть":
		return "не удалось проверить (сеть или таймаут) — ссылка может быть живой"
	}
	return raw // "ошибка: …" — как есть, текст уже человеческий
}

type dionChecker struct {
	mu      sync.Mutex
	busy    bool
	current string
	note    string
}

func (a *app) dionNote(s string) {
	a.dion.mu.Lock()
	a.dion.note = s
	a.dion.mu.Unlock()
	a.applyLocal()
}

// checkDion — проверить комнаты (slugs пусто — все сохранённые) по очереди.
func (a *app) checkDion(slugs []string) {
	a.dion.mu.Lock()
	if a.dion.busy {
		a.dion.mu.Unlock()
		return
	}
	a.dion.busy = true
	a.dion.mu.Unlock()
	defer func() {
		a.dion.mu.Lock()
		a.dion.busy, a.dion.current = false, ""
		a.dion.mu.Unlock()
		a.applyLocal()
	}()

	if len(slugs) == 0 {
		for _, u := range dionRooms() {
			if s, _ := core.ParseDionRooms(u); len(s) == 1 {
				slugs = append(slugs, s[0])
			}
		}
	}
	if len(slugs) == 0 {
		a.dionNote("Комнат нет — вставьте ссылку на комнату DION.")
		return
	}
	alive := 0
	for i, slug := range slugs {
		a.dion.mu.Lock()
		a.dion.current = slug
		a.dion.mu.Unlock()
		if len(slugs) > 1 {
			a.dionNote(fmt.Sprintf("Проверяю… %d из %d", i+1, len(slugs)))
		} else {
			a.dionNote("Проверяю…")
		}
		raw := checkDionRoomFn(slug)
		if raw == "живая" {
			alive++
		}
		setDionStatus(slug, dionDisplayStatus(raw))
		if i < len(slugs)-1 {
			pause := dionThrottleMin + time.Duration(rand.Int63n(int64(dionThrottleMax-dionThrottleMin)))
			time.Sleep(pause)
		}
	}
	if len(slugs) > 1 {
		a.dionNote(fmt.Sprintf("готово: живых %d из %d", alive, len(slugs)))
	} else {
		a.dionNote("")
	}
}

// dionSnapshot — поля для события "state".
func (a *app) dionSnapshot(m map[string]any, rooms []string) {
	a.dion.mu.Lock()
	busy, cur, note := a.dion.busy, a.dion.current, a.dion.note
	a.dion.mu.Unlock()
	texts := dionStatusTexts(rooms)
	for i, u := range rooms {
		slugs, _ := core.ParseDionRooms(u)
		if busy && len(slugs) == 1 && slugs[0] == cur {
			texts[i] = "проверяется…"
		}
	}
	m["dion_rooms"] = rooms
	m["dion_status"] = texts
	m["dion_busy"] = busy
	m["dion_note"] = note
	m["dion_all_dead"] = dionAllDead(texts)
}

// connectRelayPrimary — что уходит в core на подключении: DION основным
// только если выбрано DION, комнаты есть и хотя бы одна подтверждена
// «живая» (договор кота 3, engine-v1.10.0; комната могла умереть уже
// после выбора). Иначе VK.
func connectRelayPrimary(want string, slugs, statusTexts []string) string {
	if core.EffectiveRelayPrimary(want, slugs) != core.RelayPrimaryDion {
		return core.RelayPrimaryVK
	}
	for _, t := range statusTexts {
		if strings.HasPrefix(t, "живая") {
			return core.RelayPrimaryDion
		}
	}
	return core.RelayPrimaryVK
}

// dionAllDead — «все сохранённые мёртвые» — только по вердикту «мёртвая»,
// не по сетевой ошибке (договор кота 2): DION нельзя делать основным,
// пока хоть одна комната не подтверждена живой или хотя бы не
// проверялась. Пустой список — не «все мёртвые», а «выключено» (другая
// подпись, решает фронтенд по dion_rooms).
func dionAllDead(texts []string) bool {
	if len(texts) == 0 {
		return false
	}
	const dead = "мёртвая"
	for _, t := range texts {
		if !strings.HasPrefix(t, dead) {
			return false
		}
	}
	return true
}
