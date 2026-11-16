package core

import (
	"encoding/binary"
	"net/netip"
	"reflect"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// dnsReply — IPv4/UDP-пакет от src:53 с ответом: q → CNAME cname → A addrs
// (как в platform/windows/tunnelonly_test.go).
func dnsReply(t *testing.T, src, q, cname string, addrs ...string) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, RCode: dnsmessage.RCodeSuccess})
	b.EnableCompression()
	_ = b.StartQuestions()
	_ = b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(q), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	_ = b.StartAnswers()
	owner := q
	if cname != "" {
		_ = b.CNAMEResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(q), Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(cname)})
		owner = cname
	}
	for _, a := range addrs {
		_ = b.AResource(dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(owner), Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.AResource{A: netip.MustParseAddr(a).As4()})
	}
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	pkt := make([]byte, 28+len(msg))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:], uint16(len(pkt)))
	pkt[8], pkt[9] = 64, 17
	s := netip.MustParseAddr(src).As4()
	copy(pkt[12:16], s[:])
	copy(pkt[16:20], []byte{10, 0, 0, 2})
	binary.BigEndian.PutUint16(pkt[20:], 53)
	binary.BigEndian.PutUint16(pkt[22:], 50000)
	binary.BigEndian.PutUint16(pkt[24:], uint16(8+len(msg)))
	copy(pkt[28:], msg)
	return pkt
}

func TestTunnelDNSAnswer(t *testing.T) {
	dns := map[netip.Addr]bool{netip.MustParseAddr("1.1.1.1"): true}
	m := NewDomainMatcher([]string{"youtube.com"})
	want := []netip.Addr{netip.MustParseAddr("142.250.1.1"), netip.MustParseAddr("142.250.1.2")}
	if got := TunnelDNSAnswer(dnsReply(t, "1.1.1.1", "www.youtube.com.", "youtube-ui.l.google.com.", "142.250.1.1", "142.250.1.2"), dns, m); !reflect.DeepEqual(got, want) {
		t.Fatalf("поддомен через CNAME: %v, хотели %v", got, want)
	}
	if got := TunnelDNSAnswer(dnsReply(t, "1.1.1.1", "example.com.", "", "93.184.216.34"), dns, m); got != nil {
		t.Fatalf("имя не из списка: %v", got)
	}
	if got := TunnelDNSAnswer(dnsReply(t, "9.9.9.9", "youtube.com.", "", "142.250.1.3"), dns, m); got != nil {
		t.Fatalf("ответ не от DNS туннеля: %v", got)
	}
	if got := TunnelDNSAnswer(dnsReply(t, "1.1.1.1", "youtube.com.", "", "142.250.1.3"), dns, NewDomainMatcher(nil)); got != nil {
		t.Fatalf("пустой список: %v", got)
	}
	if got := TunnelDNSAnswer([]byte{0x45, 0, 0}, dns, m); got != nil {
		t.Fatalf("обрывок пакета: %v", got)
	}
}

func TestTunnelOnlyRoutable(t *testing.T) {
	for s, want := range map[string]bool{"142.250.1.1": true, "10.0.0.1": false, "127.0.0.1": false, "192.168.1.1": false, "0.0.0.0": false, "224.0.0.1": false} {
		if got := TunnelOnlyRoutable(netip.MustParseAddr(s)); got != want {
			t.Errorf("%s: %v, хотели %v", s, got, want)
		}
	}
}
