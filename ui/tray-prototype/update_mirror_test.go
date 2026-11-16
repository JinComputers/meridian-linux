package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"meridian/engine/params"
)

func TestMirrorFileName(t *testing.T) {
	ok := map[string]string{
		"https://github.com/o/r/releases/download/v0.5.28/meridian-v0.5.28-windows-amd64.exe":    "meridian-v0.5.28-windows-amd64.exe",
		"https://github.com/o/r/releases/download/v0.5.28/meridian-v0.5.28-darwin-arm64.dmg?x=1": "meridian-v0.5.28-darwin-arm64.dmg",
	}
	for in, want := range ok {
		if got := mirrorFileName(in); got != want {
			t.Errorf("mirrorFileName(%q) = %q, хотели %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "https://x/", "https://x/..", "https://x/a/%2e%2e", "https://x/a b.exe", "https://x/..evil", "https://x/-rf", "https://x/a..b"} {
		if got := mirrorFileName(bad); got != "" {
			t.Errorf("mirrorFileName(%q) = %q, ждали пусто", bad, got)
		}
	}
}

func startMirror(t *testing.T, h http.Handler) (mirrorAddr, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return mirrorAddr{host: host, port: port, pinned: true}, params.SPKIPin(srv.Certificate())
}

func withMirror(t *testing.T, a mirrorAddr, pin string) {
	t.Helper()
	oldM, oldP := updateMirrors, mirrorPins
	updateMirrors, mirrorPins = []mirrorAddr{a}, []string{pin}
	t.Cleanup(func() { updateMirrors, mirrorPins = oldM, oldP })
}

const mirrorManifest = `{"version":"0.5.28","url":"https://github.com/o/r/releases/download/v0.5.28/meridian-v0.5.28-windows-amd64.exe","sha256":"` +
	"1be10a1c5f91b66218b85a2ca913c9883f0fd1c8f7cb72ca561196c3cd3bd8aa" + `","size":9666372}`

func TestFetchMirrorManifestPinned(t *testing.T) {
	a, pin := startMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/update/"+updatePlatform+"/latest.json" {
			io.WriteString(w, mirrorManifest)
			return
		}
		http.NotFound(w, r)
	}))
	withMirror(t, a, pin)
	m, err := fetchMirrorManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.5.28" || m.Size != 9666372 {
		t.Fatalf("манифест разобран неверно: %+v", m)
	}
}

func TestMirrorWrongPinRejected(t *testing.T) {
	a, _ := startMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, mirrorManifest) }))
	withMirror(t, a, params.PinMain) // пин другого сервера
	if _, err := fetchMirrorManifest(context.Background()); err == nil {
		t.Fatal("чужой сертификат принят")
	}
}

func TestMirrorUnpinnedRequiresValidChain(t *testing.T) {
	a, pin := startMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, mirrorManifest) }))
	a.pinned = false // самоподписанный без пина должен не пройти обычную проверку
	withMirror(t, a, pin)
	if _, err := fetchMirrorManifest(context.Background()); err == nil {
		t.Fatal("самоподписанный сертификат принят без пина")
	}
}

func TestMirror404AndRedirectFallBack(t *testing.T) {
	a, pin := startMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/redir") {
			http.Redirect(w, r, "https://github.com/x", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	withMirror(t, a, pin)
	if _, err := fetchMirrorManifest(context.Background()); err == nil {
		t.Fatal("404 принят за успех")
	}
	if resp, err := mirrorGet(context.Background(), "redir"); err == nil {
		resp.Body.Close()
		t.Fatal("редирект принят: уводит мимо пина")
	}
}

func TestMirrorDownloadByNameFromURL(t *testing.T) {
	a, pin := startMirror(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/update/"+updatePlatform+"/meridian-v0.5.28-windows-amd64.exe" {
			io.WriteString(w, "FILEBODY")
			return
		}
		http.NotFound(w, r)
	}))
	withMirror(t, a, pin)
	m := &updateManifest{URL: "https://github.com/o/r/releases/download/v0.5.28/meridian-v0.5.28-windows-amd64.exe"}
	resp := mirrorDownload(context.Background(), m)
	if resp == nil {
		t.Fatal("файл с зеркала не получен")
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); string(b) != "FILEBODY" {
		t.Fatalf("тело = %q", b)
	}
	m.URL = "https://github.com/o/r/releases/download/v0.5.28/other.exe"
	if mirrorDownload(context.Background(), m) != nil {
		t.Fatal("чужое имя: зеркало должно ответить 404 и вернуть nil")
	}
}

func withTunnel(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := tunnelAppsBase
	tunnelAppsBase = srv.URL + "/apps/" + updatePlatform + "/"
	t.Cleanup(func() { tunnelAppsBase = old })
	return srv.URL
}

const tunnelSHA = "1be10a1c5f91b66218b85a2ca913c9883f0fd1c8f7cb72ca561196c3cd3bd8aa"

func TestTunnelManifestAndDownload(t *testing.T) {
	withTunnel(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/apps/" + updatePlatform + "/latest.json":
			io.WriteString(w, `{"version":"0.5.28","build":528,"file":"MeridianSetup-0.5.28.exe","sha256":"`+strings.ToUpper(tunnelSHA)+`","size":8}`)
		case "/apps/" + updatePlatform + "/MeridianSetup-0.5.28.exe":
			io.WriteString(w, "FILEBODY")
		default:
			http.NotFound(w, r)
		}
	}))
	m, err := fetchTunnelManifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !m.viaTunnel || m.Version != "0.5.28" || m.SHA256 != tunnelSHA || m.Size != 8 ||
		!strings.HasSuffix(m.URL, "/MeridianSetup-0.5.28.exe") {
		t.Fatalf("описание разобрано неверно: %+v", m)
	}
	resp := tunnelDownload(context.Background(), m)
	if resp == nil {
		t.Fatal("файл через туннель не получен")
	}
	defer resp.Body.Close()
	if b, _ := io.ReadAll(resp.Body); string(b) != "FILEBODY" {
		t.Fatalf("тело = %q", b)
	}
	m.URL = "https://evil.example/MeridianSetup-0.5.28.exe"
	if tunnelDownload(context.Background(), m) != nil {
		t.Fatal("скачивание вне базового пути туннеля допущено")
	}
}

func TestTunnelManifestRejectsBad(t *testing.T) {
	bodies := map[string]string{
		"путь в имени":   `{"version":"0.5.28","file":"../x.exe","sha256":"` + tunnelSHA + `","size":8}`,
		"плохая версия":  `{"version":"x","file":"a.exe","sha256":"` + tunnelSHA + `","size":8}`,
		"короткий sha":   `{"version":"0.5.28","file":"a.exe","sha256":"abc","size":8}`,
		"нулевой размер": `{"version":"0.5.28","file":"a.exe","sha256":"` + tunnelSHA + `","size":0}`,
	}
	for name, body := range bodies {
		body := body
		withTunnel(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		if _, err := fetchTunnelManifest(context.Background()); err == nil {
			t.Errorf("%s: принято", name)
		}
	}
}
