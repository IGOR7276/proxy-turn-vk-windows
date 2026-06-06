//go:build windows

package backend

// Заглушка для трея. Полная реализация win_tray.go появится позже
// (см. PWDTT-main/backend/win_tray.go — 5KB, использует lxn/win).

func (a *App) startTrayIfNeeded() {
	// no-op пока
}

func (a *App) SetTrayEnabled(v bool) {
	a.trayEnabled.Store(v)
}

func setTrayVisible(v bool) {
	// no-op
}
