//go:build linux

package linux

import (
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

// TunFlow адаптирует golang.zx2c4.com/wireguard/tun.Device к
// core.PacketFlow (ReadBatch/WriteBatch/Close, engine/flowconn.go) —
// движок кота 3 умеет говорить с настольным клиентом только через
// PacketFlow (engine.StartFlow), доступа к файловому дескриптору TUN
// напрямую (как у Android, engine.Start(fd)) у desktop-фасада нет.
//
// Кадр между движком и платформой — [2 байта длины, big-endian][пакет]
// (engine/flowconn.go). Сверено с самим движком, не только с
// комментарием: flowConn.Write на стороне движка собирает именно такой
// кадр ПЕРЕД вызовом WriteBatch — значит WriteBatch получает кадр С
// заголовком, а не голый пакет. ReadBatch отдаёт один или несколько
// таких кадров подряд одним куском.
type TunFlow struct {
	dev tun.Device

	// bufs/sizes — общий буфер под ReadBatch. Движок зовёт ReadBatch
	// ОДНОЙ горутиной (tunToNet, flowconn.go) — переиспользование между
	// вызовами без замка тут НАМЕРЕННО, не пропущенная блокировка.
	bufs  [][]byte
	sizes []int

	closeOnce sync.Once

	// inbound — чёрный режим (tunnelonly.go): смотрит каждый пакет из
	// туннеля к ОС ДО записи в utun (ответ DNS с именем из списка →
	// маршрут на адреса успевает встать раньше, чем приложение их
	// получит). nil — не смотрим.
	inbound atomic.Pointer[func([]byte)]

	// rx*/tx* — счётчики самого потока, не интерфейса ОС (netstat -I):
	// к моменту Restore() utun уже закрыт TunFlow.Close() (session.go
	// зовёт tun.Close() раньше platform.Restore()) и netstat его не
	// находит. Эти считаются прямо на WriteBatch/ReadBatch — правдивы
	// независимо от порядка закрытия (просьба владельца 28.09: разобрать
	// сессию постфактум, без слежки вживую).
	rxPackets, rxBytes atomic.Int64 // network → TUN (WriteBatch)
	txPackets, txBytes atomic.Int64 // TUN → network (ReadBatch)
}

// counters — снимок для лога, вызывать после Close() тоже можно.
func (f *TunFlow) counters() string {
	return fmt.Sprintf("rx: %d пак/%d байт, tx: %d пак/%d байт",
		f.rxPackets.Load(), f.rxBytes.Load(), f.txPackets.Load(), f.txBytes.Load())
}

// SetInboundHook — см. TunFlow.inbound. nil снимает.
func (f *TunFlow) SetInboundHook(h func(pkt []byte)) {
	if h == nil {
		f.inbound.Store(nil)
		return
	}
	f.inbound.Store(&h)
}

// frameHeaderLen — длина ЗАГОЛОВКА КАДРА МЕЖДУ ДВИЖКОМ И ПЛАТФОРМОЙ: 2
// байта, big-endian (engine/flowconn.go). НЕ ПУТАТЬ с tunOffset ниже —
// разные слои, разные числа, спутал их в первой версии этого файла и
// поймал панику живым прогоном (см. tunOffset).
const frameHeaderLen = 2

// Linux WireGuard TUN may enable virtio-net offloads. Write requires at least
// 10 bytes of headroom for the virtio header; 16 leaves a safe margin.
const tunOffset = 16

// defaultTunName выбирает имя интерфейса для Linux.
// core.ConnectOptions.TunName документирует пустую строку как «платформа
// выбирает сама», но tun.CreateTUN пустое имя НЕ принимает вообще —
// требует "utun[0-9]*" и падает с "Interface name must be utun[0-9]*".
// Первая версия этого комментария утверждала обратное, не проверив —
// живой прогон -mode=connect поймал ошибку на первом же вызове.
func defaultTunName(name string) string {
	if name == "" {
		return "meridian%d"
	}
	return name
}

// NewTunFlow поднимает utun с заданным именем ("" — берёт "meridian%d", ядро
// подставит номер само, устройство будет называться "utunN") и MTU.
// mtu — MTU туннеля, обычно core.TunnelInfo.MTU от движка после Connect.
func NewTunFlow(name string, mtu int) (*TunFlow, error) {
	name = defaultTunName(name)
	dev, err := tun.CreateTUN(name, mtu)
	if err != nil {
		return nil, fmt.Errorf("tun.CreateTUN(%q, %d): %w", name, mtu, err)
	}
	return newTunFlow(dev, mtu), nil
}

// newTunFlow — общее тело NewTunFlow, отдельно от создания устройства,
// чтобы тесты могли подставить фейковый tun.Device (интерфейс, не
// требует root и настоящей сети).
func newTunFlow(dev tun.Device, mtu int) *TunFlow {
	batch := dev.BatchSize()
	if batch < 1 {
		batch = 1
	}
	// Запас +80 — тот же, что engine.flowFrameMax (tunMTU+80,
	// engine/flowconn.go): пакет из utun крупнее заявленного MTU не
	// приходит, но запас на неточность не помешает. tunOffset — под
	// 4-байтовый заголовок семейства адресов, который сама wireguard-go
	// читает/пишет в bufs[i][:tunOffset] (см. tunOffset выше).
	bufSize := tunOffset + mtu + 80
	bufs := make([][]byte, batch)
	for i := range bufs {
		bufs[i] = make([]byte, bufSize)
	}
	return &TunFlow{dev: dev, bufs: bufs, sizes: make([]int, batch)}
}

// Name — имя реально созданного интерфейса (utunN — ядро подставляет N).
func (f *TunFlow) Name() (string, error) { return f.dev.Name() }

// ReadBatch — core.PacketFlow. Блокирует до появления пакетов, отдаёт
// один или несколько кадров подряд одним куском. Пустой срез — то же
// самое, что nil, и означает закрытие потока (flowconn.go: пустое и nil
// через границу неразличимы, договор требует не путать «сейчас нечего»
// с концом потока — тут различать и не нужно, у нас всегда один процесс).
func (f *TunFlow) ReadBatch() []byte {
	n, err := f.dev.Read(f.bufs, f.sizes, tunOffset)
	if err != nil || n == 0 {
		return nil
	}
	for i := 0; i < n; i++ {
		f.txBytes.Add(int64(f.sizes[i]))
	}
	f.txPackets.Add(int64(n))
	return frameBatch(f.bufs, f.sizes, n)
}

// frameBatch — чистая функция кодирования, вынесена ради теста без
// настоящего tun.Device: n пакетов уже лежат в bufs[i][tunOffset:]
// (dev.Read положил их туда, offset=tunOffset), длины — в sizes[i];
// результат — n кадров [2 байта длины][пакет] подряд, формат кадра
// между движком и платформой (frameHeaderLen), НЕ формат буфера devices.
func frameBatch(bufs [][]byte, sizes []int, n int) []byte {
	total := 0
	for i := 0; i < n; i++ {
		total += frameHeaderLen + sizes[i]
	}
	out := make([]byte, 0, total)
	for i := 0; i < n; i++ {
		var hdr [frameHeaderLen]byte
		binary.BigEndian.PutUint16(hdr[:], uint16(sizes[i]))
		out = append(out, hdr[:]...)
		out = append(out, bufs[i][tunOffset:tunOffset+sizes[i]]...)
	}
	return out
}

// WriteBatch — core.PacketFlow. Принимает РОВНО ОДИН кадр (с заголовком
// длины — см. комментарий у типа). Может звать до slotsMax горутин
// параллельно (netToTun, flowconn.go) — реализация ничего не делит
// между вызовами (свежий буфер на вызов), потокобезопасна по
// построению, без замка.
//
// Буфер для dev.Write — НЕ pkt напрямую: wireguard-go сама пишет
// 4-байтовый заголовок семейства адресов в bufs[0][:tunOffset] (см.
// tunOffset), значит перед пакетом обязан быть настоящий отступ в
// СОБСТВЕННОМ буфере, а не чужие байты из кадра движка — копируем.
func (f *TunFlow) WriteBatch(b []byte) bool {
	pkt, ok := parseFrame(b)
	if !ok {
		return false
	}
	if h := f.inbound.Load(); h != nil {
		(*h)(pkt)
	}
	buf := make([]byte, tunOffset+len(pkt))
	copy(buf[tunOffset:], pkt)
	if _, err := f.dev.Write([][]byte{buf}, tunOffset); err != nil {
		return false
	}
	f.rxPackets.Add(1)
	f.rxBytes.Add(int64(len(pkt)))
	return true
}

// parseFrame — чистая функция разбора, вынесена ради теста. Возвращает
// ok=false на кадре короче заголовка или с длиной, не совпавшей с
// остатком, — движок такое не пришлёт, но доверять чужому входу без
// проверки не стоит (тот же принцип, что structural() на приёме в
// движке, только здесь и решение — отказ, а не разрыв потока).
func parseFrame(b []byte) (packet []byte, ok bool) {
	if len(b) < frameHeaderLen {
		return nil, false
	}
	n := binary.BigEndian.Uint16(b[:frameHeaderLen])
	pkt := b[frameHeaderLen:]
	if int(n) != len(pkt) {
		return nil, false
	}
	return pkt, true
}

// Close — core.PacketFlow. Идемпотентен (движок обязан это уметь звать
// повторно, flowconn.go), сам utun закрывается ровно один раз: закрытие
// будит зависший ReadBatch (закрытый дескриптор обрывает блокирующий
// Read) — без этого AwaitStopped у движка не дождался бы насоса.
func (f *TunFlow) Close() {
	f.closeOnce.Do(func() {

		_ = f.dev.Close()
	})
}
