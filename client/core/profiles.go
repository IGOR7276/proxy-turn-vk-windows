package core

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
)

// Profile holds consistent browser fingerprint headers for TLS+HTTP requests.
type Profile struct {
	UserAgent       string `json:"user_agent"`
	SecChUa         string `json:"sec_ch_ua"`
	SecChUaMobile   string `json:"sec_ch_ua_mobile"`
	SecChUaPlatform string `json:"sec_ch_ua_platform"`
}

// SavedProfile is a saved real browser profile loaded from disk.
type SavedProfile struct {
	Profile
	DeviceJSON string `json:"device_json"`
	BrowserFp  string `json:"browser_fp"`
}

const profileFile = "vk_profile.json"

func LoadProfileFromDisk() (*SavedProfile, error) {
	data, err := os.ReadFile(profileFile)
	if err != nil {
		return nil, err
	}
	var sp SavedProfile
	if err := json.Unmarshal(data, &sp); err != nil {
		return nil, err
	}
	return &sp, nil
}



// profileList contains paired User-Agent and Client Hints strings.
var profileList = []Profile{
	// Windows Chrome
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="145", "Not-A.Brand";v="99", "Google Chrome";v="145"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="144", "Not-A.Brand";v="8", "Google Chrome";v="144"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},

	// Windows Edge
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36 Edg/146.0.0.0",
		SecChUa:         `"Chromium";v="146", "Not-A.Brand";v="24", "Microsoft Edge";v="146"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36 Edg/145.0.0.0",
		SecChUa:         `"Chromium";v="145", "Not-A.Brand";v="99", "Microsoft Edge";v="145"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},

	// macOS Chrome
	{
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"macOS"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="145", "Not-A.Brand";v="99", "Google Chrome";v="145"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"macOS"`,
	},

	// Linux Chrome
	{
		UserAgent:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="146", "Not-A.Brand";v="24", "Google Chrome";v="146"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Linux"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (X11; Ubuntu; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36",
		SecChUa:         `"Chromium";v="144", "Not-A.Brand";v="8", "Google Chrome";v="144"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Linux"`,
	},
	{
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0",
		SecChUa:         `"Firefox";v="132", "Not-A.Brand";v="8", "Mozilla Firefox";v="132"`,
		SecChUaMobile:   "?0",
		SecChUaPlatform: `"Windows"`,
	},
}

var androidProfiles = []Profile{
	{
		UserAgent:       "Mozilla/5.0 (Linux; Android 14; Pixel 8 Pro) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36",
		SecChUa:         `"Chromium";v="129", "Not-A.Brand";v="24", "Google Chrome";v="129"`,
		SecChUaMobile:   "?1",
		SecChUaPlatform: `"Android"`,
	},
}

var iosProfiles = []Profile{
	{
		UserAgent:       "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1",
		SecChUa:         `"Safari";v="17", "Not-A.Brand";v="24", "Apple Safari";v="17"`,
		SecChUaMobile:   "?1",
		SecChUaPlatform: `"iOS"`,
	},
}

var activeFingerprint = "chrome"

func SetActiveFingerprint(fp string) {
	activeFingerprint = fp
}

func GetActiveFingerprint() string {
	return activeFingerprint
}

// getRandomProfile returns a paired User-Agent and Client Hints profile.
func getRandomProfile() Profile {
	switch activeFingerprint {
	case "android":
		return androidProfiles[rand.Intn(len(androidProfiles))]
	case "ios":
		return iosProfiles[rand.Intn(len(iosProfiles))]
	case "safari":
		return profileList[4] // Using macOS Chrome as approximation for Safari if no specific Safari profile exists, or implement one. Actually, let's just use iOS for safari or macos.
	case "firefox":
		return profileList[len(profileList)-1]
	default:
		// chrome, or unknown, pick from first few chrome profiles
		return profileList[rand.Intn(3)]
	}
}

// --- VPN Profile Management (Web UI) ---

type VPNProfile struct {
	Name         string `json:"Name"`
	Peer         string `json:"Peer"`
	VkHash       string `json:"VkHash"`
	Password     string `json:"Password"`
	NumWorkers   int    `json:"NumWorkers"`
	ClientIds    string `json:"ClientIds"`
	Fingerprint  string `json:"Fingerprint"`
	CaptchaMode  string `json:"CaptchaMode"`
	UseWindowsWG bool   `json:"UseWindowsWG"`
	WGInterface  string `json:"WGInterface"`
	DeviceId     string `json:"DeviceId"`
	AutoConnect  bool   `json:"AutoConnect"`
}

var (
	vpnProfileMu   sync.Mutex
	vpnProfilePath string
	vpnProfileOnce sync.Once
)

func getVPNProfilePath() string {
	vpnProfileOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			vpnProfilePath = "vpn_profiles.json"
			return
		}
		vpnProfilePath = filepath.Join(filepath.Dir(exe), "vpn_profiles.json")
	})
	return vpnProfilePath
}

func loadProfilesFromFile() ([]VPNProfile, error) {
	vpnProfileMu.Lock()
	defer vpnProfileMu.Unlock()

	path := getVPNProfilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []VPNProfile{}, nil
		}
		return nil, err
	}

	var profiles []VPNProfile
	if err := json.Unmarshal(data, &profiles); err != nil {
		return nil, err
	}
	if profiles == nil {
		return []VPNProfile{}, nil
	}
	return profiles, nil
}

func saveProfilesToFile(profiles []VPNProfile) error {
	path := getVPNProfilePath()
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func saveProfile(p *VPNProfile) error {
	profiles, err := loadProfilesFromFile()
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}

	found := false
	for i, existing := range profiles {
		if existing.Name == p.Name {
			profiles[i] = *p
			found = true
			break
		}
	}
	if !found {
		profiles = append(profiles, *p)
	}

	return saveProfilesToFile(profiles)
}

func deleteProfile(name string) error {
	profiles, err := loadProfilesFromFile()
	if err != nil {
		return err
	}

	for i, pr := range profiles {
		if pr.Name == name {
			profiles = append(profiles[:i], profiles[i+1:]...)
			return saveProfilesToFile(profiles)
		}
	}
	return fmt.Errorf("profile not found: %s", name)
}
