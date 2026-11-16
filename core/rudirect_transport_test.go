package core

import (
	"context"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/JinComputers/meridian-desktop/core/rudirect"
	"meridian/engine/params"
)

type directTestPlatform struct {
	*fakePlatform
	got []string
}

func (p *directTestPlatform) ApplyRUDirect(context.Context, *State, []netip.Prefix, func() error) error {
	return nil
}
func (p *directTestPlatform) ApplyDirect(_ context.Context, _ *State, domains []string, _ func() error) error {
	p.got = append([]string(nil), domains...)
	return nil
}
func (p *directTestPlatform) RefreshDirect(context.Context, []string) error { return nil }

func TestWhiteModeDirectDomainsApplyForEveryTransport(t *testing.T) {
	answer, err := params.ParseAnswer([]byte(cacheAnswer))
	if err != nil {
		t.Fatal(err)
	}
	for _, winner := range []string{"A", "T", "S", "R", ""} {
		t.Run("path_"+winner, func(t *testing.T) {
			p := &directTestPlatform{fakePlatform: newFakePlatform()}
			engine := NewStubEngine()
			engine.Winner = winner
			sess, err := Connect(context.Background(), &Store{Path: filepath.Join(t.TempDir(), "state.json")}, p, engine, fakeFetcher{ans: answer}, ConnectOptions{KeyMaterial: "test-key", Gateway: "example.org", RUDirect: true})
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Disconnect(context.Background())
			if len(p.got) != len(rudirect.Domains) || len(p.got) == 0 {
				t.Fatalf("direct domain count %d, expected %d", len(p.got), len(rudirect.Domains))
			}
			matcher := NewDirectMatcher(p.got)
			if matcher.Match("4pda.ru") {
				t.Fatal("excluded domain must not be direct")
			}
		})
	}
}

func (p *directTestPlatform) ApplyTunnelOnly(ctx context.Context, st *State, tun Tun, info TunnelInfo, _ []string, commit func() error) error {
	return p.fakePlatform.Apply(ctx, st, tun, info, commit)
}
func (p *directTestPlatform) RefreshTunnelOnly(context.Context, []string) error { return nil }

func TestBlackModeDoesNotApplyWhiteModeDirectDomains(t *testing.T) {
	answer, err := params.ParseAnswer([]byte(cacheAnswer))
	if err != nil {
		t.Fatal(err)
	}
	p := &directTestPlatform{fakePlatform: newFakePlatform()}
	engine := NewStubEngine()
	engine.Winner = "R"
	sess, err := Connect(context.Background(), &Store{Path: filepath.Join(t.TempDir(), "state.json")}, p, engine, fakeFetcher{ans: answer}, ConnectOptions{KeyMaterial: "test-key", Gateway: "example.org", RUDirect: true, TunnelOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Disconnect(context.Background())
	if len(p.got) != 0 {
		t.Fatal("white-mode domains applied in black mode")
	}
}
