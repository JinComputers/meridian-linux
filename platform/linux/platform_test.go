//go:build linux

package linux

import (
	"context"
	"errors"
	"github.com/JinComputers/meridian-desktop/core"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls                      []string
	input, links, tables, fail string
	routes                     map[string]string
}

func (f *fakeRunner) Run(_ context.Context, n string, a ...string) (string, error) {
	s := n + " " + strings.Join(a, " ")
	f.calls = append(f.calls, s)
	if f.fail != "" && strings.Contains(s, f.fail) {
		return "", errors.New("injected")
	}
	if strings.Contains(s, "link show") {
		return f.links, nil
	}
	if strings.Contains(s, "route show exact") {
		return f.routes[a[len(a)-1]], nil
	}
	if strings.Contains(s, "list tables") {
		return f.tables, nil
	}
	return "", nil
}
func (f *fakeRunner) RunWithInput(_ context.Context, n, i string, a ...string) (string, error) {
	f.calls = append(f.calls, n)
	f.input = i
	return "", nil
}

type fakeTun struct{}

func (fakeTun) Name() (string, error)  { return "meridian0", nil }
func (fakeTun) ReadBatch() []byte      { return nil }
func (fakeTun) WriteBatch([]byte) bool { return true }
func (fakeTun) Close()                 {}
func TestIPv6JournaledBeforeFirewall(t *testing.T) {
	f := &fakeRunner{}
	st := &core.State{}
	n := 0
	c := func() error {
		n++
		if n == 1 && (!st.IPv6Off || !validFirewallName(st.IPv6Service) || len(f.calls) != 0) {
			t.Fatal("not journaled first")
		}
		return nil
	}
	p := NewPlatform(f)
	p.protector.mark = 123
	if err := p.Apply(context.Background(), st, fakeTun{}, core.TunnelInfo{IP: "10.0.0.2", PrefixLen: 24, MTU: 1200}, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.input, "meta nfproto ipv6 counter drop") {
		t.Fatal("no IPv6 filter")
	}
}
func TestRestoreRefusesUnverifiedInterface(t *testing.T) {
	name := "meridian_00112233445566778899aabb"
	f := &fakeRunner{links: `[{"ifname":"meridian0"}]`, tables: `{"nftables":[{"table":{"family":"inet","name":"` + name + `"}}]}`}
	st := &core.State{UtunName: "meridian0", FullTunnelSet: true, TunnelDNSSet: true, IPv6Off: true, IPv6Service: name}
	if NewPlatform(f).Restore(context.Background(), st) == nil {
		t.Fatal("must refuse unverified interface")
	}
	s := strings.Join(f.calls, "\n")
	if strings.Contains(s, "route del") || strings.Contains(s, "revert meridian0") {
		t.Fatal("changed unverified interface")
	}
	if strings.Contains(s, "delete table inet "+name) {
		t.Fatal("must retain fail-closed firewall while interface ownership is uncertain")
	}
}
func TestRestoreAbsentIdempotent(t *testing.T) {
	f := &fakeRunner{links: `[]`, tables: `{"nftables":[]}`}
	s := &core.State{UtunName: "meridian0", FullTunnelSet: true, TunnelDNSSet: true, IPv6Off: true, IPv6Service: "meridian_00112233445566778899aabb"}
	if err := NewPlatform(f).Restore(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestResolvedStubOnly(t *testing.T) {
	for _, tc := range []struct {
		in string
		ok bool
	}{{"nameserver 127.0.0.53\noptions edns0\n", true}, {"nameserver 1.1.1.1\n", false}, {"nameserver 127.0.0.53\nnameserver 192.0.2.1\n", false}, {"nameserver 127.0.0.54\n", false}, {"# nameserver 127.0.0.53\n", false}} {
		if got := usesResolvedStub(tc.in); got != tc.ok {
			t.Errorf("%q: %v", tc.in, got)
		}
	}
}
