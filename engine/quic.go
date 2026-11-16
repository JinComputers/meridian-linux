package engine

import "encoding/binary"

// QUIC (UDP на порт 443) В ТУННЕЛЕ НЕ ПРОХОДИТ ПО ПОСТРОЕНИЮ.
//
// QUIC требует, чтобы путь пропускал датаграммы не меньше 1200 байт
// полезной нагрузки (RFC 9000, 14.1): первый пакет клиента добивается до
// 1200. У нас MTU туннеля 1100 (без DTLS) и 1200 (с DTLS) — с заголовками
// IP и UDP такой пакет не влезает ни в один из них. Ядро режет его на
// фрагменты, ответы сервера крупнее нашего MTU, и рукопожатие QUIC
// виснет, пока приложение не сдастся и не уйдёт на TCP.
//
// Клиент 15.10: модифицированный YouTube через наш туннель запускал ролик
// через ~20 с, через AmneziaVPN (MTU больше) — сразу. Браузер не страдал:
// Chrome гоняет TCP параллельно с QUIC и не ждёт.
//
// Поэтому на UDP/443 отвечаем СРАЗУ ICMP «порт недоступен»: приложение
// узнаёт о провале за миллисекунды, а не по тайм-ауту, и идёт на TCP,
// который через туннель работает. Терять нечего: QUIC через туннель с
// нашим MTU не заработал бы всё равно.

// quicPort — порт QUIC/HTTP3.
const quicPort = 443

// isQUICv4 — IPv4-пакет UDP на порт 443, первый (или единственный)
// фрагмент. Фрагменты с ненулевым смещением портов не несут — их не
// трогаем.
func isQUICv4(p []byte) bool {
	if len(p) < 20 || p[0]>>4 != 4 {
		return false
	}
	ihl := int(p[0]&0x0f) * 4
	if ihl < 20 || len(p) < ihl+8 {
		return false
	}
	if p[9] != 17 { // UDP
		return false
	}
	fragOff := binary.BigEndian.Uint16(p[6:8]) & 0x1fff
	if fragOff != 0 {
		return false
	}
	dport := binary.BigEndian.Uint16(p[ihl+2 : ihl+4])
	return dport == quicPort
}

// icmpPortUnreachableV4 — ответ ICMP «порт недоступен» (тип 3, код 3) на
// пакет p: в теле — заголовок IP исходного пакета и первые 8 байт его
// данных, как требует RFC 792. Адреса переставлены: ответ «от» того, кому
// слали. nil — пакет разобрать нельзя.
func icmpPortUnreachableV4(p []byte) []byte {
	if len(p) < 20 {
		return nil
	}
	ihl := int(p[0]&0x0f) * 4
	if ihl < 20 || len(p) < ihl+8 {
		return nil
	}
	quote := p[:ihl+8]
	total := 20 + 8 + len(quote)
	out := make([]byte, total)

	// Заголовок IPv4.
	out[0] = 0x45
	binary.BigEndian.PutUint16(out[2:4], uint16(total))
	out[8] = 64                // TTL
	out[9] = 1                 // ICMP
	copy(out[12:16], p[16:20]) // от: адрес назначения исходного
	copy(out[16:20], p[12:16]) // кому: отправитель исходного
	binary.BigEndian.PutUint16(out[10:12], inetChecksum(out[:20]))

	// ICMP: тип 3 (адресат недоступен), код 3 (порт недоступен).
	icmp := out[20:]
	icmp[0] = 3
	icmp[1] = 3
	copy(icmp[8:], quote)
	binary.BigEndian.PutUint16(icmp[2:4], inetChecksum(icmp))
	return out
}

// inetChecksum — общая, объявлена в speedprobe.go.
