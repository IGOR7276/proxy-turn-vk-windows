//go:build !windows

package backend

func SetAutoStart(v bool) error { return nil }
func GetAutoStart() bool       { return false }
func (a *App) SetAutoStart(v bool) error { return SetAutoStart(v) }
func (a *App) GetAutoStart() bool       { return GetAutoStart() }

