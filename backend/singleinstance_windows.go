//go:build windows
// +build windows

package backend

import (
	"log"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

// Единственный экземпляр приложения.
//
// Проблема: два запущенных WDTT одновременно пытаются занять один и тот же
// WG-адаптер, порт DNS-прокси (:53) и локальный UDP-порт диспетчера. Второй
// экземпляр либо падает с невнятной ошибкой, либо (хуже) сносит туннель
// первого при teardown. Named mutex в namespace Local\ решает это атомарно
// на уровне ядра Windows и не требует файлов-локов.
const singleInstanceMutexName = `Local\WDTT-SingleInstance-v2`

// singleInstanceHandle держим открытым до выхода из процесса: как только
// handle закрывается (в том числе аварийно), mutex освобождается и следующий
// запуск снова становится возможным.
var singleInstanceHandle windows.Handle

// EnsureSingleInstance возвращает true, если это первый экземпляр.
// Если WDTT уже запущен — пытается вывести его окно на передний план
// и возвращает false (вызывающий должен завершить процесс).
func EnsureSingleInstance() bool {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		// Не смогли даже собрать имя — не блокируем запуск.
		log.Printf("[SINGLE] UTF16PtrFromString: %v", err)
		return true
	}

	h, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		// Mutex уже существует — значит, WDTT запущен. Handle всё равно
		// валиден и его нужно закрыть, иначе он останется висеть.
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		log.Printf("[SINGLE] WDTT уже запущен — активируем существующее окно")
		focusExistingWindow()
		return false
	}
	if err != nil {
		// ERROR_ACCESS_DENIED и прочее: не мешаем пользователю запустить
		// приложение, но пишем в лог для диагностики.
		log.Printf("[SINGLE] CreateMutex: %v", err)
		return true
	}

	singleInstanceHandle = h
	return true
}

// focusExistingWindow ищет окно WDTT по заголовку и разворачивает его.
// Класс окна Wails генерируется динамически, поэтому ищем по title.
func focusExistingWindow() {
	title := win.StringToBSTR("WDTT")
	defer win.SysFreeString(title)

	hwnd := win.FindWindow(nil, title)
	if hwnd == 0 {
		log.Printf("[SINGLE] Окно WDTT не найдено (возможно, свёрнуто в трей)")
		return
	}
	if win.IsIconic(hwnd) {
		win.ShowWindow(hwnd, win.SW_RESTORE)
	}
	win.ShowWindow(hwnd, win.SW_SHOW)
	win.SetForegroundWindow(hwnd)
}
