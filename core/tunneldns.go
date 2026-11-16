package core

import (
	"encoding/binary"
	"net/netip"

	"golang.org/x/net/dns/dnsmessage"
)

// TunnelDNSAnswer — чёрный режим, поддомены: pkt — IPv4-пакет из туннеля
// к ОС. Если это ответ DNS (UDP с порта 53) от одного из dns и спрошенное
// имя или любое имя цепочки CNAME попадает в m — адреса A из ответа, их
// платформа маршрутизирует в туннель ДО выдачи пакета ОС. Всё, что не
// разобралось, — nil (пакет доставляется как есть в любом случае).
//
// Общее для платформ; разбор тот же, что у кота 6 в platform/windows/
// tunnelonly.go (udpFromDNS + matchedAnswers).
func TunnelDNSAnswer(pkt []byte, dns map[netip.Addr]bool, m *DomainMatcher) []netip.Addr {
	if m.Empty() {
		return nil
	}
	payload, ok := udpFromDNS(pkt, dns)
	if !ok {
		return nil
	}
	return matchedDNSAnswers(payload, m)
}

// DNSAnswerMatches — то же, что TunnelDNSAnswer, для уже извлечённого
// сообщения DNS (свой посредник DNS платформы, без IP/UDP-обёртки).
func DNSAnswerMatches(msg []byte, m *DomainMatcher) []netip.Addr {
	if m.Empty() {
		return nil
	}
	return matchedDNSAnswers(msg, m)
}

func udpFromDNS(pkt []byte, dns map[netip.Addr]bool) ([]byte, bool) {
	if len(pkt) < 28 || pkt[0]>>4 != 4 || pkt[9] != 17 {
		return nil, false
	}
	ihl := int(pkt[0]&0x0f) * 4
	if ihl < 20 || len(pkt) < ihl+8 {
		return nil, false
	}
	// Фрагменты не разбираем (ответы DNS укладываются в один пакет).
	if binary.BigEndian.Uint16(pkt[6:8])&0x3fff != 0 {
		return nil, false
	}
	src := netip.AddrFrom4([4]byte(pkt[12:16]))
	if !dns[src] || binary.BigEndian.Uint16(pkt[ihl:ihl+2]) != 53 {
		return nil, false
	}
	return pkt[ihl+8:], true
}

func matchedDNSAnswers(msg []byte, m *DomainMatcher) []netip.Addr {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil || !h.Response || h.RCode != dnsmessage.RCodeSuccess {
		return nil
	}
	hit := false
	qs, err := p.AllQuestions()
	if err != nil {
		return nil
	}
	for _, q := range qs {
		if m.Match(q.Name.String()) {
			hit = true
		}
	}
	var addrs []netip.Addr
	for {
		ah, err := p.AnswerHeader()
		if err != nil {
			break
		}
		switch ah.Type {
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return nil
			}
			if m.Match(ah.Name.String()) {
				hit = true
			}
			addrs = append(addrs, netip.AddrFrom4(r.A))
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return nil
			}
			if m.Match(ah.Name.String()) || m.Match(r.CNAME.String()) {
				hit = true
			}
		default:
			if err := p.SkipAnswer(); err != nil {
				return nil
			}
		}
	}
	if !hit {
		return nil
	}
	return addrs
}

// TunnelOnlyRoutable — адрес годится в маршрут «в туннель»: публичный
// IPv4 (не локальная сеть, не петля, не служебные).
func TunnelOnlyRoutable(a netip.Addr) bool {
	return a.Is4() && !a.IsLoopback() && !a.IsPrivate() && !a.IsLinkLocalUnicast() && !a.IsMulticast() && !a.IsUnspecified()
}
