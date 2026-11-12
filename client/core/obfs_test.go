package core

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func testObfsKey() []byte {
	key := make([]byte, wrapKeyLen)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

// TestObfsRoundTrip — базовый сценарий: wrap → unwrap возвращает исходные байты.
func TestObfsRoundTrip(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()
	payload := []byte("wireguard handshake packet payload")

	wire, err := obfsWrapPacket(key, payload, cfg, state)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !obfsIsRTPPacket(wire) {
		t.Fatalf("wrapped packet не распознан как RTP")
	}

	dst := make([]byte, 2048)
	n, err := obfsUnwrapPacket(key, wire, dst)
	if err != nil {
		t.Fatalf("unwrap: %v", err)
	}
	if !bytes.Equal(dst[:n], payload) {
		t.Fatalf("payload не совпал: got %q want %q", dst[:n], payload)
	}
}

// TestObfsSequenceIncrements — каждый пакет получает новый seq/timestamp,
// иначе nonce повторяется и AEAD ломается.
func TestObfsSequenceIncrements(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()

	first, err := obfsWrapPacket(key, []byte("a"), cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	second, err := obfsWrapPacket(key, []byte("b"), cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first[2:4], second[2:4]) {
		t.Fatalf("seq не инкрементится: %x == %x", first[2:4], second[2:4])
	}
}

// TestObfsWrongKeyFails — чужой ключ не должен проходить аутентификацию.
func TestObfsWrongKeyFails(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()

	wire, err := obfsWrapPacket(key, []byte("secret"), cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	wrong := testObfsKey()
	wrong[0] ^= 0xFF

	dst := make([]byte, 2048)
	if _, err := obfsUnwrapPacket(wrong, wire, dst); err == nil {
		t.Fatal("unwrap с неверным ключом не вернул ошибку")
	}
}

// TestObfsTamperFails — изменение байта шифротекста должно ломать Poly1305.
func TestObfsTamperFails(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()

	wire, err := obfsWrapPacket(key, []byte("secret payload"), cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	wire[13] ^= 0xFF // байт внутри шифротекста

	dst := make([]byte, 2048)
	if _, err := obfsUnwrapPacket(key, wire, dst); err == nil {
		t.Fatal("unwrap повреждённого пакета не вернул ошибку")
	}
}

// TestObfsVideoPayloadType — режим video использует PT 96, audio — 111.
func TestObfsVideoPayloadType(t *testing.T) {
	key := testObfsKey()
	state := NewObfsState()

	audio := NewObfsConfig("audio")
	if audio.PayloadType != 111 {
		t.Fatalf("audio PT = %d, want 111", audio.PayloadType)
	}
	video := NewObfsConfig("video")
	if video.PayloadType != 96 {
		t.Fatalf("video PT = %d, want 96", video.PayloadType)
	}

	wire, err := obfsWrapPacket(key, []byte("x"), video, state)
	if err != nil {
		t.Fatal(err)
	}
	if got := wire[1] & 0x7F; got != 96 {
		t.Fatalf("PT в заголовке = %d, want 96", got)
	}
}

// TestObfsPaddingPresent — пакет всегда содержит байт длины паддинга,
// а паддинг не мешает распаковке.
func TestObfsPaddingPresent(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()
	payload := []byte("hello")

	for i := 0; i < 32; i++ {
		wire, err := obfsWrapPacket(key, payload, cfg, state)
		if err != nil {
			t.Fatalf("wrap #%d: %v", i, err)
		}
		if wire[0]&0x20 == 0 {
			t.Fatalf("P-бит не выставлен")
		}
		padLen := int(wire[len(wire)-1])
		if padLen < 1 || padLen > cfg.PaddingMax {
			t.Fatalf("padLen = %d вне диапазона 1..%d", padLen, cfg.PaddingMax)
		}
		dst := make([]byte, 2048)
		n, err := obfsUnwrapPacket(key, wire, dst)
		if err != nil {
			t.Fatalf("unwrap #%d: %v", i, err)
		}
		if !bytes.Equal(dst[:n], payload) {
			t.Fatalf("payload #%d не совпал", i)
		}
	}
}

// TestObfsInvalidInput — пустой payload, короткий пакет и неверный ключ.
func TestObfsInvalidInput(t *testing.T) {
	cfg := NewObfsConfig("audio")
	state := NewObfsState()

	if _, err := obfsWrapPacket([]byte("short"), []byte("x"), cfg, state); err == nil {
		t.Fatal("короткий ключ должен давать ошибку")
	}
	if _, err := obfsWrapPacket(testObfsKey(), nil, cfg, state); err == nil {
		t.Fatal("пустой payload должен давать ошибку")
	}
	if _, err := obfsUnwrapPacket(testObfsKey(), []byte{0x80, 0x6F}, make([]byte, 16)); err == nil {
		t.Fatal("короткий пакет должен давать ошибку")
	}
	if _, err := obfsUnwrapPacket(testObfsKey(), []byte{0x00, 0x6F, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, make([]byte, 16)); err == nil {
		t.Fatal("пакет не-RTP v2 должен давать ошибку")
	}
}

// TestObfsIsRTPPacket — детектор не должен путать мусор с нашим трафиком.
func TestObfsIsRTPPacket(t *testing.T) {
	key := testObfsKey()
	wire, err := obfsWrapPacket(key, []byte("x"), NewObfsConfig("audio"), NewObfsState())
	if err != nil {
		t.Fatal(err)
	}
	if !obfsIsRTPPacket(wire) {
		t.Fatal("валидный пакет не распознан")
	}
	if obfsIsRTPPacket(nil) {
		t.Fatal("nil не должен распознаваться")
	}
	if obfsIsRTPPacket([]byte{0x80, 0x08, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatal("чужой PT не должен распознаваться")
	}
}

// TestObfsReplayWindow — повтор и пакет из далёкого прошлого отбрасываются,
// а обычная последовательность проходит.
func TestObfsReplayWindow(t *testing.T) {
	key := testObfsKey()
	cfg := NewObfsConfig("audio")
	state := NewObfsState()

	var w replayWindow
	var first []byte
	for i := 0; i < 3; i++ {
		wire, err := obfsWrapPacket(key, []byte{byte(i)}, cfg, state)
		if err != nil {
			t.Fatalf("wrap: %v", err)
		}
		if i == 0 {
			first = append([]byte(nil), wire...)
		}
		if !w.accept(wire) {
			t.Fatalf("пакет %d должен быть принят", i)
		}
	}
	if w.accept(first) {
		t.Fatal("повтор первого пакета должен быть отброшен")
	}

	// Пакет, сдвинутый за пределы окна, тоже отбрасывается.
	old := append([]byte(nil), first...)
	oldTs := binary.BigEndian.Uint32(old[4:8]) - uint32(replayWindowSpan) - 1
	binary.BigEndian.PutUint32(old[4:8], oldTs)
	if w.accept(old) {
		t.Fatal("пакет старше окна должен быть отброшен")
	}
}

func TestNormalizeObfsMode(t *testing.T) {
	cases := map[string]string{
		"video":  "video",
		"VIDEO":  "video",
		" video": "video",
		"audio":  "audio",
		"":       "audio",
		"junk":   "audio",
	}
	for in, want := range cases {
		if got := NormalizeObfsMode(in); got != want {
			t.Errorf("NormalizeObfsMode(%q) = %q, want %q", in, got, want)
		}
	}
}
