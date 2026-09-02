package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/google/uuid"
)

// ─── VK Credential Sets (2 stable app_id with rotating fallback) ───

type VKCredentials struct {
	ClientID     string
	ClientSecret string
}

var vkCredentialsList = loadVKCredentials()

// loadVKCredentials loads credentials from WDTT_VK_CREDENTIALS env first,
// then falls back to embedded pairs. This allows updating credentials without
// rebuilding the binary.
func loadVKCredentials() []VKCredentials {
	if env := os.Getenv("WDTT_VK_CREDENTIALS"); env != "" {
		creds, err := parseVKCredentialsEnv(env)
		if err != nil {
			log.Printf("[VK Auth] WDTT_VK_CREDENTIALS parse error: %v; using fallback", err)
		} else if len(creds) > 0 {
			log.Printf("[VK Auth] Loaded %d VK credential set(s) from environment", len(creds))
			return creds
		}
	}
	log.Printf("[VK Auth] WARNING: using embedded VK credentials.")
	return []VKCredentials{
		{ClientID: "6287487", ClientSecret: "MuAxFaKDYDOICzGnEOhp"},
		{ClientID: "8202606", ClientSecret: "lMRsTiMCyPnp5vfoldmn"},
	}
}

func parseVKCredentialsEnv(env string) ([]VKCredentials, error) {
	var out []VKCredentials
	for _, pair := range strings.Split(env, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid credential pair %q (expected id:secret)", pair)
		}
		id, secret := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if id == "" || secret == "" {
			return nil, fmt.Errorf("empty id or secret in pair %q", pair)
		}
		out = append(out, VKCredentials{ClientID: id, ClientSecret: secret})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no credentials found")
	}
	return out, nil
}

// CallUnavailableError indicates a non-retryable VK call error (951/954/9xxx).
type CallUnavailableError struct {
	Code    int
	Message string
}

func (e *CallUnavailableError) Error() string {
	if e == nil {
		return "VK call is unavailable"
	}
	if e.Message != "" {
		return fmt.Sprintf("VK returns error: %s (error_code=%d)", e.Message, e.Code)
	}
	return fmt.Sprintf("VK call is unavailable (error_code=%d)", e.Code)
}

func asCallUnavailableError(err error) (*CallUnavailableError, bool) {
	var callErr *CallUnavailableError
	if errors.As(err, &callErr) {
		return callErr, true
	}
	return nil, false
}

func fatalCallError(resp map[string]interface{}) *CallUnavailableError {
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		return nil
	}
	code := vkErrorCode(errObj["error_code"])
	switch {
	case code == 951, code == 954:
	case code >= 9000 && code <= 9999:
	default:
		return nil
	}
	msg, _ := errObj["error_msg"].(string)
	return &CallUnavailableError{Code: code, Message: msg}
}

func vkErrorCode(raw interface{}) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

// Full list of known credentials to match against when setting active client IDs
var knownVKCredentials = map[string]VKCredentials{
	"6287487": {ClientID: "6287487", ClientSecret: "MuAxFaKDYDOICzGnEOhp"},
	"8202606": {ClientID: "8202606", ClientSecret: "lMRsTiMCyPnp5vfoldmn"},
}

func SetActiveClientIds(ids string) {
	if ids == "" {
		return
	}
	var newCreds []VKCredentials
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if cred, ok := knownVKCredentials[id]; ok {
			newCreds = append(newCreds, cred)
		}
	}
	if len(newCreds) > 0 {
		vkCredentialsList = newCreds
	}
}

func GetActiveClientIdsString() string {
	var ids []string
	for _, cred := range vkCredentialsList {
		ids = append(ids, cred.ClientID)
	}
	return strings.Join(ids, ", ")
}

const vkCredentialAttemptLimit = 4

// ─── Credential Caching ───

type TurnCredentials struct {
	Username    string
	Password    string
	ServerAddrs []string
	ExpiresAt   time.Time
	Link        string
}

type StreamCredentialsCache struct {
	creds         TurnCredentials
	mutex         sync.RWMutex
	errorCount    atomic.Int32
	lastErrorTime atomic.Int64
}

const (
	credentialLifetime = 10 * time.Minute
	cacheSafetyMargin  = 60 * time.Second
	maxCacheErrors     = 3
	errorWindow        = 10 * time.Second
)

var streamsPerCache = 10

func getCacheID(streamID int) int {
	return streamID / streamsPerCache
}

var credentialsStore = struct {
	mu     sync.RWMutex
	caches map[int]*StreamCredentialsCache
}{
	caches: make(map[int]*StreamCredentialsCache),
}

func getStreamCache(streamID int) *StreamCredentialsCache {
	cacheID := getCacheID(streamID)

	credentialsStore.mu.RLock()
	cache, exists := credentialsStore.caches[cacheID]
	credentialsStore.mu.RUnlock()

	if exists {
		return cache
	}

	credentialsStore.mu.Lock()
	defer credentialsStore.mu.Unlock()

	if cache, exists = credentialsStore.caches[cacheID]; exists {
		return cache
	}

	cache = &StreamCredentialsCache{}
	credentialsStore.caches[cacheID] = cache
	return cache
}

func (c *StreamCredentialsCache) invalidate(streamID int) {
	c.mutex.Lock()
	c.creds = TurnCredentials{}
	c.mutex.Unlock()

	c.errorCount.Store(0)
	c.lastErrorTime.Store(0)

	log.Printf("[STREAM %d] [VK Auth] Credentials cache invalidated", streamID)
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "Unauthorized") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "invalid credential") ||
		strings.Contains(errStr, "stale nonce")
}

func handleAuthError(streamID int) bool {
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	now := time.Now().Unix()

	if now-cache.lastErrorTime.Load() > int64(errorWindow.Seconds()) {
		cache.errorCount.Store(0)
	}

	count := cache.errorCount.Add(1)
	cache.lastErrorTime.Store(now)

	log.Printf("[STREAM %d] Auth error (cache=%d, count=%d/%d)", streamID, cacheID, count, maxCacheErrors)

	if count >= maxCacheErrors {
		log.Printf("[VK Auth] Multiple auth errors detected (%d), invalidating cache %d", count, cacheID)
		cache.invalidate(streamID)
		return true
	}
	return false
}

// ─── Captcha lockout ───

var globalCaptchaLockout atomic.Int64

const (
	captchaAutoWebViewTimeout     = 10 * time.Second
	captchaManualWebViewTimeout   = 60 * time.Second
	captchaSelectedWebViewTimeout = 120 * time.Second
)

// ─── Random delay ───

func vkDelayRandom(minMs, maxMs int) {
	ms := minMs + rand.Intn(maxMs-minMs+1)
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// ─── Cached credential fetcher ───

func getVkCredsCached(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	cache := getStreamCache(streamID)
	cacheID := getCacheID(streamID)

	cache.mutex.RLock()
	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		expires := time.Until(cache.creds.ExpiresAt)
		u, p := cache.creds.Username, cache.creds.Password
		addr := cache.creds.ServerAddrs[streamID%len(cache.creds.ServerAddrs)]
		addrs := cloneStringSlice(cache.creds.ServerAddrs)
		cache.mutex.RUnlock()
		log.Printf("[STREAM %d] [VK Auth] Using cached credentials (cache=%d, expires in %v, selected=%s, urls=%d)", streamID, cacheID, expires.Truncate(time.Second), addr, len(addrs))
		return u, p, addrs, nil
	}
	cache.mutex.RUnlock()

	cache.mutex.Lock()
	defer cache.mutex.Unlock()

	// Double-check inside lock
	if cache.creds.Link == link && time.Now().Before(cache.creds.ExpiresAt) && len(cache.creds.ServerAddrs) > 0 {
		return cache.creds.Username, cache.creds.Password, cloneStringSlice(cache.creds.ServerAddrs), nil
	}

	user, pass, addrs, err := fetchVkCredsSerialized(ctx, link, streamID)
	if err != nil {
		return "", "", nil, err
	}

	cache.creds = TurnCredentials{
		Username:    user,
		Password:    pass,
		ServerAddrs: addrs,
		ExpiresAt:   time.Now().Add(credentialLifetime - cacheSafetyMargin),
		Link:        link,
	}
	return user, pass, cloneStringSlice(addrs), nil
}

// ─── Serialized (throttled) fetcher ───

var (
	vkRequestMu           sync.Mutex
	globalLastVkFetchTime time.Time
)

func fetchVkCredsSerialized(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	vkRequestMu.Lock()
	defer vkRequestMu.Unlock()

	// Throttle: 3-6 seconds between requests
	minInterval := 3*time.Second + time.Duration(rand.Intn(3000))*time.Millisecond
	elapsed := time.Since(globalLastVkFetchTime)

	if !globalLastVkFetchTime.IsZero() && elapsed < minInterval {
		wait := minInterval - elapsed
		log.Printf("[STREAM %d] [VK Auth] Throttling: waiting %v to prevent rate limit...", streamID, wait.Truncate(time.Millisecond))
		select {
		case <-ctx.Done():
			return "", "", nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	defer func() {
		globalLastVkFetchTime = time.Now()
	}()

	return fetchVkCreds(ctx, link, streamID)
}

// ─── Main credential fetcher (rotates through stable credential sets) ───

func fetchVkCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	if GetVkAuthMode() == "account" {
		return fetchAccountVkCreds(ctx, link, streamID)
	}

	if time.Now().Unix() < globalCaptchaLockout.Load() {
		return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED: global lockout active")
	}

	// Try VKCalls path first (api.vk.me, usually no captcha) unless legacy mode forced.
	if GetVkAuthMode() != "legacy" {
		if user, pass, addrs, err := getVKCredsViaVKCallsPath(ctx, link, streamID); err == nil {
			log.Printf("[STREAM %d] [VK Auth] Success via VKCalls path", streamID)
			return user, pass, addrs, nil
		} else {
			if callErr, ok := asCallUnavailableError(err); ok {
				log.Printf("[STREAM %d] [VK Auth] VKCalls path returned non-retryable call error: %v", streamID, callErr)
				return "", "", nil, callErr
			}
			log.Printf("[STREAM %d] [VK Auth] VKCalls failed (%s), falling back to legacy", streamID, describeVKCallsFailure(err))
		}
	} else {
		log.Printf("[STREAM %d] [VK Auth] Legacy mode selected, skipping VK Calls path", streamID)
	}

	var lastErr error
	jar := tlsclient.NewCookieJar()

	for attempt := 0; attempt < vkCredentialAttemptLimit; attempt++ {
		creds := vkCredentialsList[attempt%len(vkCredentialsList)]
		log.Printf("[STREAM %d] [VK Auth] Trying credentials: client_id=%s (attempt %d/%d)", streamID, creds.ClientID, attempt+1, vkCredentialAttemptLimit)

		user, pass, addrs, err := getTokenChain(ctx, link, streamID, creds, jar)

		if err == nil {
			log.Printf("[STREAM %d] [VK Auth] Success with client_id=%s", streamID, creds.ClientID)
			return user, pass, addrs, nil
		}

		lastErr = err
		log.Printf("[STREAM %d] [VK Auth] Failed with client_id=%s: %v", streamID, creds.ClientID, err)

		if callErr, ok := asCallUnavailableError(err); ok {
			return "", "", nil, callErr
		}

		if strings.Contains(err.Error(), "CAPTCHA_WAIT_REQUIRED") || strings.Contains(err.Error(), "FATAL_CAPTCHA") {
			return "", "", nil, err
		}

		if strings.Contains(err.Error(), "error_code:29") || strings.Contains(err.Error(), "error_code: 29") || strings.Contains(err.Error(), "Rate limit") {
			log.Printf("[STREAM %d] [VK Auth] Rate limit detected, trying next credentials...", streamID)
		}

		if attempt%len(vkCredentialsList) == len(vkCredentialsList)-1 && attempt+1 < vkCredentialAttemptLimit {
			wait := time.Duration(900+rand.Intn(900)) * time.Millisecond
			log.Printf("[STREAM %d] [VK Auth] Both VK credentials failed, retrying stable pair after %v...", streamID, wait)
			select {
			case <-ctx.Done():
				return "", "", nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}

	return "", "", nil, fmt.Errorf("all VK credentials failed: %w", lastErr)
}

// ─── Token chain: anon_token → getCallPreview → getAnonymousToken → OK session → joinConversation → TURN creds ───

func getTokenChain(ctx context.Context, link string, streamID int, creds VKCredentials, jar tlsclient.CookieJar) (string, string, []string, error) {
	profile := getRandomProfile()

	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(),
		tlsclient.WithTimeoutSeconds(20),
		tlsclient.WithClientProfile(GetTLSProfile()),
		tlsclient.WithCookieJar(jar),
	)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to initialize tls_client: %w", err)
	}

	name := generateName()
	escapedName := neturl.QueryEscape(name)

	log.Printf("[STREAM %d] [VK Auth] Identity - Name: %s | UA: %s", streamID, name, profile.UserAgent)

	doRequest := func(data string, url string) (resp map[string]interface{}, err error) {
		parsedURL, err := neturl.Parse(url)
		if err != nil {
			return nil, fmt.Errorf("parse request URL: %w", err)
		}
		domain := parsedURL.Hostname()

		req, err := fhttp.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer([]byte(data)))
		if err != nil {
			return nil, err
		}

		req.Host = domain
		applyBrowserProfileFhttp(req, profile)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://vk.ru")
		req.Header.Set("Referer", "https://vk.ru/")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Priority", "u=1, i")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() {
			if closeErr := httpResp.Body.Close(); closeErr != nil {
				log.Printf("close response body: %s", closeErr)
			}
		}()

		body, err := io.ReadAll(httpResp.Body)
		if err != nil {
			return nil, err
		}

		err = json.Unmarshal(body, &resp)
		if err != nil {
			return nil, err
		}
		return resp, nil
	}

	// Step 1: get_anonym_token
	data := fmt.Sprintf("client_id=%s&token_type=messages&client_secret=%s&version=1&app_id=%s", creds.ClientID, creds.ClientSecret, creds.ClientID)
	resp, err := doRequest(data, "https://login.vk.ru/?act=get_anonym_token")
	if err != nil {
		return "", "", nil, err
	}
	dataMap, ok := resp["data"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("unexpected anon token response: %v", resp)
	}
	token1, ok := dataMap["access_token"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing access_token in response: %v", resp)
	}

	vkDelayRandom(100, 150)

	// Step 2: getCallPreview (mimics real VK client behavior)
	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&fields=photo_200&access_token=%s", link, token1)
	resp, err = doRequest(data, "https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id="+creds.ClientID)
	if err != nil {
		log.Printf("[STREAM %d] [VK Auth] Warning: getCallPreview failed: %v", streamID, err)
	} else if callErr := fatalCallError(resp); callErr != nil {
		log.Printf("[STREAM %d] [VK Auth] getCallPreview returned non-retryable call error: %v", streamID, callErr)
		return "", "", nil, callErr
	}

	vkDelayRandom(200, 400)

	// Step 3: getAnonymousToken (with captcha handling)
	data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, escapedName, token1)
	urlAddr := fmt.Sprintf("https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id=%s", creds.ClientID)

	var token2 string
	var savedProfile *SavedProfile
	savedProfile, _ = LoadProfileFromDisk()

	for attempt := 0; ; attempt++ {
		resp, err = doRequest(data, urlAddr)
		if err != nil {
			return "", "", nil, err
		}

		if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
			if callErr := fatalCallError(resp); callErr != nil {
				log.Printf("[STREAM %d] [VK Auth] getAnonymousToken returned non-retryable call error: %v", streamID, callErr)
				return "", "", nil, callErr
			}

			captchaErr := parseVkCaptchaError(errObj)
			if captchaErr != nil && captchaErr.RedirectURI != "" && captchaErr.SessionToken != "" {
				if attempt >= 3 {
					log.Printf("[STREAM %d] [Captcha] Max attempts reached", streamID)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
				}

				successToken, solveErr := solveCaptchaBySelectedMode(ctx, streamID, attempt+1, captchaErr, client, profile, savedProfile)
				if solveErr != nil {
					log.Printf("[STREAM %d] [Captcha] Solve failed: %v", streamID, solveErr)
					globalCaptchaLockout.Store(time.Now().Add(60 * time.Second).Unix())
					return "", "", nil, fmt.Errorf("CAPTCHA_WAIT_REQUIRED")
				}

				captchaAttempt := captchaErr.CaptchaAttempt
				if captchaAttempt == "0" || captchaAttempt == "" {
					captchaAttempt = "1"
				}

				data = fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&captcha_key=&captcha_sid=%s&is_sound_captcha=0&success_token=%s&captcha_ts=%s&captcha_attempt=%s&access_token=%s",
					link, escapedName, captchaErr.CaptchaSid, neturl.QueryEscape(successToken), captchaErr.CaptchaTs, captchaAttempt, token1)
				continue
			}
			return "", "", nil, fmt.Errorf("VK API error: %v", errObj)
		}

		respMap, okLoop := resp["response"].(map[string]interface{})
		if !okLoop {
			return "", "", nil, fmt.Errorf("unexpected getAnonymousToken response: %v", resp)
		}
		token2, okLoop = respMap["token"].(string)
		if !okLoop {
			return "", "", nil, fmt.Errorf("missing token in response: %v", resp)
		}
		break
	}

	vkDelayRandom(100, 150)

	// Step 4: OK.ru anonymLogin
	sessionData := fmt.Sprintf(`{"version":2,"device_id":"%s","client_version":1.1,"client_type":"SDK_JS"}`, uuid.New())
	data = fmt.Sprintf("session_data=%s&method=auth.anonymLogin&format=JSON&application_key=CGMMEJLGDIHBABABA", neturl.QueryEscape(sessionData))
	resp, err = doRequest(data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}
	token3, ok := resp["session_key"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing session_key in response: %v", resp)
	}

	vkDelayRandom(100, 150)

	// Step 5: joinConversationByLink → TURN creds
	data = fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&capabilities=2F7F&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=CGMMEJLGDIHBABABA&session_key=%s", link, token2, token3)
	resp, err = doRequest(data, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, err
	}

	tsRaw, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("missing turn_server in response: %v", resp)
	}
	user, ok := tsRaw["username"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing username in turn_server")
	}
	pass, ok := tsRaw["credential"].(string)
	if !ok {
		return "", "", nil, fmt.Errorf("missing credential in turn_server")
	}
	urlsRaw, ok := tsRaw["urls"].([]interface{})
	if !ok || len(urlsRaw) == 0 {
		return "", "", nil, fmt.Errorf("missing or empty urls in turn_server")
	}

	log.Printf("[STREAM %d] [VK Auth] TURN urls (%d total):", streamID, len(urlsRaw))
	for i, u := range urlsRaw {
		log.Printf("[STREAM %d] [VK Auth]   [%d] %v", streamID, i, u)
	}

	var addresses []string
	for _, u := range urlsRaw {
		urlStr, ok := u.(string)
		if !ok {
			continue
		}
		clean := strings.Split(urlStr, "?")[0]
		address := strings.TrimPrefix(strings.TrimPrefix(clean, "turn:"), "turns:")
		addresses = append(addresses, address)
	}

	if len(addresses) == 0 {
		return "", "", nil, fmt.Errorf("no valid TURN addresses found")
	}

	return user, pass, addresses, nil
}

func solveCaptchaBySelectedMode(
	ctx context.Context,
	streamID int,
	attempt int,
	captchaErr *VkCaptchaError,
	client tlsclient.HttpClient,
	profile Profile,
	savedProfile *SavedProfile,
) (string, error) {
	switch getCaptchaMode() {
	case "wv":
		log.Printf("[STREAM %d] [КАПЧА] WBV: режим из настроек Android (attempt %d)", streamID, attempt)
		return requestWebViewCaptcha(streamID, captchaErr, "selected", captchaSelectedWebViewTimeout)
	case "rjs":
		log.Printf("[STREAM %d] [КАПЧА] RJS: Go v2 выбран в настройках (attempt %d)", streamID, attempt)
		token, solveErr := solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 2)
		if solveErr == nil {
			return token, nil
		}
		if ctx.Err() != nil {
			return "", solveErr
		}
		if isCaptchaSessionExhausted(solveErr) {
			log.Printf("[STREAM %d] [КАПЧА] RJS: сессия исчерпана, перехожу на ручной WBV", streamID)
			return requestWebViewCaptcha(streamID, captchaErr, "manual", captchaManualWebViewTimeout)
		}
		log.Printf("[STREAM %d] [КАПЧА] RJS: ошибка, fallback на WBV Auto: %v", streamID, solveErr)
		return requestWebViewCaptcha(streamID, captchaErr, "auto", captchaAutoWebViewTimeout)
	}

	log.Printf("[STREAM %d] [КАПЧА] AUTO: старт цепочки (captcha attempt %d)", streamID, attempt)

	token, solveErr := solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 2)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: Go v2 решил капчу", streamID)
		return token, nil
	}
	if ctx.Err() != nil {
		return "", solveErr
	}
	lastErr := solveErr
	log.Printf("[STREAM %d] [КАПЧА] AUTO: Go v2 не решил за 2 попытки: %v", streamID, solveErr)

	if isCaptchaSessionExhausted(solveErr) {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: сессия исчерпана, перехожу на ручной WBV", streamID)
		token, solveErr = requestWebViewCaptcha(streamID, captchaErr, "manual", captchaManualWebViewTimeout)
		if solveErr == nil {
			return token, nil
		}
		return "", fmt.Errorf("automatic captcha chain failed: %w; manual fallback failed: %v", lastErr, solveErr)
	}

	for wbvAttempt := 1; wbvAttempt <= 2; wbvAttempt++ {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto попытка %d/2 (timeout %s)", streamID, wbvAttempt, captchaAutoWebViewTimeout)
		token, solveErr = requestWebViewCaptcha(streamID, captchaErr, "auto", captchaAutoWebViewTimeout)
		if solveErr == nil {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto решил капчу", streamID)
			return token, nil
		}
		if ctx.Err() != nil {
			return "", solveErr
		}
		lastErr = solveErr
		if isWebViewCaptchaTimeout(solveErr) {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto timeout %d/2", streamID, wbvAttempt)
		} else {
			log.Printf("[STREAM %d] [КАПЧА] AUTO: WBV Auto ошибка %d/2: %v", streamID, wbvAttempt, solveErr)
		}

		timer := time.NewTimer(time.Duration(250+rand.Intn(250)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}

	log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 попытка после WBV", streamID)
	token, solveErr = solveVkCaptchaV2Attempts(ctx, captchaErr, client, profile, savedProfile, 1)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 решила капчу", streamID)
		return token, nil
	}
	if ctx.Err() != nil {
		return "", solveErr
	}
	lastErr = solveErr
	log.Printf("[STREAM %d] [КАПЧА] AUTO: финальная Go v2 ошибка: %v", streamID, solveErr)

	log.Printf("[STREAM %d] [КАПЧА] AUTO: автоцепочка не прошла, открыт ручной WebView", streamID)
	token, solveErr = requestWebViewCaptcha(streamID, captchaErr, "manual", captchaManualWebViewTimeout)
	if solveErr == nil {
		log.Printf("[STREAM %d] [КАПЧА] AUTO: ручной WebView решил капчу", streamID)
		return token, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("automatic captcha chain failed: %w; manual fallback failed: %v", lastErr, solveErr)
	}
	return "", solveErr
}

func requestWebViewCaptcha(streamID int, captchaErr *VkCaptchaError, mode string, timeout time.Duration) (string, error) {
	if CaptchaResultChan == nil || captchaErr == nil || captchaErr.RedirectURI == "" || captchaErr.SessionToken == "" {
		return "", fmt.Errorf("webview captcha data is incomplete")
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode != "manual" && mode != "selected" {
		mode = "auto"
	}
	if timeout <= 0 {
		timeout = captchaAutoWebViewTimeout
	}

	drainCaptchaResult()

	// Если зарегистрирован хендлер (Wails режим), вызываем его синхронно
	if WebViewCaptchaHandler != nil {
		waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		type handlerResult struct {
			token string
			err   error
		}
		resultCh := make(chan handlerResult, 1)
		go func() {
			token, err := WebViewCaptchaHandler(mode, captchaErr.RedirectURI, captchaErr.SessionToken)
			resultCh <- handlerResult{token, err}
		}()
		select {
		case r := <-resultCh:
			if r.err != nil {
				return "", r.err
			}
			log.Printf("[STREAM %d] [КАПЧА] WBV: %s solve succeeded (handler)", streamID, mode)
			return r.token, nil
		case <-waitCtx.Done():
			return "", fmt.Errorf("webview captcha timed out (handler)")
		}
	}

	// CLI/Android путь: печатаем в stdout и ждём на канале
	fmt.Printf("CAPTCHA_SOLVE|%s|%s|%s\n", mode, captchaErr.RedirectURI, captchaErr.SessionToken)

	// Emit captcha_required для внешнего решателя (frontend/Android)
			emitEvent(Event{
			Type: EventEvent,
			Name: "captcha_required",
			Data: fmt.Sprintf("%s|%s|%s", mode, captchaErr.RedirectURI, captchaErr.SessionToken),
		})

	waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case result := <-CaptchaResultChan:
		result = strings.TrimSpace(result)
		if result == "" {
			return "", fmt.Errorf("webview captcha returned empty result")
		}
		lowerResult := strings.ToLower(result)
		if lowerResult == "error:timeout" {
			return "", fmt.Errorf("webview captcha timed out")
		}
		if strings.HasPrefix(lowerResult, "error:") {
			return "", fmt.Errorf("webview captcha failed: %s", result)
		}
		log.Printf("[STREAM %d] [КАПЧА] WBV: %s solve succeeded", streamID, mode)
		return result, nil
	case <-waitCtx.Done():
		return "", fmt.Errorf("webview captcha timed out")
	}
}

func isWebViewCaptchaTimeout(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "timed out")
}

// ─── GetCreds returns TURN credentials for a given stream ───

func GetCreds(ctx context.Context, link string, streamID int) (string, string, []string, error) {
	return getVkCredsCached(ctx, link, streamID)
}

// ─── DNS dialer setup ───

// publicDNSServers — fallback-резолверы на случай, когда системный DNS
// молчит (перехват/фильтрация провайдером). Пробуются ТОЛЬКО если системный
// сервер не ответил, и с реальной проверкой ответа (см. dnsServerResponds).
var publicDNSServers = []string{
	"77.88.8.8:53",      // Яндекс
	"77.88.8.1:53",      // Яндекс резерв
	"8.8.8.8:53",        // Google
	"1.1.1.1:53",        // Cloudflare
	"9.9.9.9:53",        // Quad9
	"208.67.222.222:53", // OpenDNS
}

// dnsGoodMu/dnsGoodServer кэширует последний DNS-сервер, который реально ответил,
// чтобы не перебирать весь список на каждый lookup.
var (
	dnsGoodMu     sync.Mutex
	dnsGoodServer string
)

func cachedGoodDNS() string {
	dnsGoodMu.Lock()
	defer dnsGoodMu.Unlock()
	return dnsGoodServer
}

func setCachedGoodDNS(server string) {
	dnsGoodMu.Lock()
	dnsGoodServer = server
	dnsGoodMu.Unlock()
}

// setupGlobalResolver подменяет net.DefaultResolver так, чтобы на этапе
// bootstrap (до поднятия туннеля) DNS резолвился надёжно.
//
// Порядок кандидатов: кэшированный рабочий DNS → системный DNS → публичные fallback.
// Каждый сервер проверяется настоящим DNS-запросом с коротким дедлайном.
// Сначала весь список по UDP, затем по TCP (UDP/53 может быть заблокирован).
func setupGlobalResolver() {
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			candidates := make([]string, 0, len(publicDNSServers)+2)
			seen := make(map[string]struct{})
			add := func(s string) {
				if s == "" {
					return
				}
				if _, ok := seen[s]; ok {
					return
				}
				seen[s] = struct{}{}
				candidates = append(candidates, s)
			}

			add(cachedGoodDNS())
			add(address)
			for _, s := range publicDNSServers {
				add(s)
			}

			var lastErr error
			// Пас 1 — UDP (основной транспорт DNS), пас 2 — TCP (fallback,
			// если UDP/53 фильтруется).
			for _, netw := range []string{"udp", "tcp"} {
				for _, srv := range candidates {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if !dnsServerResponds(ctx, dialer, netw, srv) {
						lastErr = fmt.Errorf("DNS probe failed for %s/%s", netw, srv)
						continue
					}
					conn, err := dialer.DialContext(ctx, netw, srv)
					if err != nil {
						lastErr = err
						continue
					}
					setCachedGoodDNS(srv)
					return conn, nil
				}
			}

			if lastErr == nil {
				lastErr = fmt.Errorf("no DNS server responded (%d candidates)", len(candidates))
			}
			return nil, lastErr
		},
	}
}

// dnsServerResponds проверяет, что DNS-сервер по адресу server отвечает на
// запросы по указанному транспорту (udp/tcp). Шлёт минимальный A-запрос для
// api.vk.me и ждёт валидный ответ с коротким дедлайном. Для UDP создание
// сокета само по себе ничего не значит, поэтому обязательна отправка пакета.
func dnsServerResponds(ctx context.Context, dialer *net.Dialer, network, server string) bool {
	const probeTimeout = 900 * time.Millisecond
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	conn, err := dialer.DialContext(probeCtx, network, server)
	if err != nil {
		return false
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(probeTimeout))

	query := buildDNSProbeQuery("api.vk.me")
	if network == "tcp" {
		// DNS over TCP: сообщение предваряется 2-байтовым префиксом длины.
		framed := append([]byte{byte(len(query) >> 8), byte(len(query))}, query...)
		if _, err := conn.Write(framed); err != nil {
			return false
		}
	} else if _, err := conn.Write(query); err != nil {
		return false
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		return false
	}

	// Проверяем, что это настоящий DNS-ответ: QR=1 и RCODE=0.
	// Биты: [2] = QR(1) | OPCODE(4) | AA(1) | TC(1) | RD(1)
	//       [3] = RA(1) | Z(3)      | RCODE(4)
	return buf[2]&0x80 != 0 && buf[3]&0x0F == 0
}

// buildDNSProbeQuery собирает минимальный DNS-запрос типа A для указанного имени.
func buildDNSProbeQuery(name string) []byte {
	q := []byte{
		0x12, 0x34, // ID
		0x01, 0x00, // flags: RD=1
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, // ANCOUNT
		0x00, 0x00, // NSCOUNT
		0x00, 0x00, // ARCOUNT
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			continue
		}
		q = append(q, byte(len(label)))
		q = append(q, []byte(label)...)
	}
	q = append(q, 0x00)       // конец QNAME
	q = append(q, 0x00, 0x01) // QTYPE = A
	q = append(q, 0x00, 0x01) // QCLASS = IN
	return q
}

