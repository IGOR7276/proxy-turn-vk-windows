//go:build !windows
// +build !windows

package core

func SetupWindowsWireGuard(rawConf, ifaceName string, customDNS []string) error {
	return nil
}

// TeardownWindowsWireGuard — no-op на не-Windows платформах.
func TeardownWindowsWireGuard() {}

// runRouteAdd / runRouteDelete — заглушки для не-Windows. На других платформах
// exclude-маршруты не используются (WG поднимается в режиме клиента через
// встроенный API ОС), поэтому функции возвращают «не добавлено».
func runRouteAdd(cidr, gateway string) bool {
	return false
}

func runRouteDelete(cidr string) {}
