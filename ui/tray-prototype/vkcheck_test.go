package main

import (
	"reflect"
	"testing"
)

func TestConnectHashesSkipsDeadOnes(t *testing.T) {
	st := map[string]vkStatus{
		"dead1": {S: "мёртвый"},
		"live1": {S: "живой"},
		"cap1":  {S: "капча"},
	}
	got := connectHashes([]string{"dead1", "live1", "cap1", "new1"}, st)
	if want := []string{"live1", "cap1", "new1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Все мёртвые — отдаём все: движок скажет сам, белый режим не пустеет.
	all := []string{"dead1", "dead2"}
	if got := connectHashes(all, map[string]vkStatus{"dead1": {S: "мёртвый · 07.10 12:00"}, "dead2": {S: "мёртвый"}}); !reflect.DeepEqual(got, all) {
		t.Fatalf("при всех мёртвых ждали все ссылки, got %v", got)
	}
	if got := connectHashes(nil, st); len(got) != 0 {
		t.Fatalf("пусто: %v", got)
	}
}
