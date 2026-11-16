package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
)

func TestWriteReadControlMessageRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := ControlConnectPayload{
		KeyMaterial: "секрет-не-для-лога",
		Gateway:     "edge.jincomputers.win",
		DeviceID:    "dev-1",
		Ladder:      LadderOptions{Known: "B", SkipDirect: true},
	}
	if err := WriteControlMessage(&buf, ControlConnect, payload); err != nil {
		t.Fatalf("WriteControlMessage: %v", err)
	}

	env, err := ReadControlMessage(&buf)
	if err != nil {
		t.Fatalf("ReadControlMessage: %v", err)
	}
	if env.Type != ControlConnect {
		t.Fatalf("Type = %q, хотели %q", env.Type, ControlConnect)
	}
	got, err := DecodeControlPayload[ControlConnectPayload](env)
	if err != nil {
		t.Fatalf("DecodeControlPayload: %v", err)
	}
	if !reflect.DeepEqual(got, payload) {
		t.Fatalf("payload после round-trip = %+v, хотели %+v", got, payload)
	}
}

func TestWriteControlMessageNilPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteControlMessage(&buf, ControlDisconnect, nil); err != nil {
		t.Fatalf("WriteControlMessage: %v", err)
	}
	env, err := ReadControlMessage(&buf)
	if err != nil {
		t.Fatalf("ReadControlMessage: %v", err)
	}
	if env.Type != ControlDisconnect {
		t.Fatalf("Type = %q, хотели %q", env.Type, ControlDisconnect)
	}
	if len(env.Payload) != 0 {
		t.Fatalf("Payload = %q, хотели пусто", env.Payload)
	}
}

func TestReadControlMessageEOFOnCleanClose(t *testing.T) {
	var buf bytes.Buffer // пусто — как только что закрытое соединение без недописанного кадра
	_, err := ReadControlMessage(&buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadControlMessage на пустом потоке = %v, хотели io.EOF", err)
	}
}

func TestReadControlMessageUnexpectedEOFOnTruncatedFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteControlMessage(&buf, ControlStatusQuery, nil); err != nil {
		t.Fatalf("WriteControlMessage: %v", err)
	}
	full := buf.Bytes()
	truncated := bytes.NewReader(full[:len(full)-2]) // обрыв внутри тела кадра
	_, err := ReadControlMessage(truncated)
	if err == nil {
		t.Fatal("ReadControlMessage на обрезанном кадре должен вернуть ошибку")
	}
}

func TestReadControlMessageRejectsOversizedFrame(t *testing.T) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], controlMaxFrame+1)
	r := bytes.NewReader(lenBuf[:])
	_, err := ReadControlMessage(r)
	if err == nil {
		t.Fatal("ReadControlMessage должен отказать кадру больше controlMaxFrame")
	}
}

func TestDecodeControlPayloadEmptyIsError(t *testing.T) {
	env := ControlEnvelope{Type: ControlConnect}
	if _, err := DecodeControlPayload[ControlConnectPayload](env); err == nil {
		t.Fatal("DecodeControlPayload с пустым Payload должен вернуть ошибку")
	}
}

// --- Hello: реальная пара соединённых концов (net.Pipe), не буфер, —
// PerformControlHelloAs{Client,Server} читают и пишут с обеих сторон
// одновременно, буфер в один конец здесь не годится (нужен настоящий
// дуплекс, как у net.Conn на любом реальном транспорте).

func TestControlHelloVersionMatch(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var clientErr, serverErr error
	go func() {
		defer wg.Done()
		clientErr = PerformControlHelloAsClient(client, "ui-test")
	}()
	go func() {
		defer wg.Done()
		serverErr = PerformControlHelloAsServer(server)
	}()
	wg.Wait()

	if clientErr != nil {
		t.Fatalf("PerformControlHelloAsClient: %v", clientErr)
	}
	if serverErr != nil {
		t.Fatalf("PerformControlHelloAsServer: %v", serverErr)
	}
}

func TestControlHelloVersionMismatchRejectedByServer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var clientErr, serverErr error
	go func() {
		defer wg.Done()
		// Не вызываем PerformControlHelloAsClient — шлём чужую версию
		// вручную, чтобы проверить именно реакцию сервера.
		clientErr = WriteControlMessage(client, ControlHello, ControlHelloPayload{ProtocolVersion: ControlProtocolVersion + 1})
		if clientErr != nil {
			return
		}
		env, err := ReadControlMessage(client)
		if err != nil {
			clientErr = err
			return
		}
		if env.Type != ControlError {
			clientErr = errFromType(env.Type)
		}
	}()
	go func() {
		defer wg.Done()
		serverErr = PerformControlHelloAsServer(server)
	}()
	wg.Wait()

	if clientErr != nil {
		t.Fatalf("клиентская часть теста: %v", clientErr)
	}
	if serverErr == nil {
		t.Fatal("PerformControlHelloAsServer должен отказать при несовпадении версий")
	}
}

func TestControlHelloNonHelloFirstRejectedByServer(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	var writeErr, serverErr error
	go func() {
		defer wg.Done()
		writeErr = WriteControlMessage(client, ControlStatusQuery, nil)
		if writeErr != nil {
			return
		}
		// Сервер отвечает ControlError (PerformControlHelloAsServer) —
		// net.Pipe синхронный: если это не вычитать, Write сервера
		// блокируется навсегда и тест виснет, а не падает по таймауту
		// теста. Ответ здесь не проверяем — за это отвечает
		// TestControlHelloVersionMismatchRejectedByServer.
		_, _ = ReadControlMessage(client)
	}()
	go func() {
		defer wg.Done()
		serverErr = PerformControlHelloAsServer(server)
	}()
	wg.Wait()

	if writeErr != nil {
		t.Fatalf("WriteControlMessage: %v", writeErr)
	}
	if serverErr == nil {
		t.Fatal("PerformControlHelloAsServer должен отказать, если первое сообщение не hello")
	}
}

func errFromType(typ ControlMessageType) error {
	return &unexpectedTypeError{typ}
}

type unexpectedTypeError struct{ typ ControlMessageType }

func (e *unexpectedTypeError) Error() string {
	return "неожиданный тип ответа: " + string(e.typ)
}

// Каждое поле ControlConnectPayload обязано доехать до ConnectOptions с
// тем же именем и значением: новое поле в протоколе без переноса — этот
// тест падает (класс бага, который кот 6 поймал с DirectDomains).
func TestControlConnectPayloadMapsEveryField(t *testing.T) {
	p := ControlConnectPayload{
		KeyMaterial:     "k",
		Gateway:         "g",
		DeviceID:        "d",
		CachedFallback:  "c",
		PartnerRouting:  true,
		DisableAutoDion: true,
		Ladder:          LadderOptions{Known: "B", Hashes: []string{"h"}},
		DirectDomains:   []string{"api.ipify.org"},
		TunnelOnly:      true,
		TunnelDomains:   []string{"youtube.com"},
		RUDirect:        true,
		AntiDPI:         AntiDPIGroups{YouTube: true, Discord: true, Cloudflare: true, Social: true, Other: true},
		TunnelApps:      []string{`C:\Games\x.exe`},
		DionRooms:       []string{"room1"},
		RelayPrimary:    RelayPrimaryDion,
	}
	pv := reflect.ValueOf(p)
	for i := 0; i < pv.NumField(); i++ {
		if pv.Field(i).IsZero() {
			t.Fatalf("тест устарел: поле %s в примере пустое — заполните, иначе перенос не проверяется", pv.Type().Field(i).Name)
		}
	}
	ov := reflect.ValueOf(p.ConnectOptions())
	for i := 0; i < pv.NumField(); i++ {
		name := pv.Type().Field(i).Name
		of := ov.FieldByName(name)
		if !of.IsValid() {
			t.Fatalf("в ConnectOptions нет поля %s из ControlConnectPayload", name)
		}
		if !reflect.DeepEqual(of.Interface(), pv.Field(i).Interface()) {
			t.Fatalf("поле %s не перенесено: %v → %v", name, pv.Field(i).Interface(), of.Interface())
		}
	}
}

func TestControlCaptchaPayloadRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := ControlCaptchaPayload{ID: "ab12", URL: "https://id.vk.ru/not_robot_captcha?x=1", Done: false}
	if err := WriteControlMessage(&buf, ControlCaptchaRequest, in); err != nil {
		t.Fatal(err)
	}
	env, err := ReadControlMessage(&buf)
	if err != nil || env.Type != ControlCaptchaRequest {
		t.Fatalf("env=%v err=%v", env.Type, err)
	}
	out, err := DecodeControlPayload[ControlCaptchaPayload](env)
	if err != nil || out != in {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}
