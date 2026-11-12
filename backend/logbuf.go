package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// LogRecord — одна строка лога. Формат общий для UI-события logs_batch и
// экспорта в файл.
type LogRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Time  string `json:"time"`
}

// logRingMax — сколько последних строк хранит бэкенд. Этого достаточно для
// экспорта и для переподключения UI после перезагрузки WebView, при этом
// память ограничена (5000 × ~200 байт ≈ 1 МБ).
const logRingMax = 5000

var logRing = struct {
	mu      sync.Mutex
	entries []LogRecord
}{entries: make([]LogRecord, 0, logRingMax)}

// appendLogRing добавляет запись в кольцевой буфер (Q1: логи живут в бэкенде,
// фронт получает их батчами и не является источником правды).
func appendLogRing(rec LogRecord) {
	logRing.mu.Lock()
	defer logRing.mu.Unlock()
	if len(logRing.entries) >= logRingMax {
		copy(logRing.entries, logRing.entries[1:])
		logRing.entries = logRing.entries[:len(logRing.entries)-1]
	}
	logRing.entries = append(logRing.entries, rec)
}

// snapshotLogs возвращает копию кольцевого буфера.
func snapshotLogs() []LogRecord {
	logRing.mu.Lock()
	defer logRing.mu.Unlock()
	out := make([]LogRecord, len(logRing.entries))
	copy(out, logRing.entries)
	return out
}

// ExportLogs открывает системный диалог сохранения и пишет туда весь
// кольцевой буфер. Возвращает путь файла ("" — пользователь отменил).
func (a *App) ExportLogs() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Экспорт логов WDTT",
		DefaultFilename: "wdtt-logs-" + time.Now().Format("2006-01-02_15-04-05") + ".txt",
		Filters: []runtime.FileFilter{
			{DisplayName: "Текстовые файлы (*.txt)", Pattern: "*.txt"},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	entries := snapshotLogs()
	var b strings.Builder
	b.Grow(len(entries) * 96)
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] [%s] %s\n", e.Time, e.Level, e.Msg)
	}
	if err := writeFileAtomic(path, []byte(b.String())); err != nil {
		return "", err
	}
	return path, nil
}

// emitLogEvent — единая точка отправки одиночной лог-строки. Дублирует её в
// кольцевой буфер (для экспорта) и шлёт батчем из одной записи, чтобы UI не
// терял сообщения оркестратора, которые не идут через log.Printf.
func emitLogEvent(ctx context.Context, level, msg string) {
	rec := LogRecord{Level: level, Msg: msg, Time: time.Now().Format("15:04:05")}
	appendLogRing(rec)
	runtime.EventsEmit(ctx, "logs_batch", []LogRecord{rec})
}

// GetLogs возвращает последние строки лога (для восстановления UI после
// перезагрузки WebView и для внешних интеграций).
func (a *App) GetLogs() []LogRecord {
	return snapshotLogs()
}

// writeFileAtomic пишет файл через временный + rename, чтобы экспорт не
// оставлял полупустой файл при ошибке/закрытии приложения.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".wdtt-export-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
