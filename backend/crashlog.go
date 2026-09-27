package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// InstallCrashLog дублирует трейс фатальной ошибки Go (panic, fatal error)
// в <config>/wdtt/logs/crash.log. У GUI-сборки нет консоли, а Windows Error
// Reporting выход рантайма Go не ловит, поэтому без этого падение не
// оставляет никаких следов.
func InstallCrashLog() {
	dir := filepath.Join(configDir(), "logs")
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "\n=== запуск %s, pid %d ===\n", time.Now().Format("2006-01-02 15:04:05"), os.Getpid())
	_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	f.Close() // SetCrashOutput держит собственный дубликат дескриптора
}
