package backend

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) OpenVkLoginWindow() {
	runtime.EventsEmit(a.ctx, "vk_open_login")
}

func (a *App) CloseVkLoginWindow() {
	runtime.EventsEmit(a.ctx, "vk_close_login")
}
