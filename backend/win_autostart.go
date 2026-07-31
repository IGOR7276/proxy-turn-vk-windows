//go:build windows

package backend

import (
	"os"

	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func SetAutoStart(v bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !v {
		return k.DeleteValue("WDTT")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("WDTT", `"`+exe+`"`)
}

func GetAutoStart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("WDTT")
	return err == nil
}

func (a *App) SetAutoStart(v bool) error {
	return SetAutoStart(v)
}

func (a *App) GetAutoStart() bool {
	return GetAutoStart()
}

