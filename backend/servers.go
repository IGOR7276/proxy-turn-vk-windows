package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ServerEntry — сервер (профиль подключения) в том виде, в котором его
// редактирует UI. Q1: серверы хранятся в бэкенде (<config>/wdtt/servers.json),
// фронтенд получает их через GetAppState/GetServers и патчит через SaveServer.
type ServerEntry struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Host            string   `json:"host"` // peer addr (ip:port)
	Password        string   `json:"password"`
	DeviceID        string   `json:"deviceId,omitempty"`
	Ping            int      `json:"ping,omitempty"`
	Port            int      `json:"port,omitempty"`
	Hashes          []string `json:"hashes"`
	UseGlobalHashes bool     `json:"useGlobalHashes"`
	Power           int      `json:"power"`
	SubscriptionID  string   `json:"subscriptionId,omitempty"`
}

// normalizeServer приводит запись к каноническому виду: 4 слота хешей,
// дефолтный power, пустые optional-поля без мусора.
func normalizeServer(s ServerEntry) ServerEntry {
	h := make([]string, 4)
	copy(h, s.Hashes)
	s.Hashes = h
	if s.Power <= 0 {
		s.Power = 9
	}
	if s.Power > 100 {
		s.Power = 100
	}
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	return s
}

// serversPath — путь к servers.json.
func (s *Store) serversPath() string {
	return filepath.Join(s.configDir, "servers.json")
}

// loadServersLocked читает servers.json. Битый файл трактуется как пустой
// список, чтобы приложение не блокировалось из-за одной опечатки в JSON.
func (s *Store) loadServersLocked() []ServerEntry {
	data, err := os.ReadFile(s.serversPath())
	if err != nil {
		return nil
	}
	var servers []ServerEntry
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil
	}
	for i := range servers {
		servers[i] = normalizeServer(servers[i])
		servers[i].Password = unprotectSecret(servers[i].Password)
	}
	return servers
}

// saveServersLocked пишет servers.json, шифруя пароли DPAPI.
func (s *Store) saveServersLocked(servers []ServerEntry) error {
	out := make([]ServerEntry, len(servers))
	for i, srv := range servers {
		srv = normalizeServer(srv)
		srv.Password = protectSecret(srv.Password)
		out[i] = srv
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.serversPath(), data)
}

// ListServers возвращает все серверы (пароли расшифрованы для UI).
func (s *Store) ListServers() []ServerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadServersLocked()
}

// SaveServer добавляет или обновляет сервер по id. Пустой id генерируется
// на стороне бэкенда, чтобы фронтенду не приходилось придумывать ключи.
func (s *Store) SaveServer(srv ServerEntry) (ServerEntry, error) {
	srv = normalizeServer(srv)
	if srv.Host == "" {
		return ServerEntry{}, fmt.Errorf("адрес сервера не задан")
	}
	if srv.ID == "" {
		srv.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	servers := s.loadServersLocked()
	found := false
	for i := range servers {
		if servers[i].ID == srv.ID {
			servers[i] = srv
			found = true
			break
		}
	}
	if !found {
		servers = append(servers, srv)
	}
	if err := s.saveServersLocked(servers); err != nil {
		return ServerEntry{}, err
	}
	return srv, nil
}

// DeleteServer удаляет сервер и сбрасывает выбор, если он указывал на него.
func (s *Store) DeleteServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	servers := s.loadServersLocked()
	remaining := make([]ServerEntry, 0, len(servers))
	for _, srv := range servers {
		if srv.ID != id {
			remaining = append(remaining, srv)
		}
	}
	if len(remaining) == len(servers) {
		return fmt.Errorf("сервер не найден")
	}
	if err := s.saveServersLocked(remaining); err != nil {
		return err
	}
	if s.settings.SelectedServer == id {
		next := ""
		if len(remaining) > 0 {
			next = remaining[0].ID
		}
		s.settings.SelectedServer = next
		return s.saveSettingsLocked(s.settings)
	}
	return nil
}

// ReplaceServers перезаписывает весь список серверов (используется, когда
// фронтенд удаляет подписку и убирает связанные профили пачкой).
func (s *Store) ReplaceServers(servers []ServerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveServersLocked(servers)
}

// SelectServer запоминает выбранный сервер в настройках (Q1: выбор тоже
// живёт в бэкенде, а не в localStorage).
func (s *Store) SelectServer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.SelectedServer = id
	return s.saveSettingsLocked(s.settings)
}
