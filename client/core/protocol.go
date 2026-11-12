package core

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// RequestConfig запрашивает WireGuard конфиг через DTLS-соединение.
func RequestConfig(conn net.Conn, localPort, deviceID, password string) (string, error) {
	payload := fmt.Sprintf("GETCONF:%s|%s|%s", localPort, deviceID, password)
	if _, err := conn.Write([]byte(payload)); err != nil {
		return "", fmt.Errorf("отправка GETCONF: %w", err)
	}

	b := make([]byte, 4096)
	if err := conn.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
		return "", fmt.Errorf("установка дедлайна: %w", err)
	}
	var (
		n    int
		err  error
		resp string
	)
	// Пропускаем keepalive-pong (одиночный 0xFF), если он пришёл раньше ответа.
	for {
		n, err = conn.Read(b)
		if err != nil {
			break
		}
		if n == 1 && b[0] == keepaliveByte {
			continue
		}
		resp = string(b[:n])
		break
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		return "", fmt.Errorf("чтение ответа конфига: %w", err)
	}
	if resp == "NOCONF" {
		return "", nil
	}

	if strings.HasPrefix(resp, "DENIED:") {
		reason := strings.TrimPrefix(resp, "DENIED:")
		var authErr error
		switch reason {
		case "wrong_password":
			authErr = fmt.Errorf("FATAL_AUTH: неверный пароль подключения")
		case "expired":
			authErr = fmt.Errorf("FATAL_AUTH: срок действия пароля истёк")
		case "device_mismatch":
			authErr = fmt.Errorf("FATAL_AUTH: пароль привязан к другому устройству")
		default:
			authErr = fmt.Errorf("FATAL_AUTH: доступ запрещён (%s)", reason)
		}
					emitEvent(Event{Type: EventEvent, Name: "fatal_auth", Data: authErr.Error()})
		return "", authErr
	}

	return resp, nil
}

// SendAuth привязывает DTLS-сессию к устройству и паролю. Её шлют воркеры,
// которые не запрашивают конфиг (GETCONF уже авторизует свою сессию).
// Без AUTH сервер считает сессию анонимной и может сразу её закрыть —
// воркер получает EOF через секунду после READY и уходит в цикл
// переподключений. Старые серверы без поддержки AUTH перешлют пакет в
// WireGuard, а тот молча отбросит его как невалидный.
func SendAuth(conn net.Conn, deviceID, password string) error {
	payload := fmt.Sprintf("AUTH:%s|%s", deviceID, password)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write([]byte(payload))
	_ = conn.SetWriteDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("отправка AUTH: %w", err)
	}
	return nil
}



