package backend

import (
	"os"
	"time"
)

// AppState — полный снимок состояния приложения для первичной гидратации
// фронтенда (Q1). Один вызов вместо десятка разрозненных геттеров: настройки,
// серверы, выбранный сервер, конфиг деплоя и текущий дашборд.
type AppState struct {
	Settings               AppSettings  `json:"settings"`
	Servers                []ServerEntry `json:"servers"`
	SelectedServer         string        `json:"selectedServer"`
	Deploy                 DeployConfig  `json:"deploy"`
	Dashboard              Dashboard     `json:"dashboard"`
	Version                string        `json:"version"`
	LegacyMigrationPending bool          `json:"legacyMigrationPending"`
}

// GetAppState возвращает полный снимок состояния.
func (a *App) GetAppState() AppState {
	st := a.store.GetSettings()
	return AppState{
		Settings:               st,
		Servers:                a.store.ListServers(),
		SelectedServer:         st.SelectedServer,
		Deploy:                 a.store.GetDeployConfig(),
		Dashboard:              a.GetDashboard(),
		Version:                appVersion,
		LegacyMigrationPending: a.store.LegacyMigrationPending(),
	}
}

// GetDashboard — снимок рантайма для дашборда (U1). До старта оркестратора
// возвращает пустое состояние, чтобы UI не падал.
func (a *App) GetDashboard() Dashboard {
	if a.orch == nil {
		return Dashboard{State: "idle"}
	}
	return a.orch.Dashboard()
}

// CheckConnection — активная проверка соединения (кнопка в дашборде).
func (a *App) CheckConnection() ConnectionCheck {
	if a.orch == nil {
		return ConnectionCheck{Error: "туннель не запущен", CheckedAt: time.Now().Unix()}
	}
	return a.orch.CheckConnection()
}

// ─── Серверы ───

// GetServers возвращает все сохранённые серверы.
func (a *App) GetServers() []ServerEntry { return a.store.ListServers() }

// SaveServer добавляет или обновляет сервер. Пустой id генерируется в бэкенде.
func (a *App) SaveServer(srv ServerEntry) (ServerEntry, error) { return a.store.SaveServer(srv) }

// SaveServers перезаписывает список серверов целиком.
func (a *App) SaveServers(servers []ServerEntry) error { return a.store.ReplaceServers(servers) }

// DeleteServer удаляет сервер по id.
func (a *App) DeleteServer(id string) error { return a.store.DeleteServer(id) }

// SelectServer запоминает выбранный сервер.
func (a *App) SelectServer(id string) error { return a.store.SelectServer(id) }

// ─── Деплой ───

// GetDeployConfig возвращает параметры деплоя (секреты расшифрованы).
func (a *App) GetDeployConfig() DeployConfig { return a.store.GetDeployConfig() }

// SaveDeployConfig сохраняет параметры деплоя (секреты шифруются DPAPI).
func (a *App) SaveDeployConfig(cfg DeployConfig) error { return a.store.SaveDeployConfig(cfg) }

// ─── Миграция localStorage → бэкенд ───

// ImportLegacyState переносит данные, которые старые версии хранили только в
// localStorage. Вызывается фронтендом один раз при старте; повторный вызов
// ничего не ломает — серверы/деплой импортируются только при отсутствии
// соответствующих файлов, настройки — только для legacy-конфига.
func (a *App) ImportLegacyState(servers []ServerEntry, legacy AppSettings, deploy DeployConfig, selected string) error {
	if !fileExists(a.store.serversPath()) && len(servers) > 0 {
		for _, srv := range servers {
			if _, err := a.store.SaveServer(srv); err != nil {
				return err
			}
		}
		if selected != "" {
			_ = a.store.SelectServer(selected)
		}
	}

	if a.store.LegacyMigrationPending() {
		_ = a.store.UpdateSettings(func(st AppSettings) AppSettings {
			st.Power = legacy.Power
			st.MTU = legacy.MTU
			st.Tray = legacy.Tray
			st.Hashes = legacy.Hashes
			st.LinkMode = legacy.LinkMode
			st.Fingerprint = legacy.Fingerprint
			st.DNSProxyEnabled = legacy.DNSProxyEnabled
			st.DNSProvider = legacy.DNSProvider
			st.DNSCustom = legacy.DNSCustom
			st.AutoWG = legacy.AutoWG
			st.WGInterface = legacy.WGInterface
			st.AutoReconnect = legacy.AutoReconnect
			if legacy.ObfsMode != "" {
				st.ObfsMode = legacy.ObfsMode
			}
			if legacy.CloseAction != "" {
				st.CloseAction = legacy.CloseAction
			}
			if legacy.AutoStart {
				st.AutoStart = true
			}
			return st
		})
		a.applySettings(a.store.GetSettings())
		a.store.MarkLegacyMigrated()
	}

	if !fileExists(a.store.deployPath()) {
		return a.store.SaveDeployConfig(deploy)
	}
	return nil
}

// fileExists — небольшой помощник для проверок «файл уже есть».
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
