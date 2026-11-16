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
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return core.WriteControlMessage(c.conn, t, p)
}

type helper struct {
	mu       sync.Mutex
	clients  map[*client]bool
	cancel   context.CancelFunc
	done     chan struct{}
	state    core.ControlStatePayload
	platform *linux.Platform
	store    *core.Store
	enabled  bool
	closing  bool
	run      func(context.Context, core.ControlConnectPayload)
}

func (h *helper) broadcast(t core.ControlMessageType, p any) error {
	h.mu.Lock()
	if t == core.ControlState {
		if s, ok := p.(core.ControlStatePayload); ok {
			h.state = s
		}
	}
	cs := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		cs = append(cs, c)
	}
	h.mu.Unlock()
	// Engine events or errors may contain sensitive data; this prototype emits
	// status and a generic error only, never raw diagnostic payloads.
	if t == core.ControlEvent {
		return nil
	}
	if t == core.ControlError {
		p = core.ControlErrorPayload{Code: core.ControlErrConnectFailed, Message: "Не удалось подключиться"}
	}
	for _, c := range cs {
		_ = c.send(t, p)
	}
	return nil
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
	st := h.state
	h.mu.Unlock()
	defer func() { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock() }()
	_ = c.send(core.ControlState, st)
	for {
		env, err := core.ReadControlMessage(n)
		if err != nil {
			return
		}
		switch env.Type {
		case core.ControlStatusQuery:
			h.mu.Lock()
			st = h.state
			h.mu.Unlock()
			_ = c.send(core.ControlState, st)
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
	flag.Parse()
	if *uid == 0 || uint64(*uid) >= uint64(^uint32(0)) {
		fmt.Fprintln(os.Stderr, "укажите непустой UID пользователя")
		os.Exit(1)
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
