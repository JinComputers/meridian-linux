// dion.go — местное хранение списка комнат DION (запасной релей рядом с
// VK, решение владельца 28.09.2026 через кота 1). Разбор/нормализация —
// core.ParseDionRooms/DionRoomURL (кот 5); здесь только путь к файлу и
// чтение/запись, тот же приём, что у vk-links.txt (lists.go/main.go).
package main

import (
	"path/filepath"
	"strings"
)

func dionRoomsPath() string { return filepath.Join(appSupportDir(), "dion-rooms.txt") }

// dionRooms — сохранённые комнаты, канонической ссылкой (core.DionRoomURL),
// в порядке ввода.
func dionRooms() []string {
	var out []string
	for _, l := range strings.Split(readFile(dionRoomsPath()), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func saveDionRooms(rooms []string) {
	writeFile(dionRoomsPath(), strings.Join(rooms, "\n")+"\n")
}
