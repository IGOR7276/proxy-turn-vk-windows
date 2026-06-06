package backend

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App — Wails App, связующее звено между UI и Orchestrator.
type App struct {
	ctx         context.Context
	orch        *Orchestrator
	trayEnabled atomic.Bool
	trayIcon    []byte
}

func NewApp(trayIcon []byte) *App { return &App{trayIcon: trayIcon} }

// Startup вызывается Wails при инициализации. Здесь создаём Orchestrator
// и регистрируем трей (если включён).
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	// Убиваем старые wdtt.exe (кроме текущего) чтобы избежать конфликта за порт 9000.
	if n := killOldInstances(); n > 0 {
		log.Printf("[WDTT] Завершено %d предыдущих экземпляров", n)
		// Даём ОС закрыть UDP-сокеты
		time.Sleep(500 * time.Millisecond)
	}
	a.orch = NewOrchestrator(ctx)
	a.startTrayIfNeeded()
}

// OnBeforeClose скрывает окно в трей, если трей включён, иначе — закрывает.
func (a *App) OnBeforeClose(ctx context.Context) bool {
	if a.trayEnabled.Load() {
		runtime.WindowHide(ctx)
		return true
	}
	return false
}

// ─── Методы, вызываемые из JS (Wails binding) ───

// Connect — запустить сессию.
func (a *App) Connect(p ConnectParams) error { return a.orch.Start(p) }

// Disconnect — остановить сессию.
func (a *App) Disconnect() { a.orch.Stop() }

// IsRunning — работает ли туннель прямо сейчас.
func (a *App) IsRunning() bool { return a.orch.IsRunning() }

// Pause / Resume — doze-режим воркеров.
func (a *App) Pause()   { a.orch.Pause() }
func (a *App) Resume()  { a.orch.Resume() }
func (a *App) SendCaptchaResult(token string) { a.orch.SendCaptchaResult(token) }

// CheckVPN — список активных VPN-интерфейсов (исключая наш wg-turn).
func (a *App) CheckVPN() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var found []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		n := strings.ToLower(iface.Name)
		if n == wgIface {
			continue
		}
		if strings.HasPrefix(n, "tun") ||
			strings.HasPrefix(n, "tap") ||
			strings.HasPrefix(n, "wg") ||
			strings.HasPrefix(n, "ppp") ||
			strings.HasPrefix(n, "nordlynx") ||
			strings.HasPrefix(n, "proton") ||
			strings.HasPrefix(n, "utun") ||
			strings.HasPrefix(n, "ipsec") {
			found = append(found, iface.Name)
		}
	}
	return found
}

// ─── Профили ───

// SaveProfile — сохранить профиль по имени.
func (a *App) SaveProfile(name string, p ProfileData) error {
	dir := filepath.Join(configDir(), "profiles")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(profilePath(name), data, 0600)
}

// GetProfile — загрузить профиль.
func (a *App) GetProfile(name string) (*ProfileData, error) {
	return loadProfile(name)
}

// DeleteProfile — удалить профиль.
func (a *App) DeleteProfile(name string) error {
	return os.Remove(profilePath(name))
}

// ListProfiles — список имён сохранённых профилей.
func (a *App) ListProfiles() []string {
	dir := filepath.Join(configDir(), "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	return names
}
