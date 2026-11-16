//go:build linux

package linux

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JinComputers/meridian-desktop/core"
)

func labRecoveryState() *core.State {
	return &core.State{PID: 2147483646, StartedAt: time.Now(), PhysicalInterface: "eth0", UtunName: "meridian0", FullTunnelSet: true, TunnelDNSSet: true, IPv6Off: true, IPv6Service: "meridian_00112233445566778899aabb"}
}
func recoveryStore(t *testing.T) (string, *core.Store) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root required")
	}
	d := t.TempDir()
	return d, &core.Store{Path: filepath.Join(d, "state.json")}
}
func TestRecoverJournalSuccessAndIdempotence(t *testing.T) {
	d, s := recoveryStore(t)
	if err := s.Save(labRecoveryState()); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{links: "[]", tables: `{"nftables":[{"table":{"family":"inet","name":"meridian_00112233445566778899aabb"}}]}`}
	p := NewPlatform(f)
	if err := recoverJournalAt(context.Background(), d, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "delete table inet meridian_00112233445566778899aabb") {
		t.Fatal("firewall not deleted")
	}
	if st, err := s.Load(); err != nil || st != nil {
		t.Fatalf("journal remains %v", err)
	}
	if err := recoverJournalAt(context.Background(), d, p); err != nil {
		t.Fatal(err)
	}
}
func TestRecoverJournalRefusesUntrustedOrUncertain(t *testing.T) {
	for _, name := range []string{"symlink", "mode", "hardlink", "live-pid", "unknown-field", "live-interface", "restore-failure"} {
		t.Run(name, func(t *testing.T) {
			d, s := recoveryStore(t)
			state := labRecoveryState()
			if name == "live-pid" {
				state.PID = os.Getpid()
			}
			if err := s.Save(state); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "symlink":
				if err := os.Remove(s.Path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/etc/passwd", s.Path); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(s.Path, 0644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(s.Path, filepath.Join(d, "other")); err != nil {
					t.Fatal(err)
				}
			case "unknown-field":
				f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.WriteString(`{}`)
				f.Close()
			}
			f := &fakeRunner{links: "[]", tables: `{"nftables":[]}`}
			if name == "live-interface" {
				f.links = `[{"ifname":"meridian0"}]`
			}
			if name == "restore-failure" {
				f.fail = "link show"
			}
			if err := recoverJournalAt(context.Background(), d, NewPlatform(f)); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if _, err := os.Lstat(s.Path); err != nil {
				t.Fatalf("journal removed: %v", err)
			}
			if strings.Contains(strings.Join(f.calls, "\n"), "delete table") {
				t.Fatal("removed firewall despite uncertainty")
			}
		})
	}
}
func TestRecoverJournalRealNft(t *testing.T) {
	if os.Getenv("MERIDIAN_ISOLATED_NETWORK_TEST") != "1" {
		t.Skip("isolated container only")
	}
	d, s := recoveryStore(t)
	state := labRecoveryState()
	name := state.IPv6Service
	for _, table := range []string{name, "unrelated_test"} {
		if out, err := exec.Command("nft", "add", "table", "inet", table).CombinedOutput(); err != nil {
			t.Fatalf("nft %s: %v %s", table, err, out)
		}
	}
	defer exec.Command("nft", "delete", "table", "inet", "unrelated_test").Run()
	if err := s.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := recoverJournalAt(context.Background(), d, NewPlatform(nil)); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("nft", "list", "tables").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), name) || !strings.Contains(string(out), "unrelated_test") {
		t.Fatalf("wrong firewall tables: %s", out)
	}
	if err := recoverJournalAt(context.Background(), d, NewPlatform(nil)); err != nil {
		t.Fatal(err)
	}
}
