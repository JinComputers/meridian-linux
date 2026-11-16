package core

import (
	"errors"
	"net"
	"net/netip"
	"time"

	"meridian/engine/params"
)

// Запасные пути к API (28.10.2026, просьба Кота 1 по слову владельца: у
// пользователя 0.5.66 «Служба недоступна» — выход 138.249.246.89 закрыт
// напрямую, DNS отдаёт 198.18.x.x и 64:ff9b::). Движок params не трогаем (это
// код Кота 3, решение владельца 24.09): все пути строятся поверх готового
// params.Client, а подмена адресов — через необязательный LookupHost у
// Protector, который клиент уже умеет звать (resolvingDial).
//
// Доверие везде одно: настоящий сертификат Let's Encrypt для
// cdn.jincomputers.win (Pinned:false, системная проверка цепочки И ИМЕНИ).
// Имя в URL остаётся cdn.jincomputers.win, поэтому SNI и Host правильные;
// меняется только IP, на который открывается сокет. Голого IP в URL нет, а
// значит нет и ветки «принять любой сертификат».

const apiTLSName = "cdn.jincomputers.win"

// apiFallbacks — порядок как у Кота 1: вход 1 :8443, вход 5 :8443. На :443
// входа 1 не API, а проброс туннеля (его сертификат для cdn не годится) — убран.
var apiFallbacks = []struct {
	ip   string
	port int
}{
	{"62.76.231.231", 8443},
	{"193.47.42.65", 8443},
}

// builtinAPIIPs — вшитые адреса основных имён: берутся, когда DNS отдал только
// подмену (или не ответил).
var builtinAPIIPs = map[string][]string{
	"jinelectronics.ru": {"138.249.246.89"},
	apiTLSName:          {"138.249.246.89"},
}

var (
	fakeIPv4Net = netip.MustParsePrefix("198.18.0.0/15") // fake-ip прокси/VPN на роутере
	nat64Net    = netip.MustParsePrefix("64:ff9b::/96")  // NAT64/DNS64
)

// bogusAPIIP — ответ DNS, которому верить нельзя: не разбирается, loopback,
// частный, link-local, multicast, 0.0.0.0, 198.18.0.0/15, 64:ff9b::/96.
func bogusAPIIP(s string) bool {
	a, err := netip.ParseAddr(s)
	if err != nil {
		return true
	}
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() || a.IsLinkLocalUnicast() || a.IsMulticast() {
		return true
	}
	return fakeIPv4Net.Contains(a) || nat64Net.Contains(a)
}

// apiProtector — обёртка над платформенным Protector для одного пути к API:
// пробрасывает Protect и ExcludeHost, а LookupHost отдаёт адреса сам.
type apiProtector struct {
	inner params.Protector // nil — сокеты без защиты (туннеля ещё нет)
	fixed []string         // не пусто — запасной путь: только эти IP, без DNS
}

func (p *apiProtector) Protect(fd int32) bool {
	if p.inner == nil {
		return true
	}
	return p.inner.Protect(fd)
}

func (p *apiProtector) ExcludeHost(host string) {
	if ex, ok := p.inner.(interface{ ExcludeHost(string) }); ok {
		ex.ExcludeHost(host)
	}
}

func (p *apiProtector) LookupHost(name string) ([]string, error) {
	if len(p.fixed) > 0 {
		return p.fixed, nil
	}
	var ips []string
	var err error
	if hr, ok := p.inner.(interface {
		LookupHost(string) ([]string, error)
	}); ok {
		ips, err = hr.LookupHost(name)
	} else {
		ips, err = net.LookupHost(name)
	}
	good := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !bogusAPIIP(ip) {
			good = append(good, ip)
		}
	}
	if len(good) > 0 {
		return good, nil
	}
	if b := builtinAPIIPs[name]; len(b) > 0 {
		return b, nil
	}
	if err == nil {
		err = errors.New("DNS вернул только подменные адреса")
	}
	return nil, err
}

// NewParamsRaceWithFallback — как NewParamsRace, но каждый основной путь
// защищён от подмены DNS (подменные ответы отбрасываются, берутся вшитые IP),
// а к нему добавлены запасные пути по IP входов без DNS. Запасные добавляются
// только при адресах по умолчанию (cfg.Addresses пуст). «Недоступно»
// возвращается, лишь когда молчат ВСЕ пути.
func NewParamsRaceWithFallback(cfg ParamsConfig) *ParamsRace {
	addrs := cfg.Addresses
	withFallback := len(addrs) == 0
	if withFallback {
		addrs = params.DefaultAddresses()
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 2500 * time.Millisecond
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 2500 * time.Millisecond
	}
	if cfg.WalkBudget == 0 {
		cfg.WalkBudget = paramsRaceBudget
	}
	inner := cfg.Protector
	r := &ParamsRace{}
	for _, a := range addrs {
		c := cfg
		c.Addresses = []params.Address{a}
		c.Protector = &apiProtector{inner: inner}
		if (a.Host == "api-ru.meridianvpn.org" || a.Host == "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev") && a.Port == 443 && !a.Pinned {
			r.Fallbacks = append(r.Fallbacks, params.New(c))
		} else {
			r.Fetchers = append(r.Fetchers, params.New(c))
		}
	}
	if withFallback {
		for _, fb := range apiFallbacks {
			c := cfg
			c.Addresses = []params.Address{{Host: apiTLSName, Port: fb.port, Pinned: false}}
			c.Protector = &apiProtector{inner: inner, fixed: []string{fb.ip}}
			r.Fetchers = append(r.Fetchers, params.New(c))
		}
	}
	return r
}
