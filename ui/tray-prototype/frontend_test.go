package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestFrontendScriptParses — скрипт страницы разбирается. Урок 0.4.7:
// настоящий перевод строки внутри JS-строки ломал весь <script
// type="module"> — окно мёртвое (ни планеты, ни кнопок), а Go-сборка и
// тесты зелёные. Нет node — пропуск (сборка .app проверяет сама).
func TestFrontendScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node не найден")
	}
	html, err := os.ReadFile(filepath.Join("frontend", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile(`(?s)<script type="module">(.*?)</script>`).FindAllSubmatch(html, -1)
	if len(blocks) == 0 {
		t.Fatal("в index.html нет <script type=\"module\">")
	}
	for i, b := range blocks {
		f := filepath.Join(t.TempDir(), "page.mjs")
		if err := os.WriteFile(f, b[1], 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
			t.Fatalf("скрипт %d страницы не разбирается:\n%s", i, out)
		}
	}
}

// A missing literal DOM lookup aborts initialization although the JS parses.
func TestFrontendLiteralElementIDsExist(t *testing.T) {
	html, err := os.ReadFile(filepath.Join("frontend", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bid=["']([^"']+)["']`).FindAllSubmatch(html, -1) {
		ids[string(m[1])] = true
	}
	blocks := regexp.MustCompile(`(?s)<script type="module">(.*?)</script>`).FindAllSubmatch(html, -1)
	if len(blocks) == 0 {
		t.Fatal("отсутствует скрипт интерфейса")
	}
	for _, block := range blocks {
		for _, m := range regexp.MustCompile(`\$\(['"]([^'"]+)['"]\)`).FindAllSubmatch(block[1], -1) {
			if id := string(m[1]); !ids[id] {
				t.Errorf("скрипт обращается к отсутствующему элементу %q", id)
			}
		}
	}
}
