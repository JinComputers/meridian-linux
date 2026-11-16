//go:build linux

// Package linux implements an experimental platform integration.
package linux

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/JinComputers/meridian-desktop/core"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

const ip = "/usr/sbin/ip"
const resolvectl = "/usr/bin/resolvectl"
const nft = "/usr/sbin/nft"

type Platform struct {
	run       core.CommandRunner
	protector socketProtector
}
type socketProtector struct {
	mu    sync.RWMutex
	iface string
	mark  uint32
}

func (p *socketProtector) Protect(fd int32) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.iface != "" && p.mark != 0 && unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, p.iface) == nil && unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(p.mark)) == nil
}
func NewPlatform(run core.CommandRunner) *Platform {
	if run == nil {
		run = core.ExecRunner{}
	}
	return &Platform{run: run}
}
func (p *Platform) Protector() core.Protector                     { return &p.protector }
func (p *Platform) AttachJournal(_ *core.State, _ func() error)   {}
func (p *Platform) NewTun(name string, mtu int) (core.Tun, error) { return NewTunFlow(name, mtu) }
func (p *Platform) Snapshot(ctx context.Context, st *core.State) error {
	out, err := p.run.Run(ctx, ip, "-j", "-4", "route", "show", "default")
	if err != nil {
		return err
	}
	var routes []struct {
		Dev     string `json:"dev"`
		Gateway string `json:"gateway"`
	}
	if err = json.Unmarshal([]byte(out), &routes); err != nil {
		return err
	}
	if len(routes) != 1 || routes[0].Dev == "" {
		return errors.New("Linux: expected exactly one IPv4 default route")
	}
	if _, err = p.run.Run(ctx, resolvectl, "status"); err != nil {
		return fmt.Errorf("Linux: systemd-resolved unavailable: %w", err)
	}
	resolvConf, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("Linux: cannot read system DNS: %w", err)
	}
	if !usesResolvedStub(string(resolvConf)) {
		return errors.New("Linux: system DNS must use systemd-resolved stub 127.0.0.53 exclusively")
	}
	var markBytes [4]byte
	if _, err = rand.Read(markBytes[:]); err != nil {
		return err
	}
	mark := binary.BigEndian.Uint32(markBytes[:])
	if mark == 0 {
		return errors.New("Linux: invalid socket mark")
	}
	st.PhysicalInterface = routes[0].Dev
	st.PhysicalGateway = routes[0].Gateway
	p.protector.mu.Lock()
	p.protector.iface = routes[0].Dev
	p.protector.mark = mark
	p.protector.mu.Unlock()
	return nil
}

// Require the local resolved stub exclusively; another configured resolver can
// bypass per-link tunnel DNS. This is a point-in-time check, not a monitor.
func usesResolvedStub(conf string) bool {
	count := 0
	for _, line := range strings.Split(conf, "\n") {
		fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
		if len(fields) > 0 && fields[0] == "nameserver" {
			if len(fields) != 2 || fields[1] != "127.0.0.53" {
				return false
			}
			count++
		}
	}
	return count == 1
}

func (p *Platform) Apply(ctx context.Context, st *core.State, t core.Tun, info core.TunnelInfo, commit func() error) error {
	name, err := t.Name()
	if err != nil {
		return err
	}
	if !validInterfaceName(name) {
		return errors.New("Linux: invalid TUN name")
	}
	p.protector.mu.RLock()
	mark := p.protector.mark
	p.protector.mu.RUnlock()
	if mark == 0 {
		return errors.New("Linux: socket protection was not initialized")
	}
	if v := net.ParseIP(info.IP); v == nil || v.To4() == nil || info.PrefixLen < 0 || info.PrefixLen > 32 || info.MTU < 576 || info.MTU > 65535 {
		return errors.New("Linux: invalid tunnel settings")
	}
	// A unique table avoids modifying any existing firewall rules. Journal the
	// intent first so cleanup after a crash knows which table to remove.
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	st.IPv6Service = "meridian_" + hex.EncodeToString(random[:])
	st.IPv6Off = true
	if err = commit(); err != nil {
		return err
	}
	// Block traffic outside this VPN except loopback and protected engine
	// sockets. This deliberately blocks access to the local network.
	rules := fmt.Sprintf("add table inet %s\nadd chain inet %s output { type filter hook output priority -150; policy accept; }\nadd rule inet %s output oifname \"lo\" counter accept\nadd rule inet %s output meta nfproto ipv6 counter drop\nadd rule inet %s output oifname \"%s\" counter accept\nadd rule inet %s output meta mark %d counter accept\nadd rule inet %s output counter drop\n", st.IPv6Service, st.IPv6Service, st.IPv6Service, st.IPv6Service, st.IPv6Service, name, st.IPv6Service, mark, st.IPv6Service)
	if _, err = p.run.RunWithInput(ctx, nft, rules, "-f", "-"); err != nil {
		return fmt.Errorf("Linux: VPN firewall: %w", err)
	}
	st.UtunName = name
	if err = commit(); err != nil {
		return err
	}
	for _, args := range [][]string{{"link", "set", "dev", name, "mtu", strconv.Itoa(int(info.MTU)), "up"}, {"address", "add", fmt.Sprintf("%s/%d", info.IP, info.PrefixLen), "dev", name}} {
		if _, err = p.run.Run(ctx, ip, args...); err != nil {
			return err
		}
	}
	st.FullTunnelSet = true
	if err = commit(); err != nil {
		return err
	}
	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if _, err = p.run.Run(ctx, ip, "route", "add", prefix, "dev", name); err != nil {
			return err
		}
	}
	st.TunnelDNSSet = true
	if err = commit(); err != nil {
		return err
	}
	if _, err = p.run.Run(ctx, resolvectl, "dns", name, "1.1.1.1", "8.8.8.8"); err != nil {
		return err
	}
	if _, err = p.run.Run(ctx, resolvectl, "domain", name, "~."); err != nil {
		return err
	}
	return nil
}
func (p *Platform) Restore(ctx context.Context, st *core.State) error {
	var failures []error
	// Read the interface inventory rather than treating every lookup error as
	// absence: failing closed preserves the journal for retry.
	out, err := p.run.Run(ctx, ip, "-j", "link", "show")
	present := false
	if err != nil {
		failures = append(failures, err)
	} else {
		var links []struct {
			Name string `json:"ifname"`
		}
		if err := json.Unmarshal([]byte(out), &links); err != nil {
			failures = append(failures, err)
		} else {
			for _, link := range links {
				if link.Name == st.UtunName && st.UtunName != "" {
					present = true
				}
			}
			// The session closes its non-persistent TUN before Restore. If a
			// device with this name still exists, identity is unproven: it may
			// have been replaced by another process after a crash. Never reset
			// DNS or routes on that device by name alone.
			if present && (st.TunnelDNSSet || st.FullTunnelSet) {
				failures = append(failures, errors.New("Linux: tunnel name exists after close; refusing to alter an unverified interface"))
			}

		}
	}
	// Keep the fail-closed firewall when interface ownership or inventory is
	// uncertain. The caller retains the journal and can retry or inspect.
	if len(failures) != 0 {
		return errors.Join(failures...)
	}
	if st.IPv6Off {
		name := st.IPv6Service
		if !validFirewallName(name) {
			failures = append(failures, errors.New("Linux: invalid firewall journal"))
		} else {
			// List first so cleanup is idempotent when the table was not created.
			out, err := p.run.Run(ctx, nft, "-j", "list", "tables")
			if err != nil {
				failures = append(failures, err)
			} else {
				var listing struct {
					Nftables []struct {
						Table *struct {
							Family string `json:"family"`
							Name   string `json:"name"`
						} `json:"table"`
					} `json:"nftables"`
				}
				if err := json.Unmarshal([]byte(out), &listing); err != nil {
					failures = append(failures, err)
				} else {
					for _, item := range listing.Nftables {
						if item.Table != nil && item.Table.Family == "inet" && item.Table.Name == name {
							if _, err := p.run.Run(ctx, nft, "delete", "table", "inet", name); err != nil {
								failures = append(failures, err)
							}
							break
						}
					}
				}
			}
		}
	}
	return errors.Join(failures...)
}

func validInterfaceName(name string) bool {
	if len(name) == 0 || len(name) > 15 {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return name != "." && name != ".."
}

func validFirewallName(name string) bool {
	if !strings.HasPrefix(name, "meridian_") || len(name) != len("meridian_")+24 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(name, "meridian_"))
	return err == nil
}

func (p *Platform) ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, e := os.FindProcess(pid)
	if e != nil {
		return false
	}
	e = proc.Signal(syscall.Signal(0))
	return e == nil || errors.Is(e, syscall.EPERM)
}

var _ core.Platform = (*Platform)(nil)
