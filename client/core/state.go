package core

import (
	"strings"
	"sync/atomic"
)

// CaptchaResultChan — канал для получения токена капчи из внешнего решателя.
// Wails backend читает токен из WebView и пишет в этот канал через
// (c *Core) SolveCaptcha(token). creds.go ждёт ответа.
var CaptchaResultChan = make(chan string, 1)

var captchaModeValue atomic.Value

func init() {
	captchaModeValue.Store("auto")
}

// normalizeCaptchaMode — допустимые значения: auto, rjs, wv.
func normalizeCaptchaMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto", "rjs", "wv":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}

// setCaptchaMode — устанавливает режим, возвращает нормализованное значение.
func setCaptchaMode(mode string) string {
	normalized := normalizeCaptchaMode(mode)
	captchaModeValue.Store(normalized)
	return normalized
}

// getCaptchaMode — текущий режим, "auto" если не задан.
func getCaptchaMode() string {
	mode, _ := captchaModeValue.Load().(string)
	if mode == "" {
		return "auto"
	}
	return mode
}

// WebViewCaptchaHandler вызывается когда нужна WebView-капча.
// Если nil, используется стандартный stdout+CaptchaResultChan путь (CLI/Android).
// Хендлер должен вернуть success_token или ошибку.
var WebViewCaptchaHandler func(mode, redirectURI, sessionToken string) (string, error)

// EmitEvent — глобальный эмиттер событий, используется для captcha_required и т.п.
// Устанавливается ядром при старте.
var EmitEvent func(Event)

// drainCaptchaResult — выкидывает устаревший токен из канала, если там что-то есть.
func drainCaptchaResult() {
	select {
	case <-CaptchaResultChan:
	default:
	}
}

