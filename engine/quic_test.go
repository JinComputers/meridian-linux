package engine

import (
	"encoding/binary"
	"testing"
)

// udpV4 — минимальный IPv4/UDP пакет от 10.77.77.94:50000 на 1.2.3.4:dport.
func udpV4(dport uint16, fragOff uint16) []byte {
	p := make([]byte, 20+8+16)
	p[0] = 0x45
	binary.BigEndian.PutUint16(p[2:4], uint16(len(p)))
	binary.BigEndian.PutUint16(p[6:8], fragOff)
	p[8] = 64
	p[9] = 17
	copy(p[12:16], []byte{10, 77, 77, 94})
	copy(p[16:20], []byte{1, 2, 3, 4})
	binary.BigEndian.PutUint16(p[10:12], inetChecksum(p[:20]))
	binary.BigEndian.PutUint16(p[20:22], 50000)
	binary.BigEndian.PutUint16(p[22:24], dport)
	return p
}

func TestIsQUICv4(t *testing.T) {
	if !isQUICv4(udpV4(443, 0)) {
		t.Fatal("UDP/443 не распознан")
	}
	if isQUICv4(udpV4(53, 0)) {
		t.Fatal("DNS принят за QUIC")
	}
	if isQUICv4(udpV4(443, 100)) {
		t.Fatal("фрагмент с ненулевым смещением принят за QUIC")
	}
	tcp := udpV4(443, 0)
	tcp[9] = 6
	if isQUICv4(tcp) {
		t.Fatal("TCP/443 принят за QUIC")
	}
}

func TestICMPPortUnreachable(t *testing.T) {
	orig := udpV4(443, 0)
	r := icmpPortUnreachableV4(orig)
	if r == nil {
		t.Fatal("ответ не собран")
	}
	if r[9] != 1 || r[20] != 3 || r[21] != 3 {
		t.Fatalf("не ICMP 3/3: proto=%d type=%d code=%d", r[9], r[20], r[21])
	}
	// Адреса переставлены.
	if string(r[12:16]) != string(orig[16:20]) || string(r[16:20]) != string(orig[12:16]) {
		t.Fatal("адреса не переставлены")
	}
	// Суммы сходятся: сумма по корректному заголовку даёт 0.
	if inetChecksum(r[:20]) != 0 {
		t.Fatal("контрольная сумма IP неверна")
	}
	if inetChecksum(r[20:]) != 0 {
		t.Fatal("контрольная сумма ICMP неверна")
	}
	// В теле — заголовок исходного пакета и 8 байт UDP.
	if string(r[28:28+28]) != string(orig[:28]) {
		t.Fatal("в теле ICMP не исходный заголовок")
	}
}
