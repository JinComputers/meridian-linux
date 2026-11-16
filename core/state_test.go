package core

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStoreLoadMissingIsNilNotError(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "нет-такого", "state.json")}
	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load() на отсутствующем журнале вернул ошибку: %v", err)
	}
	if st != nil {
		t.Fatalf("Load() на отсутствующем журнале вернул %+v, ожидали nil", st)
	}
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "sub", "state.json")}
	want := &State{
		PID:               1234,
		StartedAt:         time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC),
		PhysicalInterface: "en0",
		PhysicalGateway:   "192.168.1.1",
		PrimaryService:    "8F1B4DE2-0000-0000-0000-000000000000",
		OriginalDNS:       []string{"192.168.1.1"},
		UtunName:          "utun7",
		FullTunnelSet:     true,
		TunnelDNSSet:      true,
		ExceptionTargets:  []string{"203.0.113.10", "203.0.113.11"},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load() после Save() вернул nil")
	}
	if got.PID != want.PID || got.PhysicalGateway != want.PhysicalGateway ||
		got.UtunName != want.UtunName || len(got.ExceptionTargets) != len(want.ExceptionTargets) {
		t.Fatalf("Load() = %+v, хотели %+v", got, want)
	}

	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatalf("stat журнала: %v", err)
	}
	// Права 0600 (os.OpenFile в Save) — POSIX-гарантия, только доступна
	// на unix: NTFS не хранит unix rwx-биты, os.FileInfo.Mode().Perm()
	// на Windows возвращает 0666 для любого обычного файла независимо
	// от того, что было передано os.OpenFile (поймал кот 6 живым
	// прогоном go test на Windows — core/ кросс-платформенный, тест без
	// build-тега запускается и там). Реальная защита журнала на Windows
	// — ACL, не битовая маска; отдельная забота windows-стороны
	// (platform/windows), не то, что можно унифицированно проверить
	// здесь одним ассертом на оба мира.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("права журнала = %o, хотели 0600", perm)
		}
	}
}

func TestStoreSaveNoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s := &Store{Path: filepath.Join(dir, "state.json")}
	if err := s.Save(&State{PID: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(s.Path + ".tmp"); err == nil {
		t.Fatal("временный файл .tmp остался после Save()")
	}
}

func TestStoreRemoveMissingIsIdempotent(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := s.Remove(); err != nil {
		t.Fatalf("Remove() на отсутствующем журнале вернул ошибку: %v", err)
	}
}

func TestStoreRemoveAfterSave(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := s.Save(&State{PID: 1}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load после Remove: %v", err)
	}
	if st != nil {
		t.Fatalf("Load после Remove вернул %+v, ожидали nil", st)
	}
}

func TestStoreLoadToleratesUTF8BOM(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := os.WriteFile(s.Path, []byte("\xef\xbb\xbf{\"pid\":42,\"full_tunnel_set\":true}"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load с BOM: %v", err)
	}
	if st == nil || st.PID != 42 || !st.FullTunnelSet {
		t.Fatalf("Load с BOM = %+v", st)
	}
}
