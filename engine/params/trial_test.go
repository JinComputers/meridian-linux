package params

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// trialServer — тестовая служба: отдаёт code и body на POST /v1/trial и
// запоминает метод и параметр device_id.
func trialServer(t *testing.T, code int, body string, gotMethod, gotDevice *string) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotMethod = r.Method
		*gotDevice = r.URL.Query().Get("device_id")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
}

func trialClient(t *testing.T, s *httptest.Server) *Client {
	t.Helper()
	return New(Config{
		Addresses: []Address{addrOf(t, s, true)},
		Pins:      []string{SPKIPin(s.Certificate())},
	})
}

func TestStartTrialGranted(t *testing.T) {
	var method, dev string
	body := `{"contract":"` + Contract + `","status":"answered","verdict":"ok",` +
		`"key":"testkey-not-real-12345","expires_at":1790000000,"repeat":true}`
	s := trialServer(t, 200, body, &method, &dev)
	defer s.Close()

	a, err := trialClient(t, s).StartTrial(context.Background(), "dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost {
		t.Fatalf("метод %s, ждали POST", method)
	}
	if dev != "dev-1" {
		t.Fatalf("device_id %q", dev)
	}
	if !a.Granted() || a.Key != "testkey-not-real-12345" || !a.Repeat || a.ExpiresAt != 1790000000 {
		t.Fatalf("разбор: %+v", a)
	}
	if a.Limited || a.Temporary {
		t.Fatalf("выдача не должна быть ограничением: %+v", a)
	}
}

func TestStartTrialRateLimited429(t *testing.T) {
	var method, dev string
	body := `{"contract":"` + Contract + `","status":"answered","message":"слишком часто","retry_after":3600}`
	s := trialServer(t, 429, body, &method, &dev)
	defer s.Close()

	a, err := trialClient(t, s).StartTrial(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("429 — ответ службы, а не ошибка: %v", err)
	}
	if a.Granted() || !a.Limited || a.Code != 429 || a.RetryAfter != 3600 {
		t.Fatalf("429 разобран неверно: %+v", a)
	}
}

func TestStartTrialTemporary503(t *testing.T) {
	var method, dev string
	body := `{"contract":"` + Contract + `","status":"unavailable","message":"ключи закончились"}`
	s := trialServer(t, 503, body, &method, &dev)
	defer s.Close()

	a, err := trialClient(t, s).StartTrial(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("503 — ответ службы, а не ошибка: %v", err)
	}
	if a.Granted() || !a.Temporary || a.Limited {
		t.Fatalf("503 должен быть «временно», не отказ: %+v", a)
	}
}

func TestStartTrialNoAnswer(t *testing.T) {
	c := New(Config{Addresses: []Address{{Host: "127.0.0.1", Port: 1, Pinned: true}}, Pins: []string{PinMain}})
	_, err := c.StartTrial(context.Background(), "dev-1")
	e, ok := err.(*Error)
	if !ok || e.Kind != KindUnavailable {
		t.Fatalf("молчание адресов: ждали KindUnavailable, получили %v", err)
	}
}

// Ключ не попадает в текст ошибок и логов.
func TestStartTrialKeyNotInErrors(t *testing.T) {
	c := New(Config{Addresses: []Address{{Host: "127.0.0.1", Port: 1, Pinned: true}}, Pins: []string{PinMain}})
	_, err := c.StartTrial(context.Background(), "secret-device-id")
	if err != nil && strings.Contains(err.Error(), "secret-device-id") {
		t.Fatal("device_id попал в текст ошибки")
	}
}

// «Пробный был и закончился»: ответ по существу без ключа, повторять нельзя.
func TestStartTrialEnded(t *testing.T) {
	var method, dev string
	body := `{"contract":"` + Contract + `","status":"answered","verdict":"expired",` +
		`"repeat":true,"message":"пробный период закончился"}`
	s := trialServer(t, 200, body, &method, &dev)
	defer s.Close()

	a, err := trialClient(t, s).StartTrial(context.Background(), "dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Granted() || !a.Ended() || !a.Repeat || a.Message != "пробный период закончился" {
		t.Fatalf("конец пробного разобран неверно: %+v", a)
	}
	if a.Limited || a.Temporary {
		t.Fatalf("конец пробного — не «позже»: %+v", a)
	}
}
