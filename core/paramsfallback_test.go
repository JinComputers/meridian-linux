package core

import (
	"errors"
	"testing"
)

func TestBogusAPIIP(t *testing.T) {
	for _, ip := range []string{"198.18.0.7", "198.19.255.1", "64:ff9b::8af9:f659", "127.0.0.1", "::1", "10.0.0.5", "192.168.1.1", "172.20.151.75", "0.0.0.0", "169.254.1.1", "", "не-адрес", "::ffff:198.18.1.1"} {
		if !bogusAPIIP(ip) {
			t.Errorf("%q должен считаться подменным", ip)
		}
	}
	for _, ip := range []string{"138.249.246.89", "62.76.231.231", "193.47.42.65", "2a03:d000:321c::1"} {
		if bogusAPIIP(ip) {
			t.Errorf("%q — настоящий адрес, не подмена", ip)
		}
	}
}

type lookupProtector struct {
	ips []string
	err error
}

func (lookupProtector) Protect(int32) bool                    { return true }
func (l lookupProtector) LookupHost(string) ([]string, error) { return l.ips, l.err }

// DNS отдал только подмену (198.18/15, NAT64) — берём вшитые IP, а не подмену.
func TestAPIProtectorReplacesFakeDNS(t *testing.T) {
	p := &apiProtector{inner: lookupProtector{ips: []string{"198.18.3.4", "64:ff9b::1"}}}
	got, err := p.LookupHost("cdn.jincomputers.win")
	if err != nil || len(got) != 1 || got[0] != "138.249.246.89" {
		t.Fatalf("получили %v, %v; ждали вшитый 138.249.246.89", got, err)
	}
	// Смешанный ответ: подмена выбрасывается, настоящий остаётся.
	p = &apiProtector{inner: lookupProtector{ips: []string{"198.18.3.4", "138.249.246.89", "10.1.1.1"}}}
	got, _ = p.LookupHost("jinelectronics.ru")
	if len(got) != 1 || got[0] != "138.249.246.89" {
		t.Fatalf("смешанный ответ: %v", got)
	}
	// DNS не ответил — тоже вшитые.
	p = &apiProtector{inner: lookupProtector{err: errors.New("no such host")}}
	got, err = p.LookupHost("cdn.jincomputers.win")
	if err != nil || len(got) != 1 {
		t.Fatalf("при ошибке DNS ждали вшитый адрес: %v, %v", got, err)
	}
	// Неизвестное имя без вшитого адреса — ошибка, а не подмена.
	p = &apiProtector{inner: lookupProtector{ips: []string{"198.18.3.4"}}}
	if got, err := p.LookupHost("example.org"); err == nil {
		t.Fatalf("ждали ошибку, получили %v", got)
	}
}

// Запасной путь не обращается к DNS вовсе.
func TestAPIProtectorFixedNoDNS(t *testing.T) {
	p := &apiProtector{inner: lookupProtector{err: errors.New("не должно вызываться")}, fixed: []string{"62.76.231.231"}}
	got, err := p.LookupHost("cdn.jincomputers.win")
	if err != nil || len(got) != 1 || got[0] != "62.76.231.231" {
		t.Fatalf("получили %v, %v", got, err)
	}
	// Без платформенного Protector сокеты просто не защищаются, но работают.
	if !(&apiProtector{}).Protect(7) {
		t.Error("пустой inner: Protect должен вернуть true")
	}
}

// 3 основных пути + 2 запасных по IP; при заданных адресах запасные не добавляются.
func TestNewParamsRaceWithFallbackCount(t *testing.T) {
	if n := len(NewParamsRaceWithFallback(ParamsConfig{}).Fallbacks); n != 2 {
		t.Fatalf("expected two reserve paths, got %d", n)
	}
	if n := len(NewParamsRaceWithFallback(ParamsConfig{}).Fetchers); n != 3+len(apiFallbacks) {
		t.Fatalf("путей %d, ждали %d", n, 3+len(apiFallbacks))
	}
	if len(apiFallbacks) != 2 || apiFallbacks[0].ip != "62.76.231.231" || apiFallbacks[0].port != 8443 ||
		apiFallbacks[1].ip != "193.47.42.65" || apiFallbacks[1].port != 8443 {
		t.Fatalf("порядок запасных путей не как согласован: %+v", apiFallbacks)
	}
}
