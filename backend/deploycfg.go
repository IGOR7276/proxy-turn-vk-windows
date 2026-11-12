package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DeployConfig — параметры деплоя сервера. Q1: живёт в бэкенде
// (<config>/wdtt/deploy.json), а не в localStorage.
//
// Q10: пароль SSH, пароль туннеля и токен бота шифруются DPAPI на диске.
// В памяти и в UI они всегда открытые.
type DeployConfig struct {
	Host           string `json:"host"`
	Login          string `json:"login"`
	Password       string `json:"password"`
	PortsManual    bool   `json:"portsManual"`
	TunnelPassword string `json:"tunnelPassword"`
	TGAdminID      string `json:"tgAdminId"`
	TGBotToken     string `json:"tgBotToken"`
	SSHPort        string `json:"sshPort"`
	DTLSPort       string `json:"dtlsPort"`
	WGPort         string `json:"wgPort"`
}

// defaultDeployConfig — значения по умолчанию, совпадающие с фронтендом.
func defaultDeployConfig() DeployConfig {
	return DeployConfig{
		SSHPort:  "22",
		DTLSPort: "56000",
		WGPort:   "56001",
	}
}

func (s *Store) deployPath() string {
	return filepath.Join(s.configDir, "deploy.json")
}

// protectDeployConfig шифрует секреты перед записью на диск.
func protectDeployConfig(c DeployConfig) DeployConfig {
	c.Password = protectSecret(c.Password)
	c.TunnelPassword = protectSecret(c.TunnelPassword)
	c.TGBotToken = protectSecret(c.TGBotToken)
	return c
}

// unprotectDeployConfig расшифровывает секреты после чтения.
func unprotectDeployConfig(c DeployConfig) DeployConfig {
	c.Password = unprotectSecret(c.Password)
	c.TunnelPassword = unprotectSecret(c.TunnelPassword)
	c.TGBotToken = unprotectSecret(c.TGBotToken)
	return c
}

// GetDeployConfig читает deploy.json. Если файла нет — возвращает дефолты,
// чтобы UI сразу показывал рабочие порты.
func (s *Store) GetDeployConfig() DeployConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.deployPath())
	if err != nil {
		return defaultDeployConfig()
	}
	var c DeployConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return defaultDeployConfig()
	}
	if c.SSHPort == "" {
		c.SSHPort = "22"
	}
	if c.DTLSPort == "" {
		c.DTLSPort = "56000"
	}
	if c.WGPort == "" {
		c.WGPort = "56001"
	}
	return unprotectDeployConfig(c)
}

// SaveDeployConfig атомарно пишет deploy.json с зашифрованными секретами.
func (s *Store) SaveDeployConfig(c DeployConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(protectDeployConfig(c), "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.deployPath(), data)
}
