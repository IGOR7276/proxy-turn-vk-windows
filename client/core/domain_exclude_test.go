package core

import (
	"encoding/binary"
	"net"
	"testing"
)

func TestMatchesDomain(t *testing.T) {
	cases := []struct {
		pattern  string
		hostname string
		want     bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "www.example.com", false},
		{"Example.COM", "example.com", true},
		{"example.com", "EXAMPLE.com", true},
		{"*.example.com", "example.com", true},
		{"*.example.com", "www.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "notexample.com", false},
		{"*.example.com", "example.com.evil.net", false},
		{"", "example.com", false},
		{"example.com", "", false},
		{"  example.com  ", " example.com ", true},
	}
	for _, c := range cases {
		if got := MatchesDomain(c.pattern, c.hostname); got != c.want {
			t.Errorf("MatchesDomain(%q, %q) = %v, want %v", c.pattern, c.hostname, got, c.want)
		}
	}
}

// buildDNSResponse собирает минимальный DNS-ответ с одной A-записью.
func buildDNSResponse(id uint16, rcode byte, ancount uint16, ip [4]byte) []byte {
	var b []byte
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], id)
	hdr[2] = 0x81 // QR=1, RD=1
	hdr[3] = 0x80 | (rcode & 0x0F)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], ancount)
	b = append(b, hdr...)

	// Question: example.com A IN
	for _, part := range []string{"example", "com"} {
		b = append(b, byte(len(part)))
		b = append(b, part...)
	}
	b = append(b, 0x00)
	b = append(b, 0x00, 0x01, 0x00, 0x01) // QTYPE=A, QCLASS=IN

	if ancount == 0 {
		return b
	}
	// Answer: name pointer → offset 12, A IN TTL 60, 4 байта IP.
	b = append(b, 0xC0, 0x0C)
	b = append(b, 0x00, 0x01, 0x00, 0x01)
	ttl := make([]byte, 4)
	binary.BigEndian.PutUint32(ttl, 60)
	b = append(b, ttl...)
	b = append(b, 0x00, 0x04)
	b = append(b, ip[:]...)
	return b
}

func TestParseDNSARecords(t *testing.T) {
	resp := buildDNSResponse(0x1234, 0, 1, [4]byte{93, 184, 216, 34})
	ips, err := parseDNSARecords(resp)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ips) != 1 {
		t.Fatalf("получено %d IP, want 1", len(ips))
	}
	if !ips[0].Equal(net.IPv4(93, 184, 216, 34)) {
		t.Fatalf("IP = %v, want 93.184.216.34", ips[0])
	}
}

func TestParseDNSARecordsEmpty(t *testing.T) {
	resp := buildDNSResponse(1, 0, 0, [4]byte{})
	ips, err := parseDNSARecords(resp)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(ips) != 0 {
		t.Fatalf("ожидался пустой список, got %v", ips)
	}
}

func TestParseDNSARecordsRCodeError(t *testing.T) {
	resp := buildDNSResponse(1, 3, 0, [4]byte{}) // NXDOMAIN
	if _, err := parseDNSARecords(resp); err == nil {
		t.Fatal("NXDOMAIN должен возвращать ошибку")
	}
}

func TestParseDNSARecordsShort(t *testing.T) {
	if _, err := parseDNSARecords([]byte{1, 2, 3}); err == nil {
		t.Fatal("короткий ответ должен возвращать ошибку")
	}
}

func TestSkipDNSName(t *testing.T) {
	plain := []byte{0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00, 0xAA}
	if got := skipDNSName(plain, 0); got != 13 {
		t.Fatalf("skipDNSName(plain) = %d, want 13", got)
	}

	ptr := []byte{0xC0, 0x0C, 0xAA}
	if got := skipDNSName(ptr, 0); got != 2 {
		t.Fatalf("skipDNSName(ptr) = %d, want 2", got)
	}

	truncated := []byte{0x07, 'e'}
	if got := skipDNSName(truncated, 0); got != -1 {
		t.Fatalf("skipDNSName(truncated) = %d, want -1", got)
	}

	outOfBounds := []byte{0x01}
	if got := skipDNSName(outOfBounds, 10); got != -1 {
		t.Fatalf("skipDNSName(out of bounds) = %d, want -1", got)
	}
}
