package core

import (
	"strconv"
	"strings"
)

// patchWGConfig подготавливает сырой конфиг, пришедший от сервера, к установке
// в in-process WireGuard.
//
// Что делает:
//   - Удаляет строку `DNS = ...` из [Interface] (мы не хотим, чтобы WG перехватывал
//     системный DNS — это ломает локальный резолв и замедляет обращения к VK API).
//   - Добавляет `MTU = 1280` в [Interface], если его нет (1280 — стандарт для
//     туннелей через TURN, у которых типичный MTU ~1300-1400).
//
// Не делаем split 0.0.0.0/0 → 2× /1, как в PWDTT: in-process wireguard-go
// нормально принимает 0.0.0.0/0 в AllowedIPs, split нужен только для
// wireguard.exe из официального клиента.
func patchWGConfig(raw string) string {
	const defaultMTU = 1280
	mtuLine := "MTU = " + strconv.Itoa(defaultMTU)

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
			// Убираем любые формы DNS-строк (DNS, DNS =, dns = 1.1.1.1)
			if strings.HasPrefix(strings.ToLower(trimmed), "dns") {
				continue
			}
			// Не дублируем MTU, если сервер его уже прислал
			if strings.HasPrefix(strings.ToLower(trimmed), "mtu") {
				continue
			}
		}

		// Вставляем MTU сразу после [Interface]
		if trimmed == "[Interface]" {
			out = append(out, line, mtuLine)
			continue
		}

		out = append(out, line)
	}

	return strings.Join(out, "\n")
}
