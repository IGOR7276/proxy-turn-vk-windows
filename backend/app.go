package backend

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	stdruntime "runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"wg-turn-client/core"
)

// killOldInstances removed to reduce VirusTotal behavior alerts.
// See process_windows.go for history.

const appVersion = "2.1.3"

// App — Wails App, связующее звено между UI и Orchestrator.
type App struct {
	ctx         context.Context
	store       *Store
	orch        *Orchestrator
	trayEnabled atomic.Bool
	trayIcon    []byte
	closeAction atomic.Value // string: "ask" / "hide" / "exit"
	allowExit   atomic.Bool  // одноразовый флаг для runtime.Quit без remember
}

func NewApp(trayIcon []byte) *App {
	a := &App{
		trayIcon: trayIcon,
		store:    NewStore(),
	}
	a.closeAction.Store("ask")
	return a
}

// Startup вызывается Wails при инициализации. Здесь создаём Orchestrator
// и регистрируем трей (если включён).
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.restoreSettings()
	a.orch = NewOrchestrator(ctx, a.onTrayUpdate)
	a.startTrayIfNeeded()
	go a.checkUpdateBackground()
}

// restoreSettings loads persisted settings and applies them to core / UI state.
func (a *App) restoreSettings() {
	st := a.store.GetSettings()
	a.closeAction.Store(st.CloseAction)
	core.SetVkAuthMode(st.VkAuthMode)
	if st.AutoStart {
		_ = SetAutoStart(true)
	}
}

// onTrayUpdate вызывается Orchestrator при обновлении статистики.
// Транслирует в Windows tray (если он активен).
func (a *App) onTrayUpdate(connected bool, rx, tx int64, workers int32) {
	setTrayStatus(connected, rx, tx, workers)
}

// OnBeforeClose обрабатывает клик по X:
//   - allowExit = true → разовый выход (без remember), return false
//   - "ask"  → emit "show_close_dialog" на фронт, return true (отмена закрытия)
//   - "hide" → WindowHide, return true
//   - "exit" → return false (разрешить закрытие)
//
// "ask" — дефолт; фронт показывает диалог с галочкой "Запомнить выбор"
// и вызывает SetCloseAction для смены режима.
func (a *App) OnBeforeClose(ctx context.Context) bool {
	if a.allowExit.Load() {
		a.allowExit.Store(false)
		return false
	}
	act, _ := a.closeAction.Load().(string)
	switch act {
	case "hide":
		runtime.WindowHide(ctx)
		return true
	case "exit":
		// Останавливаем туннель в фоне, не блокируя закрытие окна
		go func() {
			if a.orch != nil {
				a.orch.Stop()
			}
		}()
		return false
	default: // "ask"
		runtime.EventsEmit(a.ctx, "show_close_dialog")
		return true
	}
}

// SetCloseAction — вызывается фронтом из диалога при клике пользователя.
//   action   = "hide" | "exit"  → применить сейчас
//   remember = true             → сохранить в atomic (влияет на будущие OnBeforeClose)
//   remember = false            → только применить, не сохранять
// При action="exit" без remember устанавливается одноразовый флаг allowExit,
// чтобы runtime.Quit не зациклился через повторный OnBeforeClose.
func (a *App) SetCloseAction(action string, remember bool) {
	if remember {
		a.closeAction.Store(action)
		_ = a.store.UpdateSettings(func(st AppSettings) AppSettings {
			st.CloseAction = action
			return st
		})
	}
	switch action {
	case "hide":
		runtime.WindowHide(a.ctx)
	case "exit":
		// Запускаем остановку туннеля в фоне, не блокируя закрытие окна
		if a.orch != nil {
			go a.orch.Stop()
		}
		if !remember {
			a.allowExit.Store(true)
		}
		runtime.Quit(a.ctx)
	}
}

// SetCloseActionPreference — вызывается фронтом при старте, чтобы синхронизировать
// ранее сохранённый выбор (из localStorage) с Go-стороной. Ничего не применяет.
func (a *App) SetCloseActionPreference(action string) {
	if action == "hide" || action == "exit" || action == "ask" {
		a.closeAction.Store(action)
	}
}

// GetSettings returns persisted app settings to the frontend.
func (a *App) GetSettings() AppSettings { return a.store.GetSettings() }

// SaveSettings persists app settings from the frontend.
func (a *App) SaveSettings(st AppSettings) error { return a.store.SaveSettings(st) }

// ListSubscriptions returns all persisted subscriptions.
func (a *App) ListSubscriptions() []Subscription { return a.store.ListSubscriptions() }

// AddSubscription fetches and persists a new subscription.
func (a *App) AddSubscription(url string) (Subscription, error) { return a.store.AddSubscription(url) }

// UpdateSubscription re-fetches a subscription by id.
func (a *App) UpdateSubscription(id string) (Subscription, error) { return a.store.UpdateSubscription(id) }

// DeleteSubscription removes a subscription and its profiles.
func (a *App) DeleteSubscription(id string) error { return a.store.DeleteSubscription(id) }

// GetSubscriptionProfiles returns all profiles from subscription directories.
func (a *App) GetSubscriptionProfiles() map[string]ProfileData { return a.store.GetSubscriptionProfiles() }

// OpenSubscriptionFolder opens the subscription profiles folder in Explorer.
func (a *App) OpenSubscriptionFolder(id string) error {
	dir := filepath.Join(a.store.configDir, "subscriptions", id)
	_ = os.MkdirAll(dir, 0755)
	var cmd string
	var args []string
	switch stdruntime.GOOS {
	case "windows":
		cmd = "explorer"
		args = []string{dir}
	case "darwin":
		cmd = "open"
		args = []string{dir}
	default:
		cmd = "xdg-open"
		args = []string{dir}
	}
	return exec.Command(cmd, args...).Start()
}

// SetAutoStartSetting enables/disables auto-start and persists the choice.
func (a *App) SetAutoStartSetting(enabled bool) error {
	if err := SetAutoStart(enabled); err != nil {
		return err
	}
	return a.store.UpdateSettings(func(st AppSettings) AppSettings {
		st.AutoStart = enabled
		return st
	})
}

// ─── Методы, вызываемые из JS (Wails binding) ───

// Connect — запустить сессию.
func (a *App) Connect(p ConnectParams) error { return a.orch.Start(p) }

// Disconnect — остановить сессию.
func (a *App) Disconnect() { a.orch.Stop() }

// ForceDisconnect — принудительный сброс состояния (если сессия зависла).
func (a *App) ForceDisconnect() {
	a.orch.Stop()
	runtime.EventsEmit(a.ctx, "state_changed", "disconnected")
	runtime.EventsEmit(a.ctx, "log", "WARN", "Принудительный сброс состояния туннеля")
}

// IsRunning — работает ли туннель прямо сейчас.
func (a *App) IsRunning() bool { return a.orch.IsRunning() }

// Pause / Resume — doze-режим воркеров.
func (a *App) Pause()   { a.orch.Pause() }
func (a *App) Resume()  { a.orch.Resume() }
func (a *App) SendCaptchaResult(token string) { a.orch.SendCaptchaResult(token) }

// SendTurnCreds — передаёт TURN-креды от VK-аккаунта в ядро.
func (a *App) SendTurnCreds(payload string) { a.orch.SendTurnCreds(payload) }

// GetVkAuthMode — текущий режим VK-авторизации.
func (a *App) GetVkAuthMode() string { return core.GetVkAuthMode() }

// SetVkAuthMode — установить режим VK-авторизации.
func (a *App) SetVkAuthMode(mode string) string {
	result := core.SetVkAuthMode(mode)
	_ = a.store.UpdateSettings(func(st AppSettings) AppSettings {
		st.VkAuthMode = result
		return st
	})
	return result
}

// GetObfsMode returns the current RTP masking mode (audio/video).
func (a *App) GetObfsMode() string {
	return a.store.GetSettings().ObfsMode
}

// SetObfsMode sets and persists the RTP masking mode.
func (a *App) SetObfsMode(mode string) string {
	mode = core.NormalizeObfsMode(mode)
	_ = a.store.UpdateSettings(func(st AppSettings) AppSettings {
		st.ObfsMode = mode
		return st
	})
	return mode
}

// GetVkAuthStatus — статус авторизации VK (для UI).
func (a *App) GetVkAuthStatus() map[string]interface{} {
	mode := core.GetVkAuthMode()
	result := map[string]interface{}{
		"mode":   mode,
		"active": mode == "account",
	}
	if mode == "account" {
		count := 0
		for range core.CountInjectedCreds() {
			count++
		}
		result["cachedHashes"] = count
	}
	return result
}

// startTrayIfNeeded — инициализация Windows tray (запускается из Startup).
// Безусловно: тред сам спит до первого SetTrayEnabled(true).
// Shutdown вызывается Wails при завершении приложения (OnShutdown).
// Останавливает туннель (WireGuard, DNS-прокси, маршруты), если он был активен.
func (a *App) Shutdown(ctx context.Context) {
	if a.orch != nil {
		a.orch.Stop()
	}
}

func (a *App) startTrayIfNeeded() {
	startTray(a.trayIcon,
		func() { // onShow — открыть/показать окно
			runtime.WindowShow(a.ctx)
		},
		func() { // onToggle — подключиться/отключиться
			if a.orch.IsRunning() {
				a.orch.Stop()
			} else {
				runtime.EventsEmit(a.ctx, "tray_request_connect")
			}
		},
		func() { // onQuit — закрыть приложение полностью (обходит OnBeforeClose)
			if a.orch != nil {
				a.orch.Stop()
			}
			a.allowExit.Store(true)
			runtime.Quit(a.ctx)
		},
	)
}

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

// ─── Проверка обновлений ───

func (a *App) checkUpdateBackground() {
	// Небольшая задержка, чтобы приложение успело запуститься
	time.Sleep(3 * time.Second)
	info := a.CheckUpdate()
	if info.Available {
		runtime.EventsEmit(a.ctx, "update_available", info.Version, info.URL, info.Body, info.AssetURL)
	}
}

// ─── Профили ───

// SaveProfile — сохранить профиль по имени.
func (a *App) SaveProfile(name string, p ProfileData) error {
	return a.store.SaveProfile(name, p)
}

// GetProfile — загрузить профиль.
func (a *App) GetProfile(name string) (*ProfileData, error) {
	return a.store.LoadProfile(name)
}

// DeleteProfile — удалить профиль.
func (a *App) DeleteProfile(name string) error {
	return a.store.DeleteProfile(name)
}

// ListProfiles — список имён сохранённых профилей.
func (a *App) ListProfiles() []string {
	all, err := a.store.ListProfiles()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	return names
}

// ListProfilesMap returns all profiles keyed by name (useful for the UI).
func (a *App) ListProfilesMap() map[string]ProfileData {
	all, _ := a.store.ListProfiles()
	return all
}

