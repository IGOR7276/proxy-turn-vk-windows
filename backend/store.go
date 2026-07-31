package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AppSettings — persisted application settings.
type AppSettings struct {
	AutoStart     bool   `json:"autoStart"`
	ObfsMode      string `json:"obfsMode,omitempty"`
	CaptchaMode   string `json:"captchaMode,omitempty"`
	VkAuthMode    string `json:"vkAuthMode,omitempty"`
	CloseAction   string `json:"closeAction,omitempty"`
	UpdateChannel string `json:"updateChannel,omitempty"`
}

// SubscriptionProfile — profile inside a subscription JSON.
type SubscriptionProfile struct {
	Name     string `json:"name"`
	Peer     string `json:"peer"`
	Password string `json:"password"`
	Hashes   string `json:"hashes,omitempty"`
	Workers  int    `json:"workers,omitempty"`
	Port     int    `json:"port,omitempty"`
}

// SubscriptionData — raw JSON from subscription URL.
type SubscriptionData struct {
	SubscriptionName string                `json:"subscriptionName"`
	Description      string                `json:"description,omitempty"`
	TrafficUsedMb    float64               `json:"trafficUsedMb,omitempty"`
	TrafficLimitMb   float64               `json:"trafficLimitMb,omitempty"`
	UpdatedAt        string                `json:"updatedAt,omitempty"`
	Version          int                   `json:"version,omitempty"`
	Profiles         []SubscriptionProfile `json:"profiles"`
}

// Subscription — persisted subscription metadata.
type Subscription struct {
	ID               string  `json:"id"`
	URL              string  `json:"url"`
	Name             string  `json:"name"`
	Description      string  `json:"description,omitempty"`
	TrafficUsedMb    float64 `json:"trafficUsedMb,omitempty"`
	TrafficLimitMb   float64 `json:"trafficLimitMb,omitempty"`
	UpdatedAt        string  `json:"updatedAt,omitempty"`
	Version          int     `json:"version,omitempty"`
	LastSyncAt       string  `json:"lastSyncAt,omitempty"`
	LastSyncError    string  `json:"lastSyncError,omitempty"`
}

func (s *AppSettings) fillDefaults() {
	if s.ObfsMode == "" {
		s.ObfsMode = "audio"
	}
	if s.CaptchaMode == "" {
		s.CaptchaMode = "auto"
	}
	if s.VkAuthMode == "" {
		s.VkAuthMode = "anonymous"
	}
	if s.CloseAction == "" {
		s.CloseAction = "ask"
	}
	if s.UpdateChannel == "" {
		s.UpdateChannel = "stable"
	}
}

// Store provides atomic read/write of config.json and profiles.
type Store struct {
	mu       sync.RWMutex
	settings AppSettings
	configDir string
}

// NewStore creates config directories and loads existing settings.
func NewStore() *Store {
	dir := configDir()
	_ = os.MkdirAll(dir, 0755)
	_ = os.MkdirAll(filepath.Join(dir, "profiles"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "subscriptions"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0755)

	s := &Store{configDir: dir}
	s.settings = s.loadSettingsLocked()
	s.settings.fillDefaults()
	return s
}

func (s *Store) settingsPath() string {
	return filepath.Join(s.configDir, "config.json")
}

func (s *Store) loadSettingsLocked() AppSettings {
	data, err := os.ReadFile(s.settingsPath())
	if err != nil {
		return AppSettings{}
	}
	var st AppSettings
	if err := json.Unmarshal(data, &st); err != nil {
		return AppSettings{}
	}
	return st
}

func (s *Store) saveSettingsLocked(st AppSettings) error {
	st.fillDefaults()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.settingsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.settingsPath())
}

// GetSettings returns a copy of current settings.
func (s *Store) GetSettings() AppSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.settings
	st.fillDefaults()
	return st
}

// SaveSettings atomically persists settings.
func (s *Store) SaveSettings(st AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.saveSettingsLocked(st); err != nil {
		return err
	}
	s.settings = st
	return nil
}

// UpdateSettings applies a patch function atomically.
func (s *Store) UpdateSettings(fn func(AppSettings) AppSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := fn(s.settings)
	if err := s.saveSettingsLocked(st); err != nil {
		return err
	}
	s.settings = st
	return nil
}

// LoadProfile reads a profile by name (from main profiles dir or subscription dir).
func (s *Store) LoadProfile(name string) (*ProfileData, error) {
	if p, err := loadProfile(name); err == nil {
		return p, nil
	}
	// Fallback: search in subscription directories.
	entries, _ := os.ReadDir(s.subscriptionsDir())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(s.subscriptionsDir(), e.Name(), safeFileName(name)+".json")
		if data, err := os.ReadFile(path); err == nil {
			var p ProfileData
			if err := json.Unmarshal(data, &p); err == nil {
				return &p, nil
			}
		}
	}
	return nil, fmt.Errorf("profile %q not found", name)
}

// SaveProfile writes a profile atomically (always to main profiles dir).
func (s *Store) SaveProfile(name string, p ProfileData) error {
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := profilePath(name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DeleteProfile removes a profile file.
func (s *Store) DeleteProfile(name string) error {
	return os.Remove(profilePath(name))
}

// ListProfiles returns a map of name -> ProfileData.
func (s *Store) ListProfiles() (map[string]ProfileData, error) {
	dir := filepath.Join(s.configDir, "profiles")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ProfileData{}, nil
		}
		return nil, err
	}
	out := make(map[string]ProfileData, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		p, err := loadProfile(name)
		if err != nil {
			continue
		}
		out[name] = *p
	}
	return out, nil
}

// subscriptionsPath returns path to subscriptions.json.
func (s *Store) subscriptionsPath() string {
	return filepath.Join(s.configDir, "subscriptions.json")
}

// subscriptionsDir returns directory for subscription-sourced profiles.
func (s *Store) subscriptionsDir() string {
	return filepath.Join(s.configDir, "subscriptions")
}

func (s *Store) loadSubscriptionsLocked() []Subscription {
	data, err := os.ReadFile(s.subscriptionsPath())
	if err != nil {
		return nil
	}
	var subs []Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil
	}
	return subs
}

func (s *Store) saveSubscriptionsLocked(subs []Subscription) error {
	data, err := json.MarshalIndent(subs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.subscriptionsPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.subscriptionsPath())
}

// GetSubscriptionProfiles returns profiles from all subscription directories.
func (s *Store) GetSubscriptionProfiles() map[string]ProfileData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]ProfileData)
	entries, _ := os.ReadDir(s.subscriptionsDir())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		profFiles, _ := os.ReadDir(filepath.Join(s.subscriptionsDir(), e.Name()))
		for _, pf := range profFiles {
			if pf.IsDir() || !strings.HasSuffix(pf.Name(), ".json") {
				continue
			}
			path := filepath.Join(s.subscriptionsDir(), e.Name(), pf.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var p ProfileData
			if err := json.Unmarshal(data, &p); err != nil {
				continue
			}
			name := strings.TrimSuffix(pf.Name(), ".json")
			out[name] = p
		}
	}
	return out
}

// ListSubscriptions returns persisted subscriptions.
func (s *Store) ListSubscriptions() []Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadSubscriptionsLocked()
}

// AddSubscription adds a new subscription and syncs it.
func (s *Store) AddSubscription(url string) (Subscription, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return Subscription{}, fmt.Errorf("URL is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return Subscription{}, fmt.Errorf("URL must be http or https")
	}

	data, err := fetchSubscription(url)
	if err != nil {
		return Subscription{}, fmt.Errorf("fetch subscription: %w", err)
	}

	sub := Subscription{
		ID:   fmt.Sprintf("%d", time.Now().UnixNano()),
		URL:  url,
		Name: data.SubscriptionName,
	}
	s.applyData(&sub, data)

	s.mu.Lock()
	defer s.mu.Unlock()
	subs := s.loadSubscriptionsLocked()
	subs = append(subs, sub)
	if err := s.saveSubscriptionsLocked(subs); err != nil {
		return Subscription{}, err
	}
	if err := s.syncSubscriptionLocked(&sub, data); err != nil {
		return sub, fmt.Errorf("sync failed: %w", err)
	}
	return sub, nil
}

// UpdateSubscription re-fetches a subscription by id.
func (s *Store) UpdateSubscription(id string) (Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.loadSubscriptionsLocked()
	for i := range subs {
		if subs[i].ID != id {
			continue
		}
		data, err := fetchSubscription(subs[i].URL)
		if err != nil {
			subs[i].LastSyncError = err.Error()
			subs[i].LastSyncAt = time.Now().UTC().Format(time.RFC3339)
			_ = s.saveSubscriptionsLocked(subs)
			return subs[i], fmt.Errorf("fetch subscription: %w", err)
		}
		s.applyData(&subs[i], data)
		if err := s.syncSubscriptionLocked(&subs[i], data); err != nil {
			subs[i].LastSyncError = err.Error()
			subs[i].LastSyncAt = time.Now().UTC().Format(time.RFC3339)
			_ = s.saveSubscriptionsLocked(subs)
			return subs[i], fmt.Errorf("sync failed: %w", err)
		}
		subs[i].LastSyncError = ""
		subs[i].LastSyncAt = time.Now().UTC().Format(time.RFC3339)
		_ = s.saveSubscriptionsLocked(subs)
		return subs[i], nil
	}
	return Subscription{}, fmt.Errorf("subscription not found")
}

// DeleteSubscription removes a subscription and its profiles.
func (s *Store) DeleteSubscription(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subs := s.loadSubscriptionsLocked()
	var found *Subscription
	var remaining []Subscription
	for _, sub := range subs {
		if sub.ID == id {
			found = &sub
			continue
		}
		remaining = append(remaining, sub)
	}
	if found == nil {
		return fmt.Errorf("subscription not found")
	}
	if err := s.saveSubscriptionsLocked(remaining); err != nil {
		return err
	}
	return os.RemoveAll(s.subscriptionProfilesDir(found.ID))
}

func (s *Store) subscriptionProfilesDir(id string) string {
	return filepath.Join(s.subscriptionsDir(), id)
}

func (s *Store) applyData(sub *Subscription, data *SubscriptionData) {
	sub.Name = data.SubscriptionName
	if sub.Name == "" {
		sub.Name = "Подписка"
	}
	sub.Description = data.Description
	sub.TrafficUsedMb = data.TrafficUsedMb
	sub.TrafficLimitMb = data.TrafficLimitMb
	sub.UpdatedAt = data.UpdatedAt
	sub.Version = data.Version
}

func (s *Store) syncSubscriptionLocked(sub *Subscription, data *SubscriptionData) error {
	dir := s.subscriptionProfilesDir(sub.ID)
	_ = os.MkdirAll(dir, 0755)

	// Remove old subscription profiles first.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}

	for _, p := range data.Profiles {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		var hashes []string
		if p.Hashes != "" {
			hashes = strings.Split(p.Hashes, ",")
			for i := range hashes {
				hashes[i] = strings.TrimSpace(hashes[i])
			}
		}
		prof := ProfileData{
			PeerAddr: p.Peer,
			Password: p.Password,
			Hashes:   hashes,
			TurnHost: "",
			TurnPort: "",
			DeviceID: "",
			Listen:   "",
		}
		if p.Port > 0 {
			prof.Listen = fmt.Sprintf("127.0.0.1:%d", p.Port)
		}
		data, err := json.MarshalIndent(prof, "", "  ")
		if err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, safeFileName(name)+".json"), data, 0600)
	}
	return nil
}

func fetchSubscription(url string) (*SubscriptionData, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	text := strings.TrimSpace(string(body))
	// Try base64 decode first.
	if !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "[") {
		if decoded, err := base64.StdEncoding.DecodeString(text); err == nil {
			text = string(decoded)
		}
	}

	var data SubscriptionData
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		return nil, err
	}
	if data.SubscriptionName == "" {
		data.SubscriptionName = "Подписка"
	}
	// Support servers alias.
	if len(data.Profiles) == 0 {
		var alias struct {
			Servers []SubscriptionProfile `json:"servers"`
		}
		if err := json.Unmarshal([]byte(text), &alias); err == nil && len(alias.Servers) > 0 {
			data.Profiles = alias.Servers
		}
	}
	if len(data.Profiles) == 0 {
		return nil, fmt.Errorf("no profiles found")
	}
	return &data, nil
}

func safeFileName(name string) string {
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_",
		"|", "_", "?", "_", "*", "_",
	)
	return strings.TrimSpace(replacer.Replace(name))
}
