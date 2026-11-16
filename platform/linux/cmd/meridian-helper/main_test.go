//go:build linux

package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/JinComputers/meridian-desktop/core"
)

func dialTest(t *testing.T, h *helper) net.Conn {
	t.Helper()
	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, e := l.Accept()
		_ = l.Close()
		if e == nil {
			h.serve(c, uint32(os.Getuid()))
		}
	}()
	c, err := net.Dial("unix", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err = core.PerformControlHelloAsClient(c, "test"); err != nil {
		t.Fatal(err)
	}
	msg, err := core.ReadControlMessage(c)
	if err != nil || msg.Type != core.ControlState {
		t.Fatalf("initial state: %v %v", msg, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func sendConnect(t *testing.T, c net.Conn) {
	t.Helper()
	if err := core.WriteControlMessage(c, core.ControlConnect, core.ControlConnectPayload{KeyMaterial: "test-only-synthetic"}); err != nil {
		t.Fatal(err)
	}
}
func TestNetworkDisabledByDefault(t *testing.T) {
	h := &helper{clients: map[*client]bool{}}
	c := dialTest(t, h)
	sendConnect(t, c)
	msg, err := core.ReadControlMessage(c)
	if err != nil || msg.Type != core.ControlError {
		t.Fatalf("wanted refusal: %v %v", msg, err)
	}
	e, err := core.DecodeControlPayload[core.ControlErrorPayload](msg)
	if err != nil || e.Code != "not_ready" {
		t.Fatalf("unexpected error: %v %v", e, err)
	}
	h.shutdown()
}
func TestSingleSessionAndShutdownWaitsForCleanup(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	cleanup := make(chan struct{})
	h := &helper{clients: map[*client]bool{}, enabled: true, run: func(ctx context.Context, _ core.ControlConnectPayload) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-cleanup
	}}
	a := dialTest(t, h)
	b := dialTest(t, h)
	sendConnect(t, a)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("session did not start")
	}
	sendConnect(t, b)
	msg, err := core.ReadControlMessage(b)
	if err != nil || msg.Type != core.ControlError {
		t.Fatalf("parallel connection accepted: %v %v", msg, err)
	}
	e, err := core.DecodeControlPayload[core.ControlErrorPayload](msg)
	if err != nil || e.Code != core.ControlErrAlreadyRunning {
		t.Fatalf("unexpected response: %v %v", e, err)
	}
	done := make(chan struct{})
	go func() { h.shutdown(); close(done) }()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("session not cancelled")
	}
	select {
	case <-done:
		t.Fatal("shutdown returned before cleanup")
	case <-time.After(50 * time.Millisecond):
	}
	close(cleanup)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	h.mu.Lock()
	closed := h.closing
	active := h.cancel != nil
	h.mu.Unlock()
	if !closed || active {
		t.Fatal("incorrect final state")
	}
}

func TestReconnectWaitsForPriorCleanup(t *testing.T) {
	started := make(chan int, 2)
	cleaning := make(chan struct{})
	release := make(chan struct{})
	runs := 0
	h := &helper{clients: map[*client]bool{}, enabled: true, run: func(ctx context.Context, _ core.ControlConnectPayload) {
		runs++
		n := runs
		started <- n
		<-ctx.Done()
		if n == 1 {
			close(cleaning)
			<-release
		}
	}}
	a := dialTest(t, h)
	b := dialTest(t, h)
	sendConnect(t, a)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no first session")
	}
	if err := core.WriteControlMessage(a, core.ControlDisconnect, struct{}{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cleaning:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not start")
	}
	sendConnect(t, b)
	msg, err := core.ReadControlMessage(b)
	if err != nil || msg.Type != core.ControlError {
		t.Fatalf("second connection accepted during cleanup: %v", err)
	}
	e, err := core.DecodeControlPayload[core.ControlErrorPayload](msg)
	if err != nil || e.Code != core.ControlErrAlreadyRunning {
		t.Fatalf("unexpected error %v %v", e, err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		h.mu.Lock()
		active := h.cancel != nil
		h.mu.Unlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first session did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	sendConnect(t, b)
	select {
	case n := <-started:
		if n != 2 {
			t.Fatal("unexpected run")
		}
	case <-time.After(time.Second):
		t.Fatal("retry not started after cleanup")
	}
	h.shutdown()
}

func TestBroadcastFailureNeverExposesRawError(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{"resolve", "resolve"},
		{core.FailureSystemDNSUnavailable, core.FailureSystemDNSUnavailable},
		{"synthetic-private-value", ""},
	} {
		a, b := net.Pipe()
		h := &helper{clients: map[*client]bool{&client{conn: a}: true}}
		done := make(chan struct{})
		go func() {
			_ = h.broadcast(core.ControlError, core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "synthetic-private-value", Failure: tc.input})
			close(done)
		}()
		env, err := core.ReadControlMessage(b)
		if err != nil {
			t.Fatal(err)
		}
		got, err := core.DecodeControlPayload[core.ControlErrorPayload](env)
		if err != nil || got.Failure != tc.expected || got.Message != "Не удалось подключиться" {
			t.Fatalf("unexpected sanitized failure: %+v %v", got, err)
		}
		<-done
		a.Close()
		b.Close()
	}
}

func TestCleanupFailureSurvivesClientReconnect(t *testing.T) {
	store := &core.Store{Path: filepath.Join(t.TempDir(), "journal.json")}
	if err := store.Save(&core.State{PID: os.Getpid(), StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	h := &helper{clients: map[*client]bool{}, store: store}
	if err := h.broadcast(core.ControlState, core.ControlStatePayload{Connected: true}); err != nil {
		t.Fatal(err)
	}
	first := dialTest(t, h)
	done := make(chan struct{})
	go func() {
		_ = h.broadcast(core.ControlError, core.ControlErrorPayload{Message: "synthetic-secret", Failure: "synthetic-secret"})
		close(done)
	}()
	if msg, err := core.ReadControlMessage(first); err != nil || msg.Type != core.ControlError {
		t.Fatalf("initial error missing: %v %v", msg, err)
	}
	<-done
	_ = first.Close()
	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "s"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, e := l.Accept()
		_ = l.Close()
		if e == nil {
			h.serve(c, uint32(os.Getuid()))
		}
	}()
	second, err := net.Dial("unix", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := core.PerformControlHelloAsClient(second, "test"); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		msg, err := core.ReadControlMessage(second)
		if err != nil || msg.Type != core.ControlError {
			t.Fatalf("expected retained error, got %v %v", msg, err)
		}
		got, err := core.DecodeControlPayload[core.ControlErrorPayload](msg)
		if err != nil || got.Code != "cleanup_failed" || got.Failure != "" || got.Message != "Не удалось подключиться" {
			t.Fatalf("unexpected error: %+v %v", got, err)
		}
	}
	check()
	if err := core.WriteControlMessage(second, core.ControlStatusQuery, nil); err != nil {
		t.Fatal(err)
	}
	check()
	h.shutdown()
}
