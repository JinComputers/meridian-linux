package core

import (
	"reflect"
	"testing"
)

func TestParseDirectDomains(t *testing.T) {
	in := `
https://2ip.ru/some/path?x=1
VK.com.
*.gosuslugi.ru
госуслуги.рф
yandex.ru:443
vk.com
192.168.0.1
91.108.4.7/22
2001:db8::/32
2001:db8::1
localhost
плохой домен
`
	want := []string{
		"2ip.ru",
		"vk.com",
		"gosuslugi.ru",
		"xn--c1aapkosapc.xn--p1ai",
		"yandex.ru",
		"192.168.0.1",
		"91.108.4.0/22",
	}
	got := ParseDirectDomains(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDirectDomains =\n %q\nхотели\n %q", got, want)
	}
}

func TestParseDirectDomainsEmpty(t *testing.T) {
	if got := ParseDirectDomains("  \n ,; "); len(got) != 0 {
		t.Fatalf("пустой ввод дал %q", got)
	}
}
