//go:build !windows
// +build !windows

package backend

// EnsureSingleInstance — на не-Windows платформах проверка не нужна
// (приложение собирается только под Windows, но stub нужен для сборки
// и тестов на других ОС).
func EnsureSingleInstance() bool {
	return true
}
