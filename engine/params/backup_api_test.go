package params

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackupAddressAndRelayAllowlist(t *testing.T) {
	a := DefaultAddresses()
	if len(a) < 2 || a[len(a)-2] != (Address{Host: "api-ru.meridianvpn.org", Port: 443}) || a[len(a)-1] != (Address{Host: "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev", Port: 443}) {
		t.Fatal("backup must be last and use system TLS")
	}
	for _, tc := range []struct {
		address Address
		want    bool
	}{
		{Address{Host: "api-ru.meridianvpn.org", Port: 443}, true},
		{Address{Host: "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev", Port: 443}, true},
		{Address{Host: "cdn.jincomputers.win", Port: 443}, true},
		{Address{Host: "cdn.jincomputers.win", Port: 8443}, true},
		{Address{Host: "api-ru.meridianvpn.org", Port: 8443}, false},
		{Address{Host: "api-ru.meridianvpn.org", Port: 443, Pinned: true}, false},
		{Address{Host: "api-ru.meridianvpn.org.attacker.example", Port: 443}, false},
	} {
		endpoint, ok := relayLinksEndpoint(tc.address)
		if ok != tc.want || (ok && !strings.HasPrefix(endpoint, "https://"+tc.address.Host)) {
			t.Fatal("relay endpoint allowlist failure")
		}
	}
}

func TestRememberedBackupDoesNotPreemptPrimary(t *testing.T) {
	for _, saved := range []string{"api-ru.meridianvpn.org", "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev"} {
		c := New(Config{LoadLastGood: func() string { return saved }})
		addresses := c.ordered()
		if addresses[len(addresses)-2].Host != "api-ru.meridianvpn.org" || addresses[len(addresses)-1].Host != "meridian-ru-reserve-8d8e90b95b.bioton2.workers.dev" {
			t.Fatal("backup was promoted")
		}
	}
}

func TestGetBudgetReservesTimeForLaterAddress(t *testing.T) {
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	fast := httptest.NewTLSServer(apiHandler(liveAnonymous))
	defer fast.Close()
	c := New(Config{Addresses: []Address{addrOf(t, slow, true), addrOf(t, fast, true)}, Pins: []string{SPKIPin(slow.Certificate()), SPKIPin(fast.Certificate())}, WalkBudget: 1200 * time.Millisecond})
	start := time.Now()
	_, err := c.Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("did not reach later address: %v", err)
	}
	if time.Since(start) > 1100*time.Millisecond {
		t.Fatal("later address not reached within reserved budget")
	}
}

func TestRelayHTTPRefusalIsNotTransportError(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		_, err := decodeRelayLinksResponse(&http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))})
		if !errors.Is(err, ErrRelayRefused) {
			t.Fatal("server refusal not classified")
		}
	}
}

func TestMutationNotRetriedAfterAmbiguousTimeout(t *testing.T) {
	var calls atomic.Int32
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-r.Context().Done() }))
	defer slow.Close()
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(100); _, _ = io.WriteString(w, liveAnonymous) }))
	defer other.Close()
	c := New(Config{Addresses: []Address{addrOf(t, slow, true), addrOf(t, other, true)}, Pins: []string{SPKIPin(slow.Certificate()), SPKIPin(other.Certificate())}, ConnectTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WalkBudget: time.Second})
	_ = c.CallMethod(context.Background(), http.MethodPost, "/v1/trial", map[string]string{})
	if calls.Load() != 1 {
		t.Fatalf("mutation retried: %d", calls.Load())
	}
}
