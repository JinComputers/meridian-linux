//go:build linux

package main

import (
	"errors"
	"os/exec"
	"testing"
)

func TestClassifyKeyLookup(t *testing.T) {
	for _, tc := range []struct {
		script  string
		want    string
		wantErr error
	}{
		{"printf 'synthetic-key\\n'", "synthetic-key", nil},
		{"exit 1", "", errKeyAbsent},
		{"printf 'private error' >&2; exit 1", "", errKeyStoreUnavailable},
		{"exit 2", "", errKeyStoreUnavailable},
	} {
		out, err := exec.Command("sh", "-c", tc.script).Output()
		got, actualErr := classifyKeyLookup(out, err)
		if got != tc.want || !errors.Is(actualErr, tc.wantErr) {
			t.Fatalf("got %q, %v", got, actualErr)
		}
	}
	if _, err := classifyKeyLookup(nil, errors.New("tool missing")); !errors.Is(err, errKeyStoreUnavailable) {
		t.Fatal("missing executable must be a storage error")
	}
}
