//go:build windows

package backend

import (
	"context"
	"sync"

	"github.com/lxn/win"
)

// VkAuthManager управляет VK OAuth токеном.
type VkAuthManager struct {
	ctx      context.Context
	mu       sync.Mutex
	token    string
	joinHash string
	windowHWND win.HWND // HWND WebView2 окна (для CancelVkOAuth)
}

var vkAuth = &VkAuthManager{}

// setWindowHWND сохраняет HWND окна для CancelVkOAuth.
func (a *App) setOAuthWindowHWND(hwnd win.HWND) {
	vkAuth.mu.Lock()
	defer vkAuth.mu.Unlock()
	vkAuth.windowHWND = hwnd
}

func (a *App) GetVkToken() string {
	vkAuth.mu.Lock()
	defer vkAuth.mu.Unlock()
	return vkAuth.token
}

// CancelVkOAuth закрывает окно WebView2 если оно открыто.
func (a *App) CancelVkOAuth() {
	vkAuth.mu.Lock()
	hwnd := vkAuth.windowHWND
	vkAuth.windowHWND = 0
	vkAuth.mu.Unlock()

	if hwnd != 0 {
		win.SendMessage(hwnd, win.WM_CLOSE, 0, 0)
	}
	vkAuth.mu.Lock()
	vkAuth.token = ""
	vkAuth.mu.Unlock()
	vkLogf("OAuth отменён")
}
