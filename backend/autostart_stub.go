//go:build !windows

package backend

func (a *App) SetAutoStart(v bool) error { return nil }
func (a *App) GetAutoStart() bool       { return false }

