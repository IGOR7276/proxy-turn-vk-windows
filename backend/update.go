package backend

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// UpdateInfo holds the result of a version check.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	Body      string `json:"body"`
	AssetURL  string `json:"assetUrl,omitempty"`
}

// CheckUpdate checks GitHub releases for a newer version matching the current OS/arch.
func (a *App) CheckUpdate() UpdateInfo {
	return a.checkUpdateGitHub()
}

func (a *App) checkUpdateGitHub() UpdateInfo {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/IGOR7276/proxy-turn-vk-windows/releases/latest", nil)
	if err != nil {
		return UpdateInfo{}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return UpdateInfo{}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UpdateInfo{}
	}

	var release struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return UpdateInfo{}
	}
	if release.TagName == "" {
		return UpdateInfo{}
	}

	current := strings.TrimPrefix(appVersion, "v")
	latest := strings.TrimPrefix(release.TagName, "v")
	if !isNewerVersion(current, latest) {
		return UpdateInfo{}
	}

	assetName := updateAssetName()
	var assetURL string
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, assetName) {
			assetURL = asset.URL
			break
		}
	}

	return UpdateInfo{
		Available: true,
		Version:   latest,
		URL:       "https://github.com/IGOR7276/proxy-turn-vk-windows/releases/tag/v" + latest,
		Body:      release.Body,
		AssetURL:  assetURL,
	}
}

func updateAssetName() string {
	suffix := "windows-amd64.zip"
	if runtime.GOARCH == "arm64" {
		suffix = "windows-arm64.zip"
	}
	return "wdtt-" + suffix
}

// isNewerVersion reports whether latest is semantically newer than current.
func isNewerVersion(current, latest string) bool {
	cur := parseVersion(current)
	lat := parseVersion(latest)
	for i := 0; i < 3; i++ {
		if lat[i] > cur[i] {
			return true
		}
		if lat[i] < cur[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, "-", 2)
	nums := strings.Split(parts[0], ".")
	var out [3]int
	for i := 0; i < len(nums) && i < 3; i++ {
		n, err := strconv.Atoi(nums[i])
		if err != nil {
			log.Printf("[Update] failed to parse version component %q: %v", nums[i], err)
			continue
		}
		out[i] = n
	}
	return out
}
