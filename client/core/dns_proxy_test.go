package core

import (
	"encoding/binary"
	"net"
	"testing"
)

// makeDNSQuery собирает минимальный DNS-запрос для тестов.
func makeDNSQuery(qid uint16, name string, qtype uint16) []byte {
	q := make([]byte, 12)
	binary.BigEndian.PutUint16(q[0:2], qid)
	binary.BigEndian.PutUint16(q[4:6], 1) // QDCOUNT
	for _, label := range splitLabels(name) {
		q = append(q, byte(len(label)))
		q = append(q, label...)
	}
	q = append(q, 0)
	q = binary.BigEndian.AppendUint16(q, qtype)
	q = binary.BigEndian.AppendUint16(q, 1) // IN
	return q
}

func splitLabels(name string) []string {
	var labels []string
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			labels = append(labels, name[start:i])
			start = i + 1
		}
	}
	if start < len(name) {
		labels = append(labels, name[start:])
	}
	return labels
}

func TestBuildDNSResponseA(t *testing.T) {
	query := makeDNSQuery(0x1234, "example.com", 1)
	ips := []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("2001:db8::1")}
	resp := buildDNSResponseForQuery(query, ips)
	if resp == nil {
		t.Fatal("ожидался ответ, получен nil")
	}
	if binary.BigEndian.Uint16(resp[0:2]) != 0x1234 {
		t.Fatalf("qid не скопирован: %x", resp[0:2])
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 1 {
		t.Fatalf("ANCOUNT=%d, ожидалось 1 (только IPv4)", an)
	}
	// QTYPE в answer-секции должен быть A (1).
	// question заканчивается перед answer; ищем пару 0x00 0x01 после QCLASS.
	if resp[len(resp)-16] != 0xC0 || resp[len(resp)-15] != 0x0C {
		t.Fatalf("answer NAME не указывает на QNAME")
	}
	if binary.BigEndian.Uint16(resp[len(resp)-14:len(resp)-12]) != 1 {
		t.Fatalf("ожидалась A-запись")
	}
}

func TestBuildDNSResponseAAAA(t *testing.T) {
	query := makeDNSQuery(0xBEEF, "example.com", 28)
	ips := []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("2001:db8::1")}
	resp := buildDNSResponseForQuery(query, ips)
	if resp == nil {
		t.Fatal("ожидался ответ, получен nil")
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 1 {
		t.Fatalf("ANCOUNT=%d, ожидалось 1 (только IPv6)", an)
	}
	if binary.BigEndian.Uint16(resp[len(resp)-26:len(resp)-24]) != 28 {
		t.Fatalf("ожидалась AAAA-запись")
	}
	if rdlen := binary.BigEndian.Uint16(resp[len(resp)-18 : len(resp)-16]); rdlen != 16 {
		t.Fatalf("RDLENGTH=%d, ожидалось 16", rdlen)
	}
}

func TestBuildDNSResponseAAAAEmpty(t *testing.T) {
	query := makeDNSQuery(7, "example.com", 28)
	resp := buildDNSResponseForQuery(query, []net.IP{net.ParseIP("1.2.3.4")})
	if resp == nil {
		t.Fatal("для AAAA без IPv6 ожидался пустой NOERROR, получен nil")
	}
	if an := binary.BigEndian.Uint16(resp[6:8]); an != 0 {
		t.Fatalf("ANCOUNT=%d, ожидалось 0", an)
	}
}

func TestBuildDNSResponseOtherType(t *testing.T) {
	query := makeDNSQuery(1, "example.com", 15) // MX
	if resp := buildDNSResponseForQuery(query, []net.IP{net.ParseIP("1.2.3.4")}); resp != nil {
		t.Fatal("для MX-запроса ожидался nil (forward на upstream)")
	}
}
