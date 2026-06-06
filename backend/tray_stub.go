//go:build !windows

package backend

func (a *App) startTrayIfNeeded() {}
func (a *App) SetTrayEnabled(v bool) {
	a.trayEnabled.Store(v)
}
func setTrayVisible(v bool) {}
