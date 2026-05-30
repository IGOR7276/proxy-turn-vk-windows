//go:build !windows
// +build !windows

package main

func SetupWindowsWireGuard(rawConf, ifaceName string) error {
	return nil
}
