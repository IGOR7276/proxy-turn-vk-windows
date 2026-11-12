package core

import "strings"

// workerErrorHint возвращает короткую понятную подсказку по тексту ошибки
// воркера. Сырая ошибка остаётся в логе, подсказка добавляется к ней и
// показывается пользователю/уходит в UI.
func workerErrorHint(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "wrap_auth_timeout"):
		return "сервер не ответил на WRAP/DTLS — проверьте пароль, IP/порт и что wdtt-server запущен"
	case strings.Contains(text, "context canceled"):
		return "соединение прервано до handshake — часто сервер недоступен, UDP режет оператор или сменилась сеть"
	case strings.Contains(text, "context deadline exceeded"):
		return "таймаут handshake — сервер не отвечает, проверьте VPS, файрвол и пароль WRAP"
	case strings.Contains(text, "deadline exceeded") || strings.Contains(text, "timeout") || strings.Contains(text, "i/o timeout"):
		return "таймаут — сервер не отвечает, проверьте доступность VPS и пароль"
	case strings.Contains(text, "connection refused"):
		return "сервер отклонил подключение — проверьте IP, порт DTLS и что wdtt-server запущен"
	case strings.Contains(text, "connection reset"):
		return "сервер сбросил соединение — возможен неверный пароль WRAP или перезапуск сервера"
	case strings.Contains(text, "no route") || strings.Contains(text, "network is unreachable"):
		return "нет маршрута до сервера — проверьте интернет; отключите другие VPN/прокси"
	case strings.Contains(text, "lookup") || strings.Contains(text, "no such host"):
		return "DNS не резолвит адрес — смените DNS в настройках сети"
	case strings.Contains(text, "turn квота") || strings.Contains(text, "quota") || strings.Contains(text, "error 486"):
		return "VK исчерпал TURN-слоты — уменьшите число потоков или смените VK-хеш/аккаунт"
	case strings.Contains(text, "turn allocate"):
		return "ошибка TURN relay — VK может резать UDP; попробуйте другой хеш или режим капчи"
	case strings.Contains(text, "rate limit") || strings.Contains(text, "flood") || strings.Contains(text, "error 29"):
		return "VK временно ограничил запросы — подождите или смените IP/хеш"
	case strings.Contains(text, "rtp aead") || strings.Contains(text, "auth failed") || strings.Contains(text, "tag mismatch"):
		return "ошибка WRAP/RTP — неверный пароль или несовместимая версия сервера"
	case strings.Contains(text, "fatal_auth") || strings.Contains(text, "неверный пароль") || strings.Contains(text, "хеш мёртв"):
		return "неверный пароль подключения, мёртвый хеш или истёк срок действия"
	case strings.Contains(text, "cannot create socket"):
		return "не удалось открыть UDP-сокет — ограничение прошивки или нет сети"
	case strings.Contains(text, "captcha_wait_required") || strings.Contains(text, "captcha"):
		return "VK требует капчу — подождите около минуты или смените режим капчи"
	default:
		return ""
	}
}
