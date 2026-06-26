//go:build windows

package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

func vkLogf(format string, args ...interface{}) {
	msg := fmt.Sprintf("[VK Auth] "+format, args...)
	log.Printf(msg)
	if vkAuth != nil && vkAuth.ctx != nil {
		wailsRuntime.EventsEmit(vkAuth.ctx, "log", "INFO", msg)
	}
}

func (a *App) VkLogin() error {
	vkAuth.mu.Lock()
	vkAuth.ctx = a.ctx
	vkAuth.token = ""
	vkAuth.joinHash = ""
	vkAuth.mu.Unlock()

	vkLogf("VkLogin called")

	go func() {
		defer func() {
			if r := recover(); r != nil {
				vkLogf("PANIC: %v", r)
			}
		}()

		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
		defer windows.CoUninitialize()

		a.runVkLoginWindow()
	}()

	return nil
}

func (a *App) VkCallJoin(hash string) error {
	vkLogf("VkCallJoin called, hash=%s...", shortHash(hash))
	oauthMu.Lock()
	ch := callJoinReqCh
	oauthMu.Unlock()
	if ch != nil {
		vkLogf("Sending hash to login window...")
		ch <- hash
	} else {
		vkLogf("callJoinReqCh is nil, login window not running")
	}
	return nil
}

var classCounter atomic.Int32

func nextClassName() *uint16 {
	n := classCounter.Add(1)
	s, _ := syscall.UTF16PtrFromString(fmt.Sprintf("VkOAuthWin_%d", n))
	return s
}

const (
	msgInitWebView2 = win.WM_APP + 2
)

var (
	oauthMu       sync.Mutex
	oauthDone     chan struct{}
	navHeld       *navStartingHandler
	oauthTokenCh  chan<- tokenDetail
	turnCredsCh   chan<- string
	loginDoneCh   chan<- struct{} // signals login completed
	callJoinReqCh chan string     // receives call/join hash from VkCallJoin
	oauthHwnd     win.HWND
)

const interceptorScript = `
(function() {
	if (window.__wdtt_turn_interceptor) return;
	window.__wdtt_turn_interceptor = true;

	// ── Turn server interceptor ──
	function sendPayload(payload) {
		try {
			window.chrome.webview.postMessage(JSON.stringify({turndata: payload}));
		} catch(e) {
			window.__wdtt_turndata = payload;
		}
	}
	function tryEmitTurnServer(data) {
		if (!data) return;
		var ts = data.turn_server;
		if (!ts && data.response && data.response.turn_server) {
			ts = data.response.turn_server;
		}
		if (ts && ts.username && ts.credential && ts.urls && ts.urls.length > 0) {
			var payload = btoa(unescape(encodeURIComponent(JSON.stringify({u:ts.username, p:ts.credential, urls:ts.urls}))));
			sendPayload(payload);
		}
	}
	var origFetch = window.fetch;
	window.fetch = async function() {
		var response = await origFetch.apply(this, arguments);
		try {
			var clone = response.clone();
			var text = await clone.text();
			if (text && text.indexOf('turn_server') !== -1) {
				tryEmitTurnServer(JSON.parse(text));
			}
		} catch(e) {}
		return response;
	};
	var origXHROpen = XMLHttpRequest.prototype.open;
	var origXHRSend = XMLHttpRequest.prototype.send;
	XMLHttpRequest.prototype.open = function(method, url) {
		this._wdtt_url = url;
		return origXHROpen.apply(this, arguments);
	};
	XMLHttpRequest.prototype.send = function() {
		var xhr = this;
		xhr.addEventListener('load', function() {
			try {
				if (!xhr.responseText || xhr.responseText.indexOf('turn_server') === -1) return;
				tryEmitTurnServer(JSON.parse(xhr.responseText));
			} catch(e) {}
		});
		return origXHRSend.apply(this, arguments);
	};

	// ── WebSocket interceptor ──
	var origWS = window.WebSocket;
	window.WebSocket = function(url, protocols) {
		var ws = protocols ? new origWS(url, protocols) : new origWS(url);
		ws.addEventListener('message', function(event) {
			try {
				var data = JSON.parse(event.data);
				tryEmitTurnServer(data);
			} catch(e) {}
		});
		return ws;
	};
})();
`

func (a *App) runVkLoginWindow() {
	vkLogf("RegisterClassEx...")
	hInst := win.GetModuleHandle(nil)
	classNamePtr := nextClassName()
	wc := win.WNDCLASSEX{
		CbSize:        uint32(unsafe.Sizeof(win.WNDCLASSEX{})),
		Style:         win.CS_HREDRAW | win.CS_VREDRAW,
		LpfnWndProc:   windows.NewCallback(oauthWndProc),
		HInstance:     hInst,
		HCursor:       win.LoadCursor(0, win.MAKEINTRESOURCE(32512)),
		HbrBackground: win.HBRUSH(win.COLOR_WINDOW + 1),
		LpszClassName: classNamePtr,
	}
	if atom := win.RegisterClassEx(&wc); atom == 0 {
		vkLogf("RegisterClassEx failed")
		return
	}
	defer win.UnregisterClass(classNamePtr)

	vkLogf("CreateWindowEx...")
	hwnd := win.CreateWindowEx(
		win.WS_EX_DLGMODALFRAME, classNamePtr,
		syscall.StringToUTF16Ptr("VK Авторизация"),
		win.WS_OVERLAPPED|win.WS_CAPTION|win.WS_SYSMENU|win.WS_MINIMIZEBOX,
		win.CW_USEDEFAULT, win.CW_USEDEFAULT, 800, 700,
		0, 0, hInst, nil,
	)
	if hwnd == 0 {
		vkLogf("CreateWindowEx failed")
		return
	}
	defer a.setOAuthWindowHWND(0)
	defer win.DestroyWindow(hwnd)

	win.ShowWindow(hwnd, win.SW_SHOW)
	centerWindow(hwnd)
	a.setOAuthWindowHWND(hwnd)

	tokenCh := make(chan tokenDetail, 1)
	loginCh := make(chan struct{}, 1)
	done := make(chan struct{})

	oauthMu.Lock()
	oauthDone = done
	oauthHwnd = hwnd
	oauthTokenCh = tokenCh
	loginDoneCh = loginCh
	oauthMu.Unlock()
	defer func() {
		oauthMu.Lock()
		oauthDone = nil
		oauthHwnd = 0
		oauthTokenCh = nil
		loginDoneCh = nil
		turnCredsCh = nil
		callJoinReqCh = nil
		oauthMu.Unlock()
	}()

	vkLogf("Creating edge.Chromium...")
	chrome := edge.NewChromium()
	chrome.SetErrorCallback(func(err error) {
		vkLogf("Chromium error: %v", err)
	})
	*(*uintptr)(unsafe.Pointer(chrome)) = uintptr(hwnd)

	webviewloader.CreateCoreWebView2EnvironmentWithOptions(&envHandler{
		chrome: chrome,
		hwnd:   hwnd,
		token:  tokenCh,
	}, webviewloader.WithUserDataFolder(filepath.Join(configDir(), "webview2")))

	vkLogf("Starting msg pump (waiting for controller)...")
	for {
		var msg win.MSG
		for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
			if msg.Message == win.WM_QUIT {
				vkLogf("WM_QUIT received")
				return
			}
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}
		select {
		case <-done:
			vkLogf("Окно закрыто, вход отменён")
			wailsRuntime.EventsEmit(a.ctx, "vk_login_done", "")
			return
		default:
		}
		if chrome.GetController() != nil {
			break
		}
		select {
		case td := <-tokenCh:
			if td.token == "" {
				vkLogf("Environment creation failed")
				wailsRuntime.EventsEmit(a.ctx, "vk_login_done", "")
			}
			return
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}

	vkLogf("Controller ready, initializing webview...")
	ctrl := chrome.GetController()
	webview, err := ctrl.GetCoreWebView2()
	if err != nil {
		vkLogf("GetCoreWebView2 failed: %v", err)
		return
	}
	vkLogf("Got webview")

	addNavStarting(webview)

	chrome.MessageCallback = func(message string, sender *edge.ICoreWebView2, args *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		go func() {
			var pd struct {
				Turndata string `json:"turndata"`
			}
			if err := json.Unmarshal([]byte(message), &pd); err != nil || pd.Turndata == "" {
				return
			}
			raw, err := base64.StdEncoding.DecodeString(pd.Turndata)
			if err != nil {
				vkLogf("turndata base64 decode error: %v", err)
				return
			}
			var tcp turnCredsPayload
			if err := json.Unmarshal(raw, &tcp); err != nil {
				vkLogf("turndata json decode error: %v", err)
				return
			}
			vkLogf("TURN creds received via postMessage")
			tcpRaw, _ := json.Marshal(tcp)
			finalB64 := base64.StdEncoding.EncodeToString(tcpRaw)
			hash := ""
			vkAuth.mu.Lock()
			hash = vkAuth.joinHash
			vkAuth.mu.Unlock()
			out := fmt.Sprintf("%s|%s", hash, finalB64)
			if ch := turnCredsCh; ch != nil {
				ch <- out
			}
		}()
	}

	settings, err := webview.GetSettings()
	if err == nil {
		settings.PutAreDevToolsEnabled(false)
		settings.PutAreDefaultContextMenusEnabled(false)
		vkLogf("Settings updated")
	}
	ctrl.PutIsVisible(true)
	bounds := edge.Rect{Left: 0, Top: 0, Right: 800, Bottom: 700}
	ctrl.PutBounds(bounds)

	if err := webview.AddScriptToExecuteOnDocumentCreated(interceptorScript, nil); err != nil {
		vkLogf("AddScriptToExecuteOnDocumentCreated failed: %v", err)
	} else {
		vkLogf("AddScriptToExecuteOnDocumentCreated done")
	}

	callJoinCh := make(chan string, 1)
	turnCh := make(chan string, 1)

	oauthMu.Lock()
	callJoinReqCh = callJoinCh
	turnCredsCh = turnCh
	oauthMu.Unlock()
	vkLogf("Channels set, entering message pump...")

	vkLogf("Navigating to m.vk.com for login...")
	if err := webview.Navigate("https://m.vk.com/"); err != nil {
		vkLogf("Navigate failed: %v", err)
		return
	}

	loggedIn := false
	turnDone := false

	for {
		var msg win.MSG
		for win.PeekMessage(&msg, 0, 0, 0, win.PM_REMOVE) {
			if msg.Message == win.WM_QUIT {
				return
			}
			win.TranslateMessage(&msg)
			win.DispatchMessage(&msg)
		}

		select {
		case <-done:
			vkLogf("Окно закрыто")
			if !loggedIn {
				wailsRuntime.EventsEmit(a.ctx, "vk_login_done", "")
			}
			return
		default:
		}

		if !loggedIn {
			select {
			case <-loginCh:
				vkLogf("Login detected, hiding window")
				loggedIn = true
				win.ShowWindow(hwnd, win.SW_HIDE)
				wailsRuntime.EventsEmit(a.ctx, "vk_login_done", "ok")
			default:
			}
		} else {
			select {
			case hash := <-callJoinCh:
				vkAuth.mu.Lock()
				vkAuth.joinHash = hash
				vkAuth.mu.Unlock()
				callURL := fmt.Sprintf("https://m.vk.com/call/join/%s", hash)
				vkLogf("Navigating to call/join, showing window...")
				win.ShowWindow(hwnd, win.SW_SHOW)
				win.SetForegroundWindow(hwnd)
				if err := webview.Navigate(callURL); err != nil {
					vkLogf("Navigate failed: %v", err)
				}
			case payload := <-turnCh:
				if turnDone {
					continue
				}
				turnDone = true
				vkLogf("TURN creds received, emitting...")
				wailsRuntime.EventsEmit(a.ctx, "vk_turn_creds", payload)
				if a.orch != nil {
					a.orch.SendTurnCreds(payload)
				}
			default:
			}
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func centerWindow(hwnd win.HWND) {
	var r win.RECT
	win.GetWindowRect(hwnd, &r)
	w := r.Right - r.Left
	h := r.Bottom - r.Top
	sw := win.GetSystemMetrics(win.SM_CXSCREEN)
	sh := win.GetSystemMetrics(win.SM_CYSCREEN)
	win.MoveWindow(hwnd, (sw-w)/2, (sh-h)/2, w, h, true)
}

func oauthWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case msgInitWebView2:
		return 0
	case win.WM_CLOSE:
		oauthMu.Lock()
		if oauthDone != nil {
			close(oauthDone)
		}
		oauthMu.Unlock()
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

// ─── Env handler: forwards created environment to edge Chromium ───

type envHandler struct {
	chrome *edge.Chromium
	hwnd   win.HWND
	token  chan<- tokenDetail
}

func (h *envHandler) EnvironmentCompleted(errorCode webviewloader.HRESULT, createdEnvironment *webviewloader.ICoreWebView2Environment) webviewloader.HRESULT {
	if errorCode != 0 || createdEnvironment == nil {
		vkLogf("Environment error: hr=%d", errorCode)
		h.token <- tokenDetail{}
		return webviewloader.HRESULT(windows.S_OK)
	}

	vkLogf("Environment created, forwarding to edge Chromium...")

	// AddRef to keep environment alive
	createdEnvironment.AddRef()

	// Cast to edge's ICoreWebView2Environment (same memory layout)
	envEdge := (*edge.ICoreWebView2Environment)(unsafe.Pointer(createdEnvironment))

	// Call Chromium's EnvironmentCompleted — this will CreateCoreWebView2Controller
	h.chrome.EnvironmentCompleted(0, envEdge)

	vkLogf("EnvironmentCompleted forwarded, controller creation in progress...")

	return webviewloader.HRESULT(windows.S_OK)
}

// ─── AddNavigationStarting via raw vtbl ───

type rawWv2Vtbl struct {
	_              [7]uintptr
	AddNavStarting uintptr
}

type rawWebView struct {
	vtbl *rawWv2Vtbl
}

type rawNavArgsVtbl struct {
	_ [3]uintptr
	GetUri_    uintptr
	_          uintptr
	_          uintptr
	_          uintptr
	_          uintptr
	PutCancel_ uintptr
}

type rawNavArgs struct {
	vtbl *rawNavArgsVtbl
}

type eventToken struct{ value int64 }

func addNavStarting(wv *edge.ICoreWebView2) {
	ov := (*rawWebView)(unsafe.Pointer(wv))
	navH := &navStartingHandler{vtbl: &navStartingHandlerFn}
	navHeld = navH
	var token eventToken
	hr, _, _ := syscall.SyscallN(
		ov.vtbl.AddNavStarting,
		uintptr(unsafe.Pointer(wv)),
		uintptr(unsafe.Pointer(navH)),
		uintptr(unsafe.Pointer(&token)),
	)
	if int32(hr) < 0 {
		vkLogf("AddNavigationStarting failed hr=%d", int32(hr))
	} else {
		vkLogf("AddNavigationStarting done")
	}
}

type wv2IUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type navStartingHandlerVtbl struct {
	wv2IUnknownVtbl
	Invoke uintptr
}

type navStartingHandler struct {
	vtbl *navStartingHandlerVtbl
}

var navStartingHandlerFn = navStartingHandlerVtbl{
	wv2IUnknownVtbl{
		windows.NewCallback(navStartingHandlerQI),
		windows.NewCallback(navStartingHandlerAddRef),
		windows.NewCallback(navStartingHandlerRelease),
	},
	windows.NewCallback(navStartingHandlerInvoke),
}

func navStartingHandlerQI(this, refiid, object uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(object)) = this
	return 0
}
func navStartingHandlerAddRef(this uintptr) uintptr  { return 1 }
func navStartingHandlerRelease(this uintptr) uintptr { return 1 }

type tokenDetail struct {
	token string
	uuid  string
}

func isVkLoggedIn(uri string) bool {
	// Only m.vk.com/feed (and variations with query/hash) means fully logged in.
	// Intermediary pages (SMS confirmation, etc.) are on m.vk.com/* paths too,
	// so we must be very specific — only /feed is the canonical post-login page.
	if !strings.Contains(uri, "m.vk.com/feed") {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	return strings.TrimPrefix(u.Path, "/") == "feed"
}

func navStartingHandlerInvoke(this, sender, argsPtr uintptr) uintptr {
	args := (*rawNavArgs)(unsafe.Pointer(argsPtr))

	var uriP *uint16
	syscall.SyscallN(args.vtbl.GetUri_, uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&uriP)))
	if uriP == nil {
		return 0
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(uriP))
	uri := windows.UTF16PtrToString(uriP)
	vkLogf("NavigationStarting: %s", uri)

	if isVkLoggedIn(uri) {
		vkLogf("Login detected on m.vk.com")
		select {
		case loginDoneCh <- struct{}{}:
		default:
		}
	}

	return 0
}
