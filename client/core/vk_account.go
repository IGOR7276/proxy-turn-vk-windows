package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const accountCredsLifetime = 9 * time.Minute

type injectedTurnCreds struct {
	Username  string
	Password  string
	URLs      []string
	ExpiresAt time.Time
}

type TurnCredsPayload struct {
	User string   `json:"u"`
	Pass string   `json:"p"`
	URLs []string `json:"urls"`
}

var (
	vkAuthModeValue     atomic.Value
	injectedCredsMu     sync.RWMutex
	injectedCredsByLink map[string]injectedTurnCreds
)

func init() {
	vkAuthModeValue.Store("anonymous")
	injectedCredsByLink = make(map[string]injectedTurnCreds)
}

var TurnCredsResultChan = make(chan TurnCredsPayload, 1)

func SetVkAuthMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "anonymous" {
		mode = "account"
	}
	vkAuthModeValue.Store(mode)
	return mode
}

func GetVkAuthMode() string {
	mode, _ := vkAuthModeValue.Load().(string)
	if mode == "" {
		return "anonymous"
	}
	return mode
}

func drainTurnCredsResult() {
	select {
	case <-TurnCredsResultChan:
	default:
	}
}

func InjectTurnCreds(link, user, pass string, urls []string) {
	link = strings.TrimSpace(link)
	if link == "" || user == "" || pass == "" || len(urls) == 0 {
		return
	}
	injectedCredsMu.Lock()
	defer injectedCredsMu.Unlock()
	injectedCredsByLink[link] = injectedTurnCreds{
		Username:  user,
		Password:  pass,
		URLs:      cloneStringSlice(urls),
		ExpiresAt: time.Now().Add(accountCredsLifetime),
	}
}

func GetInjectedTurnCreds(link string) (user, pass string, urls []string, ok bool) {
	link = strings.TrimSpace(link)
	injectedCredsMu.RLock()
	creds, exists := injectedCredsByLink[link]
	injectedCredsMu.RUnlock()
	if !exists {
		return "", "", nil, false
	}
	if time.Now().After(creds.ExpiresAt) {
		injectedCredsMu.Lock()
		delete(injectedCredsByLink, link)
		injectedCredsMu.Unlock()
		return "", "", nil, false
	}
	return creds.Username, creds.Password, cloneStringSlice(creds.URLs), true
}

func InvalidateInjectedTurnCreds(link string) {
	link = strings.TrimSpace(link)
	injectedCredsMu.Lock()
	delete(injectedCredsByLink, link)
	injectedCredsMu.Unlock()
}

func HandleTurnCredsPayload(line string) {
	line = strings.TrimPrefix(line, "TURN_CREDS|")
	if strings.HasPrefix(line, "error:") {
		drainTurnCredsResult()
		TurnCredsResultChan <- TurnCredsPayload{}
		return
	}

	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return
	}
	link := strings.TrimSpace(parts[0])
	payloadRaw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		log.Printf("[VK Auth] TURN_CREDS decode error: %v", err)
		return
	}
	var payload TurnCredsPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		log.Printf("[VK Auth] TURN_CREDS JSON error: %v", err)
		return
	}
	if payload.User == "" || payload.Pass == "" || len(payload.URLs) == 0 {
		log.Printf("[VK Auth] TURN_CREDS rejected: empty fields for link %s", link)
		return
	}
	InjectTurnCreds(link, payload.User, payload.Pass, payload.URLs)
	drainTurnCredsResult()
	TurnCredsResultChan <- payload
	log.Printf("[VK Auth] Account TURN creds received for link %s (urls=%d)", link, len(payload.URLs))
}

func requestAccountTurnCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	link = strings.TrimSpace(link)
	if u, p, urls, ok := GetInjectedTurnCreds(link); ok {
		return u, p, urls, nil
	}

	drainTurnCredsResult()
	log.Printf("[STREAM %d] [VK Auth] VK_AUTH_REQUIRED|%s", streamID, link)
	log.Printf("[STREAM %d] [VK Auth] Waiting for account TURN creds (link=%s...)", streamID, shortLink(link))

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	select {
	case payload := <-TurnCredsResultChan:
		if payload.User == "" {
			return "", "", nil, fmt.Errorf("VK account auth cancelled")
		}
		InjectTurnCreds(link, payload.User, payload.Pass, payload.URLs)
		if u, p, urls, ok := GetInjectedTurnCreds(link); ok {
			return u, p, urls, nil
		}
		return payload.User, payload.Pass, cloneStringSlice(payload.URLs), nil
	case <-waitCtx.Done():
		return "", "", nil, fmt.Errorf("VK account auth timeout: %w", waitCtx.Err())
	}
}

func shortLink(link string) string {
	if len(link) <= 8 {
		return link
	}
	return link[:8]
}

func fetchAccountVkCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	if u, p, urls, ok := GetInjectedTurnCreds(link); ok {
		log.Printf("[STREAM %d] [VK Auth] Using injected account creds (urls=%d)", streamID, len(urls))
		return u, p, urls, nil
	}
	return requestAccountTurnCreds(ctx, link, streamID)
}

func CountInjectedCreds() map[string]time.Time {
	injectedCredsMu.RLock()
	defer injectedCredsMu.RUnlock()
	result := make(map[string]time.Time, len(injectedCredsByLink))
	for k, v := range injectedCredsByLink {
		result[k] = v.ExpiresAt
	}
	return result
}
