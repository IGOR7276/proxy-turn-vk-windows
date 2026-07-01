package core

import (
	"fmt"
	"strings"
)

// patchWGConfig подготавливает сырой конфиг, пришедший от сервера, к установке
// в in-process WireGuard.
//
// Что делает:
//   - Удаляет строку `DNS = ...` из [Interface] (мы не хотим, чтобы WG перехватывал
//     системный DNS — это ломает локальный резолв и замедляет обращения к VK API).
//   - Добавляет `MTU = <mtu>` в [Interface] (1280 по умолчанию).
//
// Не делаем split 0.0.0.0/0 → 2× /1, как в PWDTT: in-process wireguard-go
// нормально принимает 0.0.0.0/0 в AllowedIPs, split нужен только для
// wireguard.exe из официального клиента.
func patchWGConfig(raw string, mtu int) string {
	if mtu <= 0 {
		mtu = 1280
	}
	mtuLine := fmt.Sprintf("MTU = %d", mtu)
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines)+2)
	inInterface := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "[Interface]" {
			inInterface = true
		} else if strings.HasPrefix(trimmed, "[") {
			inInterface = false
		}

		if inInterface {
			if strings.HasPrefix(strings.ToLower(trimmed), "dns") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "mtu") {
				continue
			}
		}

		if trimmed == "[Interface]" {
			out = append(out, line, mtuLine)
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
