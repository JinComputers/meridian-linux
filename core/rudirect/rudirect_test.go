package rudirect

import (
	"bytes"
	"compress/gzip"
	"net/netip"
	"testing"
)

func TestPrefixes(t *testing.T) {
	ps, err := Prefixes()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 8641 {
		t.Fatalf("блоков %d, в снимке 8641", len(ps))
	}
	has := func(a string) bool {
		ip := netip.MustParseAddr(a)
		for _, p := range ps {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	// 77.88.8.8 — DNS Яндекса (RU); 1.1.1.1 и 8.8.8.8 — DNS туннеля, в
	// российский список попадать не должны (иначе DNS ушёл бы мимо туннеля).
	if !has("77.88.8.8") {
		t.Error("77.88.8.8 (Яндекс) не в списке")
	}
	for _, a := range []string{"1.1.1.1", "8.8.8.8"} {
		if has(a) {
			t.Errorf("%s (DNS туннеля) в российском списке", a)
		}
	}
}

func TestDomains(t *testing.T) {
	if len(Domains) != 119 {
		t.Fatalf("корней %d, в списке 119", len(Domains))
	}
	if len(Exclusions) != 19 {
		t.Fatalf("исключений %d, в списке 19", len(Exclusions))
	}
	for _, d := range []string{"ya.ru", "vk.com", "mail.ru", "gosuslugi.ru"} {
		if !IsRoot(d) {
			t.Errorf("нет корня %s", d)
		}
	}
	for _, d := range []string{"habr.com", "comms.ru", "static.4pda.to", "A.Habr.com."} {
		if !IsExcluded(d) {
			t.Errorf("%s не в исключениях", d)
		}
	}
	for _, d := range []string{"ya.ru", "notcomms.ru", "ru"} {
		if IsExcluded(d) {
			t.Errorf("%s ошибочно в исключениях", d)
		}
	}
}

func TestLinesRejectsWrongSum(t *testing.T) {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte("1.2.3.0/24\n"))
	_ = w.Close()
	if _, err := lines(b.Bytes(), rangesSHA256); err == nil {
		t.Fatal("чужой текст с суммой снимка принят")
	}
	if _, err := lines([]byte("не gzip"), rangesSHA256); err == nil {
		t.Fatal("битый архив принят")
	}
}
