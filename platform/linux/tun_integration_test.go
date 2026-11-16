//go:build linux

package linux

import (
	"encoding/binary"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

// Runs only in an isolated network namespace with CAP_NET_ADMIN and /dev/net/tun.
// It checks both the actual kernel TUN and the PacketFlow frame contract.
func TestKernelTunPacketFlow(t *testing.T) {
	if os.Getenv("MERIDIAN_ISOLATED_TUN_TEST") != "1" {
		t.Skip("requires an isolated Linux network namespace")
	}
	f, err := NewTunFlow("", 1200)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	name, err := f.Name()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"addr", "add", "10.201.77.1/24", "dev", name}, {"link", "set", "dev", name, "up"}} {
		if out, e := exec.Command("ip", args...).CombinedOutput(); e != nil {
			t.Fatalf("ip: %v %s", e, out)
		}
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(10, 201, 77, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	localPort := conn.LocalAddr().(*net.UDPAddr).Port
	const remotePort = 42001
	if _, err := conn.WriteToUDP([]byte("tun-probe"), &net.UDPAddr{IP: net.IPv4(10, 201, 77, 2), Port: remotePort}); err != nil {
		t.Fatal(err)
	}
	frameCh := make(chan []byte, 1)
	go func() { frameCh <- f.ReadBatch() }()
	var frame []byte
	select {
	case frame = <-frameCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out reading kernel TUN")
	}
	p, ok := parseFrame(frame)
	if !ok || len(p) < 28 || p[0]>>4 != 4 || p[9] != 17 {
		t.Fatalf("invalid IPv4/UDP packet or frame")
	}
	if !net.IP(p[12:16]).Equal(net.IPv4(10, 201, 77, 1)) || !net.IP(p[16:20]).Equal(net.IPv4(10, 201, 77, 2)) || int(binary.BigEndian.Uint16(p[20:22])) != localPort || binary.BigEndian.Uint16(p[22:24]) != remotePort || string(p[28:]) != "tun-probe" {
		t.Fatal("kernel TUN outbound packet contents differ")
	}
	payload := []byte("tun-response")
	pkt := make([]byte, 20+8+len(payload))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = 17
	copy(pkt[12:16], net.IPv4(10, 201, 77, 2).To4())
	copy(pkt[16:20], net.IPv4(10, 201, 77, 1).To4())
	binary.BigEndian.PutUint16(pkt[10:12], ipChecksum(pkt[:20]))
	binary.BigEndian.PutUint16(pkt[20:22], remotePort)
	binary.BigEndian.PutUint16(pkt[22:24], uint16(localPort))
	binary.BigEndian.PutUint16(pkt[24:26], uint16(8+len(payload)))
	copy(pkt[28:], payload)
	framed := make([]byte, 2+len(pkt))
	binary.BigEndian.PutUint16(framed[:2], uint16(len(pkt)))
	copy(framed[2:], pkt)
	if !f.WriteBatch(framed) {
		t.Fatal("WriteBatch rejected valid frame")
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 100)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatal("wrong inbound payload")
	}
}
func ipChecksum(b []byte) uint16 {
	var n uint32
	for i := 0; i < len(b); i += 2 {
		n += uint32(binary.BigEndian.Uint16(b[i : i+2]))
	}
	for n>>16 != 0 {
		n = (n & 0xffff) + (n >> 16)
	}
	return ^uint16(n)
}
