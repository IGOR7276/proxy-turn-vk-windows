package core

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)


const (
	workersPerGroup = 9
)

// WorkerGroup:
// Запускает 9 потоков с одними кредами. Ротации нет — работает до смерти воркеров.
func WorkerGroup(
	ctx context.Context,
	groupID int,
	hashIndex int,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	workerIDs []int,
	pauseFlag *int32,
	deviceID, password string,
	stats *Stats,
	waitReady <-chan struct{},
	signalReady chan<- struct{},
) {
	// Каскадный запуск: ждем свою очередь
	if waitReady != nil {
		log.Printf("[ГРУППА #%d] Ожидание сигнала от предыдущей группы...", groupID)
		select {
		case <-waitReady:
		case <-ctx.Done():
			return
		}
	}

	var configSent int32
	if !getConfig {
		configSent = 1
	}

	// Doze-mode пауза
	for atomic.LoadInt32(pauseFlag) != 0 {
		if ctx.Err() != nil {
			return
		}
		time.Sleep(1 * time.Second)
	}

	hash := tp.Hashes[hashIndex%len(tp.Hashes)]
	shortHash := hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	log.Printf("[ГРУППА #%d] Запрос кредов (хеш: %s...)", groupID, shortHash)

	credStreamID := groupID * 100
	var creds *Credentials

	// Retry-цикл: при получении капчи VK требует подождать ~60 секунд перед
	// повторной попыткой. Без ретраев группа умирает на первой же капче и
	// теряет 9 воркеров. Подсмотрено в PWDTT (client/core/group.go).
	for {
		if ctx.Err() != nil {
			return
		}
		credsCtx, credsCancel := context.WithTimeout(context.Background(), 120*time.Second)
		go func() {
			select {
			case <-ctx.Done():
				credsCancel()
			case <-credsCtx.Done():
			}
		}()
		user, pass, turnURLs, err := GetCreds(credsCtx, hash, credStreamID)
		credsCancel()
		if err == nil {
			creds = &Credentials{User: user, Pass: pass, TurnURLs: turnURLs, CacheStreamID: credStreamID}
			break
		}
		log.Printf("[ГРУППА #%d] Ошибка кредов: %v", groupID, err)
		if strings.Contains(err.Error(), "FATAL_AUTH") || strings.Contains(err.Error(), "context canceled") {
			return
		}
		wait := 15 * time.Second
		if strings.Contains(err.Error(), "CAPTCHA_WAIT_REQUIRED") {
			wait = 65 * time.Second
		}
		log.Printf("[ГРУППА #%d] Повторная попытка через %v...", groupID, wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return
		}
	}

	// Регистрируем TURN IP-адреса для исключения из WG-маршрутизации
	registerTurnExcludes(creds.TurnURLs)

	log.Printf("[ГРУППА #%d] Креды OK, TURN: %v, %d воркеров", groupID, creds.TurnURLs, len(workerIDs))
			emitEvent(Event{Type: EventEvent, Name: "vk_creds_ok", Data: fmt.Sprintf("group=%d urls=%d", groupID, len(creds.TurnURLs))})

	var configRequestInFlight int32
	var wg sync.WaitGroup
	var credsMu sync.RWMutex
	var refreshMu sync.Mutex
	var lastCredRefresh atomic.Int64

	refreshCreds := func(reason string) bool {
		refreshMu.Lock()
		defer refreshMu.Unlock()

		now := time.Now().Unix()
		last := lastCredRefresh.Load()
		if last > 0 && now-last < 15 {
			log.Printf("[TURN] Креды уже обновлялись %d сек назад, ждём следующий retry (%s)", now-last, reason)
			return true
		}

		getStreamCache(credStreamID).invalidate(credStreamID)
		if GetVkAuthMode() == "account" {
			InvalidateInjectedTurnCreds(hash)
		}
		// Hard timeout 35s — иначе зависший auth endpoint блокирует ретраи навсегда
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer refreshCancel()
		u, p, urls, refreshErr := GetCreds(refreshCtx, hash, credStreamID)
		if refreshErr != nil {
			log.Printf("[TURN] Не удалось обновить креды после %s: %v", reason, refreshErr)
			return false
		}

		credsMu.Lock()
		creds = &Credentials{User: u, Pass: p, TurnURLs: urls, CacheStreamID: credStreamID}
		credsMu.Unlock()
		lastCredRefresh.Store(time.Now().Unix())
		// Новые креды могут прийти с другими TURN-серверами — их тоже нужно
		// вывести из-под WG-маршрута, иначе трафик воркера зациклится в туннеле.
		registerTurnExcludes(urls)
		log.Printf("[TURN] Креды обновлены после %s, TURN urls=%d", reason, len(urls))
		return true
	}

	// Сигнализируем следующей группе, что мы успешно запустились (креды получены + 2 сек форы)
	if signalReady != nil {
		go func() {
			select {
			case <-time.After(2000 * time.Millisecond):
				if ctx.Err() == nil {
					close(signalReady)
					log.Printf("[ГРУППА #%d] Успешный старт! Передача эстафеты следующей группе...", groupID)
				}
			case <-ctx.Done():
			}
		}()
	}

	for i, wid := range workerIDs {
		wg.Add(1)

		// Stagger: 500мс между воркерами
		workerDelay := time.Duration(i) * 500 * time.Millisecond

		go func(wid int, delay time.Duration) {
			defer wg.Done()

			if delay > 0 {
				select {
				case <-time.After(delay):
				case <-ctx.Done():
					return
				}
			}

			shouldGetConfig := getConfig
			attempt := 0

			for {
				if ctx.Err() != nil {
					return
				}

				getConf := false
				if shouldGetConfig && atomic.LoadInt32(&configSent) == 0 {
					getConf = atomic.CompareAndSwapInt32(&configRequestInFlight, 0, 1)
				}
				var cc chan<- string
				if getConf {
					cc = configCh
				}

				credsMu.RLock()
				credsSnapshot := *creds
				credsSnapshot.TurnURLs = cloneStringSlice(creds.TurnURLs)
				credsMu.RUnlock()

				// Результат GETCONF приходит колбэком сразу после попытки, а не
				// после смерти сессии — иначе неудачный запрос блокировал бы
				// конфиг на всё время жизни сессии.
				var onConfigAttempt func(bool)
				if getConf {
					onConfigAttempt = func(delivered bool) {
						if delivered {
							atomic.StoreInt32(&configSent, 1)
						} else {
							atomic.StoreInt32(&configRequestInFlight, 0)
						}
					}
				}

				sessStart := time.Now()
				_, sessErr := RunSession(ctx, tp, peer, d, localPort,
					getConf, cc, wid, &credsSnapshot, deviceID, password, stats, onConfigAttempt)

				// Сессия прожила достаточно долго → это был не «неудачный
				// коннект», а нормальная работа с последующим обрывом.
				// Сбрасываем backoff, чтобы переподключение было быстрым.
				if time.Since(sessStart) > 60*time.Second {
					attempt = 0
				}

				if sessErr != nil {
					if ctx.Err() != nil {
						return
					}
					errStr := sessErr.Error()
					errStrLower := strings.ToLower(errStr)

					turnAllocAttrMissing := strings.Contains(errStrLower, "turn allocate") &&
						strings.Contains(errStrLower, "attribute not found")
					turnCredRefreshNeeded := turnAllocAttrMissing ||
						strings.Contains(errStrLower, "turn allocate auth") ||
						strings.Contains(errStrLower, "invalid credential") ||
						strings.Contains(errStrLower, "stale nonce") ||
						strings.Contains(errStrLower, "allocation mismatch") ||
						strings.Contains(errStrLower, "error 508") ||
						strings.Contains(errStrLower, "turn квота") ||
						strings.Contains(errStrLower, "quota")

					if strings.Contains(errStrLower, "rate limit") ||
						strings.Contains(errStrLower, "flood control") ||
						strings.Contains(errStrLower, "ip mismatch") ||
						strings.Contains(errStrLower, "error 29") {
						errStr += " (ошибка со стороны ВК)"
					}

					if strings.Contains(errStrLower, "хеш мёртв") ||
						strings.Contains(errStrLower, "fatal_auth") {
						log.Printf("[ВОРКЕР #%d] Фатальная ошибка: %s", wid, errStr)
						return
					}

					attempt++
					if turnAllocAttrMissing {
						log.Printf("[ВОРКЕР #%d] [TURN] Allocate вернул неполный ответ, обновляем TURN-креды и повторяем (попытка %d): %s", wid, attempt, errStr)
						refreshCreds("TURN Allocate attribute-not-found")
					} else if turnCredRefreshNeeded {
						log.Printf("[ВОРКЕР #%d] [TURN] Ошибка allocation/кредов, обновляем TURN-креды и повторяем (попытка %d): %s", wid, attempt, errStr)
						refreshCreds("TURN allocation error")
					} else {
						log.Printf("[ВОРКЕР #%d] Ошибка (попытка %d): %s", wid, attempt, errStr)
					}

					// Раньше «error 29» / «cannot create socket» считались
					// невосстановимыми и воркер выходил навсегда. На практике
					// это временные состояния (сеть ещё не поднялась после сна,
					// VK-флуд-контроль), после которых воркер должен вернуться.
					// Оставляем воркер в retry-цикле с обычным backoff.
				}

				if ctx.Err() != nil {
					return
				}

				// Exponential backoff с jitter — предотвращает thundering herd,
				// когда у VK hiccup и все 9 воркеров ретраят одновременно.
				// 0: 1-3s (сессия умерла после долгой работы — переподключаемся
				// быстро), 1: 2-5s, 2: 4-7s, 3: 8-11s, 4: 16-19s, 5+: 30-33s.
				base := 1
				if attempt > 0 {
					base = 2 << uint(attempt-1)
				}
				if base > 30 {
					base = 30
				}
				retryDelay := time.Duration(base)*time.Second + time.Duration(rand.Intn(3))*time.Second
				select {
				case <-time.After(retryDelay):
				case <-ctx.Done():
					return
				}
			}
		}(wid, workerDelay)
	}

	wg.Wait()
	log.Printf("[ГРУППА #%d] Все воркеры группы завершились.", groupID)
}

// turnURLHost извлекает хост из TURN URL вида "turn:1.2.3.4:443",
// "turns:host:5349?transport=udp", "turn://…". Возвращает "" если не разобрать.
func turnURLHost(u string) string {
	u = strings.TrimSpace(u)
	for _, prefix := range []string{"turns://", "turn://", "turns:", "turn:"} {
		if strings.HasPrefix(strings.ToLower(u), prefix) {
			u = u[len(prefix):]
			break
		}
	}
	if idx := strings.IndexAny(u, "?#"); idx >= 0 {
		u = u[:idx]
	}
	if host, _, err := net.SplitHostPort(u); err == nil {
		return host
	}
	return u
}

// registerTurnExcludes выводит хосты TURN-серверов из-под WG-маршрута.
// Старая версия делала TrimPrefix("turn://") на URL вида "turn:IP:PORT" и
// SplitHostPort всегда падал с «too many colons» — исключения не добавлялись
// вообще, спасали только широкие VK CIDR.
func registerTurnExcludes(urls []string) {
	for _, u := range urls {
		if host := turnURLHost(u); host != "" {
			AddTurnExcludeIP(host)
		}
	}
}

// ParseHashes — парсит строку хешей
func ParseHashes(raw string) []string {
	var result []string
	seen := make(map[string]struct{})
	for _, h := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	}) {
		h = normalizeVKJoinHash(h)
		if h != "" {
			if _, exists := seen[h]; exists {
				continue
			}
			seen[h] = struct{}{}
			result = append(result, h)
		}
	}
	return result
}

func normalizeVKJoinHash(input string) string {
	s := strings.Trim(strings.TrimSpace(input), "<>\"'")
	if s == "" {
		return ""
	}

	lower := strings.ToLower(s)
	if idx := strings.Index(lower, "/call/join/"); idx >= 0 {
		s = s[idx+len("/call/join/"):]
	} else if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ""
	}

	if idx := strings.IndexAny(s, "?#/"); idx != -1 {
		s = s[:idx]
	}
	s = strings.Trim(strings.TrimSpace(s), "/")

	if s == "" {
		return ""
	}

	if lower2 := strings.ToLower(s); strings.HasPrefix(lower2, "vk.com/") ||
		strings.HasPrefix(lower2, "vk.ru/") ||
		strings.HasPrefix(lower2, "m.vk.com/") ||
		strings.HasPrefix(lower2, "m.vk.ru/") {
		if idx := strings.Index(s, "/"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	return s
}

// TurnParams — конфигурация TURN
type TurnParams struct {
	Host     string
	Port     string
	Hashes   []string
	WrapKey  []byte // Password-derived WRAP key (32 bytes), nil = disabled
	ObfsMode string // "audio" or "video" — RTP masking mode
}

// Credentials — учетные данные TURN
type Credentials struct {
	User          string
	Pass          string
	TurnURLs      []string
	CacheStreamID int
}



