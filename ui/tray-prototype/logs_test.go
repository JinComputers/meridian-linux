package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactWriterHidesKey(t *testing.T) {
	var b bytes.Buffer
	w := redactWriter{&b}
	line := "отказ: Get \"https://h:1/v1/params?device_id=d&key=SECRET123&platform=macos\": EOF\n"
	if n, err := w.Write([]byte(line)); err != nil || n != len(line) {
		t.Fatalf("Write: %d %v", n, err)
	}
	if strings.Contains(b.String(), "SECRET123") || !strings.Contains(b.String(), "key=REDACTED&platform") {
		t.Fatalf("не затёрто: %q", b.String())
	}
}
