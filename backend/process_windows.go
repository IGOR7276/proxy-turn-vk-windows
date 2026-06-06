//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// killOldInstances находит уже работающие wdtt.exe, кроме текущего процесса,
// и завершает их. Это нужно, чтобы избежать ситуации когда два экземпляра
// держат один порт (9000) и второй уходит в fallback на dynamic.
//
// Безопасность: трогаем только процессы, чей ExecutablePath заканчивается на
// "wdtt.exe" — это исключает случайное убийство чего-то неожиданного.
// Процессы с тем же путём что и наш (например, дочерний watcher) пропускаются.
func killOldInstances() int {
	currentPID := os.Getpid()
	currentExe, _ := os.Executable()
	currentExe = strings.ToLower(filepath.Clean(currentExe))

	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	var pe32 windows.ProcessEntry32
	pe32.Size = uint32(unsafe.Sizeof(pe32))
	if err := windows.Process32First(snap, &pe32); err != nil {
		return 0
	}

	killed := 0
	for {
		name := strings.ToLower(windows.UTF16ToString(pe32.ExeFile[:]))
		if name == "wdtt.exe" {
			pid := int(pe32.ProcessID)
			if pid == currentPID {
				goto next
			}
			// Открываем процесс и читаем его полный путь
			h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
			if err == nil {
				var buf [260]uint16
				n := uint32(len(buf))
				err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n)
				if err == nil {
					otherPath := strings.ToLower(filepath.Clean(windows.UTF16ToString(buf[:n])))
					if otherPath == currentExe {
						windows.CloseHandle(h)
						goto next
					}
				}
				windows.CloseHandle(h)
			}
			// Чужой экземпляр — убиваем
			h, err = windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
			if err == nil {
				windows.TerminateProcess(h, 0)
				windows.CloseHandle(h)
				killed++
			}
		}
	next:
		if err := windows.Process32Next(snap, &pe32); err != nil {
			break
		}
	}
	return killed
}
