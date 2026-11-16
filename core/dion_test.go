package core

import (
	"reflect"
	"testing"
)

func TestParseDionRooms(t *testing.T) {
	in := "https://dion.vc/event/Abc-1?x=1#y, www.dion.vc/event/def_2/\nhttp://dion.vc/event/Abc-1 ghi3,https://evil.vc/event/zzz -bad"
	slugs, bad := ParseDionRooms(in)
	if want := []string{"Abc-1", "def_2", "ghi3"}; !reflect.DeepEqual(slugs, want) {
		t.Fatalf("slugs = %v, want %v", slugs, want)
	}
	if want := []string{"https://evil.vc/event/zzz", "-bad"}; !reflect.DeepEqual(bad, want) {
		t.Fatalf("bad = %v, want %v", bad, want)
	}
}

func TestParseDionRoomsRejectsWrongPath(t *testing.T) {
	for _, s := range []string{"https://dion.vc/abc", "https://dion.vc/event/", "https://dion.vc/event/a/b", "ftp://dion.vc/event/abc"} {
		if slugs, _ := ParseDionRooms(s); len(slugs) != 0 {
			t.Fatalf("%q дал %v", s, slugs)
		}
	}
}

func TestDionRoomsForLadder(t *testing.T) {
	got := dionRoomsForLadder([]string{"a1", "https://dion.vc/event/b2", "a1", "-bad", "c3", "d4"})
	if want := []string{"a1", "b2", "c3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := dionRoomsForLadder(nil); len(got) != 0 {
		t.Fatalf("пусто дало %v", got)
	}
}

func TestDionRoomURL(t *testing.T) {
	if got := DionRoomURL("Abc-1"); got != "https://dion.vc/event/Abc-1" {
		t.Fatal(got)
	}
}

func TestEffectiveRelayPrimary(t *testing.T) {
	for _, tc := range []struct {
		primary string
		rooms   []string
		want    string
	}{
		{"", nil, RelayPrimaryVK},
		{RelayPrimaryDion, nil, RelayPrimaryVK},
		{RelayPrimaryDion, []string{"a"}, RelayPrimaryDion},
		{RelayPrimaryVK, []string{"a"}, RelayPrimaryVK},
		{"мусор", []string{"a"}, RelayPrimaryVK},
	} {
		if got := EffectiveRelayPrimary(tc.primary, tc.rooms); got != tc.want {
			t.Fatalf("%q %v: %q, want %q", tc.primary, tc.rooms, got, tc.want)
		}
	}
}

func TestParseDionRoomsCodeFromAnyDionLink(t *testing.T) {
	for in, want := range map[string]string{
		"https://dion.vc/join/abc-def-ghi":         "abc-def-ghi",
		"https://app.dion.vc/room/ABC-def-ghi?x=1": "ABC-def-ghi",
		"dion.vc/r/abc-def-ghi/":                   "abc-def-ghi",
		"https://dion.vc/event/Abc-1":              "Abc-1",
		"abc-def-ghi":                              "abc-def-ghi",
	} {
		slugs, bad := ParseDionRooms(in)
		if len(bad) != 0 || len(slugs) != 1 || slugs[0] != want {
			t.Errorf("%q: slugs=%v bad=%v, хочу %q", in, slugs, bad, want)
		}
	}
	for _, in := range []string{"https://dion.vc/join/abcd-def-ghi", "https://evil.vc/abc-def-ghi", "https://dion.vc/settings"} {
		if slugs, _ := ParseDionRooms(in); len(slugs) != 0 {
			t.Errorf("%q дал %v", in, slugs)
		}
	}
}
