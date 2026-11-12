//go:build !windows
// +build !windows

package backend

// На не-Windows DPAPI недоступен: возвращаем строку как есть. Приложение
// собирается под Windows, stub нужен только для тестов/кросс-сборки.

func dpapiProtectString(plain string) (string, error) { return plain, nil }

func dpapiUnprotectString(stored string) (string, error) { return stored, nil }
