package engine

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVKPostAdvertisesSupportedEncodingAndDecodes(t *testing.T) {
	for _, encoding := range []string{"", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPost || req.Header.Get("Accept-Encoding") != "gzip" {
					t.Errorf("unexpected method or advertised compression")
				}
				if encoding == "gzip" {
					w.Header().Set("Content-Encoding", "gzip")
					z := gzip.NewWriter(w)
					_, _ = z.Write([]byte(`{"ok":true}`))
					_ = z.Close()
					return
				}
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))
			defer server.Close()
			c := &vkChain{s: &session{}, client: server.Client()}
			got, err := c.post(context.Background(), nil, server.URL)
			if err != nil || got["ok"] != true {
				t.Fatalf("post failed: %v", err)
			}
		})
	}
}

func TestVKReadBodyRejectsUnrequestedEncodingAndLimitsOutput(t *testing.T) {
	for _, enc := range []string{"br", "zstd", "deflate"} {
		resp := &http.Response{Header: http.Header{"Content-Encoding": []string{enc}}, Body: io.NopCloser(strings.NewReader("invalid"))}
		if _, err := readBody(resp); err == nil {
			t.Fatalf("accepted %s", enc)
		}
	}
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	_, _ = z.Write(bytes.Repeat([]byte("x"), (1<<20)+1))
	_ = z.Close()
	resp := &http.Response{Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(&b)}
	got, err := readBody(resp)
	if err != nil || len(got) != 1<<20 {
		t.Fatalf("decompressed size %d, error %v", len(got), err)
	}
}
