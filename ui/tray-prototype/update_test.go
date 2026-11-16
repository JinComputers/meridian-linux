package main

import "testing"

func TestVersionNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.4.4", "0.4.3", true},
		{"0.4.3", "0.4.3", false},
		{"0.4.3", "0.4.4", false},
		{"0.10.0", "0.9.9", true},
		{"v1.0.0", "0.99.99", true},
		{"1.0.0-beta", "0.9.0", true},
		{"мусор", "0.1.0", false},
		{"0.4", "0.3.0", false},
	} {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestManifestValidate(t *testing.T) {
	ok := updateManifest{Version: "0.4.4", URL: "https://github.com/x/y/releases/download/v0.4.4/MeridianVPN-Setup-0.4.4.exe",
		SHA256: "245810a709d5da3147dff597bd9b5911d0ac50366d195059a871c8cd8ca76a5e", Size: 9 << 20}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	bad := []func(m *updateManifest){
		func(m *updateManifest) { m.URL = "http://example.com/x.exe" },
		func(m *updateManifest) { m.URL = "file:///C:/x.exe" },
		func(m *updateManifest) { m.SHA256 = "abc" },
		func(m *updateManifest) { m.Size = 0 },
		func(m *updateManifest) { m.Version = "latest" },
	}
	for i, f := range bad {
		m := ok
		f(&m)
		if m.validate() == nil {
			t.Errorf("случай %d: принято, должно быть отвергнуто", i)
		}
	}
	loop := ok
	loop.URL = "http://127.0.0.1:8765/MeridianVPN-Setup.exe"
	if err := loop.validate(); err != nil {
		t.Errorf("стенд на 127.0.0.1: %v", err)
	}
}

func TestParseAfterUpdate(t *testing.T) {
	r := parseAfterUpdate([]string{"--updated=0.4.5", "--reconnect"})
	if r.message != "" || !r.reconnect { // об удачном обновлении молчим
		t.Fatalf("%+v", r)
	}
	if r := parseAfterUpdate([]string{"--update-failed=1223"}); r.message != "Обновление отменено" || r.reconnect {
		t.Fatalf("%+v", r)
	}
}
