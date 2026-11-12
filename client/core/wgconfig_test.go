package core

import (
	"strings"
	"testing"
)

const sampleWG = `[Interface]
PrivateKey = aGVsbG8=
Address = 10.66.0.32/32
DNS = 8.8.8.8, 1.1.1.1
MTU = 1420

[Peer]
PublicKey = d29ybGQ=
AllowedIPs = 0.0.0.0/0
Endpoint = 1.2.3.4:51820
`

// TestPatchWGConfigRemovesDNS — DNS из [Interface] должен исчезнуть, иначе
// WireGuard перехватывает системный резолв и ломает VK API.
func TestPatchWGConfigRemovesDNS(t *testing.T) {
	out := patchWGConfig(sampleWG, 1280)
	if strings.Contains(strings.ToLower(out), "dns") {
		t.Fatalf("DNS остался в конфиге:\n%s", out)
	}
}

// TestPatchWGConfigSetsMTU — MTU заменяется, а не дублируется.
func TestPatchWGConfigSetsMTU(t *testing.T) {
	out := patchWGConfig(sampleWG, 1280)
	if !strings.Contains(out, "MTU = 1280") {
		t.Fatalf("MTU = 1280 не найден:\n%s", out)
	}
	if strings.Contains(out, "MTU = 1420") {
		t.Fatalf("старый MTU не удалён:\n%s", out)
	}
	if n := strings.Count(out, "MTU ="); n != 1 {
		t.Fatalf("MTU встречается %d раз, want 1:\n%s", n, out)
	}
}

// TestPatchWGConfigDefaultMTU — mtu <= 0 превращается в 1280.
func TestPatchWGConfigDefaultMTU(t *testing.T) {
	for _, mtu := range []int{0, -1} {
		out := patchWGConfig(sampleWG, mtu)
		if !strings.Contains(out, "MTU = 1280") {
			t.Fatalf("mtu=%d: ожидался дефолт 1280:\n%s", mtu, out)
		}
	}
}

// TestPatchWGConfigKeepsPeer — секция [Peer] не должна пострадать: DNS-фильтр
// работает только внутри [Interface].
func TestPatchWGConfigKeepsPeer(t *testing.T) {
	raw := `[Interface]
PrivateKey = key
[Peer]
PublicKey = peer
DNS = not-a-real-peer-field
AllowedIPs = 0.0.0.0/0
`
	out := patchWGConfig(raw, 1280)
	if !strings.Contains(out, "DNS = not-a-real-peer-field") {
		t.Fatalf("строка из [Peer] удалена:\n%s", out)
	}
}

// TestPatchWGConfigCaseInsensitive — dns/mtu в нижнем регистре тоже удаляются.
func TestPatchWGConfigCaseInsensitive(t *testing.T) {
	raw := "[Interface]\ndns = 8.8.8.8\nmtu = 1400\nPrivateKey = k\n"
	out := patchWGConfig(raw, 1300)
	if strings.Contains(out, "dns =") || strings.Contains(out, "mtu = 1400") {
		t.Fatalf("нижний регистр не обработан:\n%s", out)
	}
	if !strings.Contains(out, "MTU = 1300") {
		t.Fatalf("MTU не добавлен:\n%s", out)
	}
}

// TestPatchWGConfigNoInterface — конфиг без [Interface] не должен паниковать.
func TestPatchWGConfigNoInterface(t *testing.T) {
	out := patchWGConfig("[Peer]\nPublicKey = x\n", 1280)
	if !strings.Contains(out, "[Peer]") {
		t.Fatalf("секция потеряна:\n%s", out)
	}
	if strings.Contains(out, "MTU =") {
		t.Fatalf("MTU не должен добавляться без [Interface]:\n%s", out)
	}
}
