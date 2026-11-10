package core

import (
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func dnsQuery(name string, qtype uint16) []byte {
	q := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			q = append(q, byte(i-start))
			q = append(q, name[start:i]...)
			start = i + 1
		}
	}
	q = append(q, 0, byte(qtype>>8), byte(qtype), 0, 1)
	return q
}

// answerTypes разбирает ответ и возвращает QTYPE вопроса и типы записей ответа.
func answerTypes(t *testing.T, resp []byte) (uint16, []uint16) {
	t.Helper()
	if len(resp) < 12 || resp[2]&0x80 == 0 || resp[3]&0x0F != 0 {
		t.Fatalf("не ответ или RCODE≠0: % x", resp)
	}
	an := int(binary.BigEndian.Uint16(resp[6:8]))
	qtype, ok := dnsQueryType(resp)
	if !ok {
		t.Fatalf("не разобран вопрос: % x", resp)
	}
	off := 12
	for resp[off] != 0 {
		off += 1 + int(resp[off])
	}
	off += 5
	var types []uint16
	for i := 0; i < an; i++ {
		if off+12 > len(resp) {
			t.Fatalf("ANCOUNT=%d, а записей меньше: % x", an, resp)
		}
		types = append(types, binary.BigEndian.Uint16(resp[off+2:off+4]))
		rdlen := int(binary.BigEndian.Uint16(resp[off+10 : off+12]))
		off += 12 + rdlen
	}
	if off != len(resp) {
		t.Fatalf("лишние байты после ответа: %d из %d", off, len(resp))
	}
	return qtype, types
}

func TestBuildDNSResponseCountsOnlyIPv4(t *testing.T) {
	q := dnsQuery("rasp.yandex.ru", dnsTypeA)
	resp := buildDNSResponseWithA(q, []net.IP{net.ParseIP("87.250.250.158"), net.ParseIP("2a02:6b8::2:242")})
	qt, types := answerTypes(t, resp)
	if qt != dnsTypeA || len(types) != 1 || types[0] != dnsTypeA {
		t.Fatalf("qtype=%d answers=%v, нужен один A", qt, types)
	}
}

// Прокси с исключённым доменом: ответ берём из сокета клиента.
func excludedProxyAnswer(t *testing.T, qtype uint16, calls *atomic.Int32) []byte {
	t.Helper()
	ln, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	p := newDNSProxy([]string{"192.0.2.1"}, "") // TEST-NET: пересылка не ответит
	p.timeout = 300 * time.Millisecond
	p.listener = ln
	p.SetExcludedDomainHandler(func(string) []net.IP {
		calls.Add(1)
		return []net.IP{net.ParseIP("77.88.44.242")}
	})
	p.handleQuery(dnsQuery("ya.ru", qtype), client.LocalAddr())

	buf := make([]byte, 1500)
	client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	n, _, err := client.ReadFrom(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

func TestExcludedDomainA(t *testing.T) {
	var calls atomic.Int32
	resp := excludedProxyAnswer(t, dnsTypeA, &calls)
	if resp == nil {
		t.Fatal("нет ответа на A")
	}
	qt, types := answerTypes(t, resp)
	if qt != dnsTypeA || len(types) != 1 || types[0] != dnsTypeA {
		t.Fatalf("qtype=%d answers=%v", qt, types)
	}
}

// На AAAA исключённого домена раньше приходили A-записи — строгие
// резолверы (например, curl из Git Bash) отбрасывали ответ целиком.
func TestExcludedDomainAAAAIsNoData(t *testing.T) {
	var calls atomic.Int32
	resp := excludedProxyAnswer(t, dnsTypeAAAA, &calls)
	if resp == nil {
		t.Fatal("нет ответа на AAAA")
	}
	qt, types := answerTypes(t, resp)
	if qt != dnsTypeAAAA || len(types) != 0 {
		t.Fatalf("qtype=%d answers=%v, нужен пустой ответ", qt, types)
	}
}

// Прочие типы (HTTPS=65) не подменяются — уходят к upstream.
func TestExcludedDomainOtherTypesForwarded(t *testing.T) {
	var calls atomic.Int32
	if resp := excludedProxyAnswer(t, 65, &calls); resp != nil {
		t.Fatalf("прокси сам ответил на HTTPS: % x", resp)
	}
	if calls.Load() != 0 {
		t.Fatal("обработчик исключений вызван для HTTPS")
	}
}
