//go:build linux

// This helper is a prototype and must not be installed as a production VPN.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/JinComputers/meridian-desktop/core"
	linux "github.com/JinComputers/meridian-desktop/platform/linux"
)

type client struct {
	conn net.Conn
	mu   sync.Mutex
}

func (c *client) send(t core.ControlMessageType, p any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendLocked(t, p)
}

// sendLocked requires c.mu and preserves ordering with state snapshots.
func (c *client) sendLocked(t core.ControlMessageType, p any) error {
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return core.WriteControlMessage(c.conn, t, p)
}

type helper struct {
	mu        sync.Mutex
	eventsMu  sync.Mutex
	clients   map[*client]bool
	cancel    context.CancelFunc
	done      chan struct{}
	state     core.ControlStatePayload
	lastError *core.ControlErrorPayload
	platform  *linux.Platform
	store     *core.Store
	enabled   bool
	closing   bool
	run       func(context.Context, core.ControlConnectPayload)
}

func (h *helper) broadcast(t core.ControlMessageType, p any) error {
	// Keep concurrent event deliveries in the same order as state updates.
	h.eventsMu.Lock()
	defer h.eventsMu.Unlock()
	// Raw diagnostics can contain secrets; retain only an allowlisted failure.
	if t == core.ControlEvent {
		return nil
	}
	if t == core.ControlError {
		failure := ""
		if original, ok := p.(core.ControlErrorPayload); ok {
			switch original.Failure {
			case core.FailureAPIUnavailable, "network", "refused", "resolve", "silent", "blocked", "blocked-nolinks", "blocked-linksdead", "blocked-captcha", "blocked-links", "blocked-relay", "unknown":
				failure = original.Failure
			}
		}
		code := core.ControlErrConnectFailed
		if h.store != nil {
			state, err := h.store.Load()
			if err != nil || state != nil {
				code = "cleanup_failed"
			}
		}
		p = core.ControlErrorPayload{Code: code, Message: "Не удалось подключиться", Failure: failure}
	}
	h.mu.Lock()
	if t == core.ControlState {
		if state, ok := p.(core.ControlStatePayload); ok {
			h.state = state
			h.lastError = nil
		}
	} else if t == core.ControlError {
		if e, ok := p.(core.ControlErrorPayload); ok {
			h.state = core.ControlStatePayload{}
			h.lastError = &e
		}
	}
	cs := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	for _, c := range cs {
		_ = c.send(t, p)
	}
	return nil
}

// sendSnapshot returns the latest error instead of stale Connected state.
// Reserve the client write lock before releasing h.mu so an older snapshot
// cannot be written after a newer broadcast.
func (h *helper) sendSnapshot(c *client) error {
	h.mu.Lock()
	c.mu.Lock()
	var typ core.ControlMessageType = core.ControlState
	var payload any = h.state
	if h.lastError != nil {
		typ, payload = core.ControlError, *h.lastError
	}
	h.mu.Unlock()
	defer c.mu.Unlock()
	return c.sendLocked(typ, payload)
}
func (h *helper) serve(n net.Conn, uid uint32) {
	defer n.Close()
	if linux.VerifyPeer(n, uid) != nil {
		return
	}
	_ = n.SetDeadline(time.Now().Add(5 * time.Second))
	if core.PerformControlHelloAsServer(n) != nil {
		return
	}
	_ = n.SetDeadline(time.Time{})
	c := &client{conn: n}
	h.mu.Lock()
	if h.closing || len(h.clients) >= 16 {
		h.mu.Unlock()
		return
	}
	h.clients[c] = true
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
	_ = h.sendSnapshot(c)
	for {
		env, err := core.ReadControlMessage(n)
		if err != nil {
			return
		}
		switch env.Type {
		case core.ControlStatusQuery:
			_ = h.sendSnapshot(c)
		case core.ControlDisconnect:
			h.mu.Lock()
			cancel := h.cancel
			h.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		case core.ControlConnect:
			if !h.enabled {
				_ = c.send(core.ControlError, core.ControlErrorPayload{Code: "not_ready", Message: "Linux-клиент ещё не прошёл проверку безопасности"})
				continue
			}
			payload, err := core.DecodeControlPayload[core.ControlConnectPayload](env)
			if err != nil {
				_ = c.send(core.ControlError, core.ControlErrorPayload{Code: "bad_request", Message: "Некорректный запрос"})
				continue
			}
			if payload.TunnelOnly || len(payload.DirectDomains) > 0 || payload.RUDirect || len(payload.TunnelApps) > 0 || payload.PartnerRouting {
				_ = c.send(core.ControlError, core.ControlErrorPayload{Code: "unsupported", Message: "Режим не поддерживается"})
				continue
			}
			h.mu.Lock()
			if h.cancel != nil || h.closing {
				h.mu.Unlock()
				_ = c.send(core.ControlError, core.ControlErrorPayload{Code: core.ControlErrAlreadyRunning, Message: "Уже подключается"})
				continue
			}
			ctx, cancel := context.WithCancel(context.Background())
			h.cancel = cancel
			done := make(chan struct{})
			h.done = done
			h.mu.Unlock()
			go func() {
				defer cancel()
				h.run(ctx, payload)
				h.mu.Lock()
				h.cancel = nil
				close(done)
				h.mu.Unlock()
			}()
		default:
			_ = c.send(core.ControlError, core.ControlErrorPayload{Code: "unsupported", Message: "Команда не поддерживается"})
		}
	}
}

// shutdown blocks until the session function has returned, including its
// cleanup. No handler can start a new session once closing is set.
func (h *helper) shutdown() {
	h.mu.Lock()
	h.closing = true
	cancel, done := h.cancel, h.done
	for c := range h.clients {
		_ = c.conn.Close()
	}
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (h *helper) runSession(ctx context.Context, payload core.ControlConnectPayload) {
	opts := payload.ConnectOptions()
	opts.TunnelDNS = []string{"1.1.1.1", "8.8.8.8"}
	cfg := core.ParamsConfig{Protector: h.platform.Protector(), Envelope: map[string]string{"device_id": opts.DeviceID, "platform": "linux", "app_version": "experimental", "app_code": "0"}}
	_ = core.RunControlConnect(ctx, h.store, h.platform, core.RealEngine{}, core.NewParamsRace(cfg), opts, h.broadcast)
}

func main() {
	uid := flag.Uint("uid", 0, "UID единственного пользователя")
	enabled := flag.Bool("enable-unverified-network", false, "разрешить экспериментальное подключение (только для изолированного стенда)")
	recoverNetwork := flag.Bool("recover-network", false, "явно восстановить сеть после сбоя и вернуть обычный доступ")
	flag.Parse()
	if !*recoverNetwork && (*uid == 0 || uint64(*uid) >= uint64(^uint32(0))) {
		fmt.Fprintln(os.Stderr, "укажите непустой UID пользователя")
		os.Exit(1)
	}
	lock, err := linux.AcquireHelperLock()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer lock.Close()
	if *recoverNetwork {
		if err := linux.RecoverJournal(context.Background(), linux.NewPlatform(nil)); err != nil {
			fmt.Fprintln(os.Stderr, "Восстановление сети не завершено:", err)
			os.Exit(1)
		}
		fmt.Println("Восстановление сети завершено")
		return
	}
	h := &helper{clients: map[*client]bool{}, platform: linux.NewPlatform(nil), store: &core.Store{Path: "/run/meridian/state.json"}, enabled: *enabled}
	h.run = h.runSession
	if st, err := h.store.Load(); err != nil || st != nil {
		fmt.Fprintln(os.Stderr, "обнаружен журнал или ошибка чтения; требуется ручная проверка")
		os.Exit(1)
	}
	l, err := linux.ListenControl(uint32(*uid))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer l.Close()
	defer h.shutdown()
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(ch)
	go func() { <-ch; _ = l.Close() }()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go h.serve(c, uint32(*uid))
	}
}
