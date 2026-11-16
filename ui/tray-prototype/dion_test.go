package main

import "testing"

func TestConnectRelayPrimary(t *testing.T) {
	live := []string{"живая · 01.01 00:00"}
	cases := []struct {
		name  string
		want  string
		slugs []string
		texts []string
		res   string
	}{
		{"VK выбран", "vk", []string{"a"}, live, "vk"},
		{"DION и живая", "dion", []string{"a"}, live, "dion"},
		{"DION, комнат нет", "dion", nil, nil, "vk"},
		{"DION, все мёртвые", "dion", []string{"a"}, []string{"мёртвая: комнаты нет или она закрыта"}, "vk"},
		{"DION, не проверялась", "dion", []string{"a"}, []string{"не проверялась"}, "vk"},
		{"DION, одна из двух живая", "dion", []string{"a", "b"}, []string{"мёртвая: x", "живая · 01.01 00:00"}, "dion"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := connectRelayPrimary(c.want, c.slugs, c.texts); got != c.res {
				t.Errorf("got %q, want %q", got, c.res)
			}
		})
	}
}

func TestDionAllDead(t *testing.T) {
	cases := []struct {
		name  string
		texts []string
		want  bool
	}{
		{"пусто — выключено, не «все мёртвые»", nil, false},
		{"одна живая", []string{"живая · 01.01 00:00"}, false},
		{"одна мёртвая", []string{"мёртвая: комнаты нет или она закрыта"}, true},
		{"мёртвая + не проверялась — не всё мёртвое", []string{"мёртвая: комнаты нет или она закрыта", "не проверялась"}, false},
		{"мёртвая + сетевая ошибка — не всё мёртвое (сеть не приговор)",
			[]string{"мёртвая: комнаты нет или она закрыта", "не удалось проверить (сеть или таймаут) — ссылка может быть живой"}, false},
		{"все мёртвые", []string{"мёртвая: комнаты нет или она закрыта", "мёртвая: комнаты нет или она закрыта"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dionAllDead(c.texts); got != c.want {
				t.Errorf("dionAllDead(%v) = %v, хотели %v", c.texts, got, c.want)
			}
		})
	}
}
