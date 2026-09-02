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

// emitEventPtr — глобальный эмиттер событий (captcha_required, dtls_ok и т.п.).
// Устанавливается ядром при старте через SetEmitEvent и сбрасывается при Stop.
//
// Хранится атомарно: раньше это была обычная переменная `EmitEvent`, и
// паттерн `if EmitEvent != nil { EmitEvent(...) }` из воркер-горутин гонялся
// с `EmitEvent = nil` в Core.Stop(). Между проверкой и вызовом переменная
// успевала обнулиться → nil-func call → паника всего процесса. Теперь
// указатель читается ровно один раз.
var emitEventPtr atomic.Pointer[func(Event)]

// SetEmitEvent устанавливает (или сбрасывает, если nil) глобальный эмиттер.
func SetEmitEvent(f func(Event)) {
	if f == nil {
		emitEventPtr.Store(nil)
		return
	}
	emitEventPtr.Store(&f)
}

// emitEvent безопасно вызывает текущий эмиттер (no-op, если он не задан).
func emitEvent(ev Event) {
	if p := emitEventPtr.Load(); p != nil {
		(*p)(ev)
	}
}

// drainCaptchaResult — выкидывает устаревший токен из канала, если там что-то есть.
func drainCaptchaResult() {
	select {
	case <-CaptchaResultChan:
	default:
	}
}

