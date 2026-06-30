//go:build windows

package backend

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// SolveCaptchaWebView запускает WebView2-решатель капчи.
// Блокирует до получения токена или таймаута.
// Должен вызываться из goroutine с COM STA (делает runtime.LockOSThread).
func SolveCaptchaWebView(mode, redirectURI, sessionToken string) (string, error) {
	if mode == "" {
		mode = "auto"
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	defer windows.CoUninitialize()

	// Регистрируем класс окна
	hInst := win.GetModuleHandle(nil)
	className, _ := syscall.UTF16PtrFromString(fmt.Sprintf("WdttCaptchaWv_%d", time.Now().UnixNano()))
	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		Style:         win.CS_HREDRAW | win.CS_VREDRAW,
		LpfnWndProc:   windows.NewCallback(captchaWndProc),
		HInstance:     hInst,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(32512)),
		HbrBackground: win.HBRUSH(win.COLOR_WINDOW + 1),
		LpszClassName: className,
	}
	if atom := win.RegisterClassEx(&wc); atom == 0 {
		return "", fmt.Errorf("RegisterClassEx failed")
	}
	defer win.UnregisterClass(className)

	isManual := mode == "manual" || mode == "selected"
	timeout := 35 * time.Second
	if isManual {
		timeout = 120 * time.Second
	}

	// Создаём окно (скрытое для auto, видимое для manual)
	hwnd := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		className,
		syscall.StringToUTF16Ptr("WDTT Captcha"),
		win.WS_OVERLAPPED|win.WS_CAPTION|win.WS_SYSMENU,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT, 900, 750,
		0, 0, hInst, nil,
	)
	if hwnd == 0 {
		return "", fmt.Errorf("CreateWindowEx failed")
	}
	defer win.DestroyWindow(hwnd)

	chrome := edge.NewChromium()
	chrome.SetErrorCallback(func(err error) {
		log.Printf("[CAPTCHA WV] Chromium error: %v", err)
	})
	*(*uintptr)(unsafe.Pointer(chrome)) = uintptr(hwnd)
	defer func() {
		*(*uintptr)(unsafe.Pointer(chrome)) = 0
		chrome.MessageCallback = nil
		chrome.SetErrorCallback(nil)
	}()

	resultCh := make(chan captchaResult, 1)

	webviewloader.CreateCoreWebView2EnvironmentWithOptions(
		&captchaEnvHandler{chrome: chrome, hwnd: hwnd, result: resultCh},
		webviewloader.WithUserDataFolder(filepath.Join(configDir(), "webview2_captcha")),
	)

	// Ждём контроллер
	for chrome.GetController() == nil {
		select {
		case r := <-resultCh:
			if r.err != nil {
				return "", r.err
			}
		default:
		}
		var msg win.MSG
		for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctrl := chrome.GetController()
	webview, err := ctrl.GetCoreWebView2()
	if err != nil {
		return "", fmt.Errorf("GetCoreWebView2: %w", err)
	}

	settings, err := webview.GetSettings()
	if err == nil {
		settings.PutAreDevToolsEnabled(false)
		settings.PutAreDefaultContextMenusEnabled(false)
	}
	if isManual {
		ctrl.PutIsVisible(true)
		win.ShowWindow(hwnd, win.SW_SHOW)
		win.SetForegroundWindow(hwnd)
	} else {
		ctrl.PutIsVisible(false)
	}
	bounds := edge.Rect{Left: 0, Top: 0, Right: 900, Bottom: 750}
	ctrl.PutBounds(bounds)

	// JS interceptor
	interceptorJS := `
(function() {
    if (window.__wdtt_captcha) return;
    window.__wdtt_captcha = true;
    function onSuccess(token) {
        window.chrome.webview.postMessage(JSON.stringify({type:'captcha_result', token: token}));
    }
    function onSlider() {
        window.chrome.webview.postMessage(JSON.stringify({type:'captcha_slider'}));
    }
    var origFetch = window.fetch;
    window.fetch = function() {
        var url = arguments[0] || '';
        if (typeof url === 'string' && url.indexOf('captchaNotRobot.check') !== -1) {
            return origFetch.apply(this, arguments).then(function(response) {
                var clone = response.clone();
                clone.json().then(function(data) {
                    if (data.response && data.response.success_token) {
                        onSuccess(data.response.success_token);
                    } else if (data.response && data.response.show_captcha_type === 'slider') {
                        onSlider();
                    }
                }).catch(function(){});
                return response;
            });
        }
        return origFetch.apply(this, arguments);
    };
    var origOpen = XMLHttpRequest.prototype.open;
    XMLHttpRequest.prototype.open = function(method, url) {
        this._wdtt_url = url;
        return origOpen.apply(this, arguments);
    };
    var origSend = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.send = function() {
        var xhr = this;
        if (xhr._wdtt_url && xhr._wdtt_url.indexOf('captchaNotRobot.check') !== -1) {
            xhr.addEventListener('load', function() {
                try {
                    var data = JSON.parse(xhr.responseText);
                    if (data.response && data.response.success_token) {
                        onSuccess(data.response.success_token);
                    } else if (data.response && data.response.show_captcha_type === 'slider') {
                        onSlider();
                    }
                } catch(e) {}
            });
        }
        return origSend.apply(this, arguments);
    };
})();
`
	autoClickJS := `
(function() {
    var iv = setInterval(function() {
        var cb = document.querySelector('.vkc__Checkbox-module__Checkbox, label[class*="Checkbox"], [role="checkbox"]');
        if (cb) { cb.click(); clearInterval(iv); }
    }, 200);
    setTimeout(function() { clearInterval(iv); }, 8000);
})();
`

	chrome.MessageCallback = func(message string, sender *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		var payload struct {
			Type  string `json:"type"`
			Token string `json:"token"`
		}
		if err := json.Unmarshal([]byte(message), &payload); err != nil {
			return
		}
		switch payload.Type {
		case "captcha_result":
			if payload.Token != "" {
				resultCh <- captchaResult{token: payload.Token}
			}
		case "captcha_slider":
			if isManual {
				win.ShowWindow(hwnd, win.SW_SHOW)
				win.SetForegroundWindow(hwnd)
			}
		}
	}

	if err := webview.AddScriptToExecuteOnDocumentCreated(interceptorJS, nil); err != nil {
		log.Printf("[CAPTCHA WV] inject interceptor: %v", err)
	}

	log.Printf("[CAPTCHA WV] loading captcha page (mode=%s)", mode)
	if err := webview.Navigate(redirectURI); err != nil {
		return "", fmt.Errorf("Navigate: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Ждём загрузку
	time.Sleep(1500 * time.Millisecond)

	if !isManual {
		webview.ExecuteScript(autoClickJS, nil)
	}

	for {
		select {
		case r := <-resultCh:
			if r.err != nil {
				return "", r.err
			}
			if r.token != "" {
				log.Printf("[CAPTCHA WV] captcha solved (mode=%s)", mode)
				return r.token, nil
			}
		case <-timer.C:
			return "", fmt.Errorf("captcha timed out after %v", timeout)
		default:
		}

		var msg win.MSG
		for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
			if msg.Message == win.WM_QUIT {
				return "", fmt.Errorf("captcha window closed")
			}
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type captchaResult struct {
	token string
	err   error
}

type captchaEnvHandler struct {
	chrome *edge.Chromium
	hwnd   win.HWND
	result chan<- captchaResult
}

func (h *captchaEnvHandler) EnvironmentCompleted(errorCode webviewloader.HRESULT, createdEnvironment *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	if errorCode != 0 || createdEnvironment == nil {
		h.result <- captchaResult{err: fmt.Errorf("env creation failed: hr=%d", errorCode)}
		return webviewloader.HRESULT(windows.S_OK)
	}
	createdEnvironment.AddRef()
	envEdge := (*edge.ICoreWebView2Environment)(unsafe.Pointer(createdEnvironment))
	h.chrome.EnvironmentCompleted(0, envEdge)
	return webviewloader.HRESULT(windows.S_OK)
}

func captchaWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_CLOSE:
		win.DestroyWindow(hwnd)
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}
