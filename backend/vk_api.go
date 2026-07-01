package backend

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"wg-turn-client/core"
)

const (
	vkClientID     = "6287487"
	vkClientSecret = "MuAxFaKDYDOICzGnEOhp"
	okAppKey       = "CGMMEJLGDIHBABABA"
)

type turnCredsPayload struct {
	User string   `json:"u"`
	Pass string   `json:"p"`
	URLs []string `json:"urls"`
}

func nameGenerator() string {
	first := []string{"Александр", "Максим", "Дмитрий", "Иван", "Сергей", "Анна", "Елена", "Ольга", "Наталья", "Екатерина"}
	last := []string{"Иванов", "Смирнов", "Кузнецов", "Попов", "Васильев", "Петров", "Соколов", "Михайлов", "Новиков", "Федоров"}
	return first[time.Now().UnixNano()%int64(len(first))] + " " + last[time.Now().UnixNano()/int64(len(last))%int64(len(last))]
}

func fetchVkTurnCreds(token, link, deviceID string) (user, pass string, urls []string, err error) {
	if deviceID == "" {
		b := make([]byte, 16)
		rand.Read(b)
		deviceID = "wdtt-" + hex.EncodeToString(b)
	}

	jar := tlsclient.NewCookieJar()
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), []tlsclient.HttpClientOption{
		tlsclient.WithCookieJar(jar),
		tlsclient.WithClientProfile(core.GetTLSProfile()),
		tlsclient.WithInsecureSkipVerify(),
	}...)
	if err != nil {
		return "", "", nil, fmt.Errorf("create client: %w", err)
	}

	doReq := func(data, apiURL string) (map[string]interface{}, error) {
		u, _ := url.Parse(apiURL)
		req, err := fhttp.NewRequest("POST", apiURL, bytes.NewBuffer([]byte(data)))
		if err != nil {
			return nil, err
		}
		req.Host = u.Hostname()
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", "https://vk.ru")
		req.Header.Set("Referer", "https://vk.ru/")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Dest", "empty")

		httpResp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer httpResp.Body.Close()

		body, readErr := io.ReadAll(httpResp.Body)
		if readErr != nil {
			return nil, readErr
		}
		var resp map[string]interface{}
		if jsonErr := json.Unmarshal(body, &resp); jsonErr != nil {
			return nil, fmt.Errorf("json decode: %w (body: %s)", jsonErr, string(body))
		}
		return resp, nil
	}

	// 1) get_anonym_token (skip if token already provided, else anonymous)
	anonToken := token
	if anonToken == "" {
		data := fmt.Sprintf("client_id=%s&token_type=messages&client_secret=%s&version=1&app_id=%s", vkClientID, vkClientSecret, vkClientID)
		resp, err := doReq(data, "https://login.vk.ru/?act=get_anonym_token")
		if err != nil {
			return "", "", nil, fmt.Errorf("get_anonym_token: %w", err)
		}
		dataMap, ok := resp["data"].(map[string]interface{})
		if !ok {
			return "", "", nil, fmt.Errorf("unexpected anon token response: %v", resp)
		}
		anonToken, ok = dataMap["access_token"].(string)
		if !ok || anonToken == "" {
			return "", "", nil, fmt.Errorf("missing access_token: %v", resp)
		}
	}

	time.Sleep(time.Duration(100+time.Now().UnixNano()%50) * time.Millisecond)

	// 2) calls.getCallPreview
	previewData := fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&fields=photo_200&access_token=%s", link, anonToken)
	_, _ = doReq(previewData, fmt.Sprintf("https://api.vk.ru/method/calls.getCallPreview?v=5.275&client_id=%s", vkClientID))

	time.Sleep(time.Duration(200+time.Now().UnixNano()%200) * time.Millisecond)

	// 3) calls.getAnonymousToken
	name := nameGenerator()
	anonData := fmt.Sprintf("vk_join_link=https://vk.com/call/join/%s&name=%s&access_token=%s", link, url.QueryEscape(name), anonToken)
	resp, err := doReq(anonData, fmt.Sprintf("https://api.vk.ru/method/calls.getAnonymousToken?v=5.275&client_id=%s", vkClientID))
	if err != nil {
		return "", "", nil, fmt.Errorf("getAnonymousToken: %w", err)
	}
	if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
		code, _ := errObj["error_code"].(float64)
		msg, _ := errObj["error_msg"].(string)
		return "", "", nil, fmt.Errorf("VK API error %.0f: %s", code, msg)
	}
	respMap, ok := resp["response"].(map[string]interface{})
	if !ok {
		return "", "", nil, fmt.Errorf("unexpected anon response: %v", resp)
	}
	vkToken, ok := respMap["token"].(string)
	if !ok || vkToken == "" {
		return "", "", nil, fmt.Errorf("missing token: %v", resp)
	}

	time.Sleep(time.Duration(100+time.Now().UnixNano()%50) * time.Millisecond)

	// 4) OK.ru auth.anonymLogin
	sessionData := fmt.Sprintf(`{"version":2,"device_id":"%s","client_version":1.1,"client_type":"SDK_JS"}`, uuid.New().String())
	okLoginData := fmt.Sprintf("session_data=%s&method=auth.anonymLogin&format=JSON&application_key=%s", url.QueryEscape(sessionData), okAppKey)
	resp, err = doReq(okLoginData, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, fmt.Errorf("ok.ru auth: %w", err)
	}
	sessionKey, ok := resp["session_key"].(string)
	if !ok || sessionKey == "" {
		return "", "", nil, fmt.Errorf("missing session_key: %v", resp)
	}

	time.Sleep(time.Duration(100+time.Now().UnixNano()%50) * time.Millisecond)

	// 5) OK.ru vchat.joinConversationByLink
	joinData := fmt.Sprintf("joinLink=%s&isVideo=false&protocolVersion=5&capabilities=2F7F&anonymToken=%s&method=vchat.joinConversationByLink&format=JSON&application_key=%s&session_key=%s",
		link, vkToken, okAppKey, sessionKey)
	resp, err = doReq(joinData, "https://calls.okcdn.ru/fb.do")
	if err != nil {
		return "", "", nil, fmt.Errorf("ok.ru join: %w", err)
	}
	tsRaw, ok := resp["turn_server"].(map[string]interface{})
	if !ok {
		body, _ := json.Marshal(resp)
		return "", "", nil, fmt.Errorf("no turn_server: %s", string(body))
	}
	user, _ = tsRaw["username"].(string)
	pass, _ = tsRaw["credential"].(string)
	if urlsRaw, ok := tsRaw["urls"].([]interface{}); ok {
		for _, u := range urlsRaw {
			if s, ok := u.(string); ok {
				urls = append(urls, s)
			}
		}
	}
	if user == "" || pass == "" || len(urls) == 0 {
		return "", "", nil, fmt.Errorf("incomplete turn creds: user=%q pass=%q urls=%d", user, pass, len(urls))
	}

	return user, pass, urls, nil
}

func (a *App) FetchVkTurnCredentials(token, hash, deviceID string) error {
	log.Printf("[VK Auth] Fetching TURN creds for hash %s...", shortHash(hash))
	user, pass, urls, err := fetchVkTurnCreds(token, hash, deviceID)
	if err != nil {
		runtime.EventsEmit(a.ctx, "log", "ERROR", fmt.Sprintf("[VK Auth] %v", err))
		return err
	}

	tcp := turnCredsPayload{User: user, Pass: pass, URLs: urls}
	raw, _ := json.Marshal(tcp)
	b64 := base64.StdEncoding.EncodeToString(raw)
	payload := fmt.Sprintf("%s|%s", hash, b64)

	runtime.EventsEmit(a.ctx, "vk_turn_creds", payload)
	runtime.EventsEmit(a.ctx, "log", "INFO", fmt.Sprintf("[VK Auth] TURN креды получены ✓ (urls=%d)", len(urls)))

	if a.orch != nil {
		a.orch.SendTurnCreds(payload)
	}
	return nil
}

func shortHash(h string) string {
	if len(h) <= 8 {
		return h
	}
	return h[:8]
}

func newVkHttpClient() (tlsclient.HttpClient, error) {
	jar := tlsclient.NewCookieJar()
	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), []tlsclient.HttpClientOption{
		tlsclient.WithCookieJar(jar),
		tlsclient.WithClientProfile(core.GetTLSProfile()),
		tlsclient.WithInsecureSkipVerify(),
	}...)
}

func vkApiPost(client tlsclient.HttpClient, data, apiURL string) (map[string]interface{}, error) {
	u, _ := url.Parse(apiURL)
	req, err := fhttp.NewRequest("POST", apiURL, bytes.NewBuffer([]byte(data)))
	if err != nil {
		return nil, err
	}
	req.Host = u.Hostname()
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", "https://vk.ru")
	req.Header.Set("Referer", "https://vk.ru/")

	httpResp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	body, readErr := io.ReadAll(httpResp.Body)
	if readErr != nil {
		return nil, readErr
	}
	var resp map[string]interface{}
	if jsonErr := json.Unmarshal(body, &resp); jsonErr != nil {
		return nil, fmt.Errorf("json decode: %w (body: %s)", jsonErr, string(body))
	}
	return resp, nil
}

// exchangeSilentToken exchanges a VK silent_token for a proper access_token.
// Tries client_secret as access_token; if that fails, returns "" so caller
// can fall back to anonymous flow.
func exchangeSilentToken(silentToken, uuidStr string) string {
	if uuidStr == "" {
		uuidStr = uuid.New().String()
		vkLogf("exchangeSilentToken: no uuid from payload, generated: %s", uuidStr)
	}

	client, err := newVkHttpClient()
	if err != nil {
		vkLogf("exchangeSilentToken: create client: %v", err)
		return ""
	}

	// Try client_secret as access_token (works for some VK API methods)
	vkLogf("exchangeSilentToken: exchanging with client_secret...")
	resp, err := vkApiPost(client,
		fmt.Sprintf(
			"v=5.199&token=%s&uuid=%s&access_token=%s",
			url.QueryEscape(silentToken),
			url.QueryEscape(uuidStr),
			vkClientSecret,
		),
		"https://api.vk.com/method/auth.exchangeSilentAuthToken",
	)
	if err != nil {
		vkLogf("exchangeSilentToken: exchange error: %v", err)
		return ""
	}

	if errObj, hasErr := resp["error"].(map[string]interface{}); hasErr {
		code, _ := errObj["error_code"].(float64)
		msg, _ := errObj["error_msg"].(string)
		vkLogf("exchangeSilentToken: VK API error %.0f: %s", code, msg)
		return ""
	}

	response, ok := resp["response"].(map[string]interface{})
	if !ok {
		vkLogf("exchangeSilentToken: unexpected response: %v", resp)
		return ""
	}
	accessToken, _ := response["access_token"].(string)
	if accessToken == "" {
		vkLogf("exchangeSilentToken: no access_token: %v", resp)
		return ""
	}
	vkLogf("exchangeSilentToken: success, token=%s...", accessToken[:min(len(accessToken), 20)])
	return accessToken
}
