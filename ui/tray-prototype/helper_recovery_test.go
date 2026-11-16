package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestHelperRecoveryClearsOnlyHelperError(t *testing.T) {
	s := uiState{kind: uiError, notice: "helper unavailable", noticeBad: true, helperUnavailable: true}
	s.clearHelperUnavailable()
	if s.kind != uiDisconnected || s.notice != "" || s.noticeBad || s.helperUnavailable {
		t.Fatal("helper error remained")
	}
	s = uiState{kind: uiError, notice: "connection failed", noticeBad: true}
	s.clearHelperUnavailable()
	if s.kind != uiError || s.notice != "connection failed" || !s.noticeBad {
		t.Fatal("unrelated error was erased")
	}
}

func TestHelperDialFailureCategory(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{fmt.Errorf("wrapped: %w", os.ErrNotExist), "сокет отсутствует"},
		{fmt.Errorf("wrapped: %w", os.ErrPermission), "нет доступа к сокету"},
		{fmt.Errorf("wrapped: %w", syscall.ECONNREFUSED), "сокет не принимает соединения"},
		{errors.New("private data"), "ошибка соединения или рукопожатия"},
	} {
		if got := helperDialFailureCategory(tc.err); got != tc.want {
			t.Fatalf("got %q, want %q", got, tc.want)
		}
	}
}
