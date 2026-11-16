package main

import "testing"

func TestOfflinePreflightOnlyBlocksConfirmedMissingNetwork(t *testing.T) {
	for _, tc := range []struct {
		network string
		block   bool
	}{{"нет", true}, {"", false}, {"wifi", false}, {"ethernet", false}} {
		if got := shouldBlockOfflineConnect(tc.network); got != tc.block {
			t.Fatalf("network %q blocked=%v", tc.network, got)
		}
	}
}
