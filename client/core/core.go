package core

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Config — все параметры запуска (соответствует старым флагам main()).
type Config struct {
	PeerAddr     string   // -peer
	Password     string   // -password (WRAP key derives from this)
	Hashes       []string // -vk (уже распарсенные ParseHashes)
	Listen       string   // -listen, default "127.0.0.1:9000"
	TurnHost     string   // -turn
	TurnPort     string   // -port
	DeviceID     string   // -device-id
	Workers      int      // -n
	CaptchaMode  string   // -captcha-mode: auto/wv/rjs
	Fingerprint  string   // -fingerprint: chrome/safari/...
	ClientIDs    string   // -client-ids
	WGInterface  string   // -wg-interface, default "WDTT"
	AutoWG       bool     // -windows-wg
	DNSUpstream  []string // -dns (если пусто — дефолт 8.8.8.8,1.1.1.1)
	NoDNSProxy   bool     // -no-dns-proxy
	WGConfigMTU  int      // MTU для патча конфига (0 = default 1280)
}

// EventType — тип события от ядра.
type EventType string

const (
	EventState EventType = "state" // состояние (connecting/connected/disconnected/error)
	EventLog   EventType = "log"   // лог-сообщение
	EventEvent EventType = "event" // структурное событие (captcha_required, wg_config)
	EventError EventType = "error" // ошибка
)

// Event — событие от ядра к orchestrator/backend.
type Event struct {
	Type   EventType
	Status string // для state
	Level  string // для log: info/warn/error
	Msg    string // для log
	Name   string // для event
	Data   string // для event
}

// Core — runtime controller ядра. Потокобезопасен, можно Start/Stop несколько раз
// (повторный Start после Stop создаёт новое ядро — нужна пересоздать через New).
type Core struct {
	cfg  Config
	ctx  context.Context
	cancel context.CancelFunc

	pauseFlag int32
	events    chan Event
	stats     *Stats

	turnIPsMu sync.Mutex
	turnIPs   []string

	// Кэш последнего WG-конфига: если тот же профиль перезапускается
	// в течение wgConfigCacheTTL, конфиг берётся из памяти, минуя DTLS exchange.
	wgCacheMu       sync.Mutex
	wgCacheConf     string
	wgCacheAt       time.Time
	wgCacheKey      string

	once sync.Once
}

const wgConfigCacheTTL = 60 * time.Second

// wgCacheKeyOf — уникальный ключ профиля для кэша.
func (c *Core) wgCacheKeyOf() string {
	return c.cfg.PeerAddr + "|" + c.cfg.Password
}

// wgCacheGet — отдаёт кэшированный конфиг, если он свежий для текущего ключа.
func (c *Core) wgCacheGet() (string, bool) {
	c.wgCacheMu.Lock()
	defer c.wgCacheMu.Unlock()
	if c.wgCacheConf == "" || c.wgCacheKey != c.wgCacheKeyOf() {
		return "", false
	}
	if time.Since(c.wgCacheAt) > wgConfigCacheTTL {
		return "", false
	}
	return c.wgCacheConf, true
}

// wgCachePut — сохраняет конфиг в кэш.
func (c *Core) wgCachePut(conf string) {
	c.wgCacheMu.Lock()
	defer c.wgCacheMu.Unlock()
	c.wgCacheConf = conf
	c.wgCacheAt = time.Now()
	c.wgCacheKey = c.wgCacheKeyOf()
}

// New создаёт Core. После Start() можно дёргать Pause/Resume/SolveCaptcha.
func New(cfg Config) *Core {
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:9000"
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = "unknown"
	}
	if cfg.WGInterface == "" {
		cfg.WGInterface = "WDTT"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 24
	}
	c := &Core{
		cfg:    cfg,
		events: make(chan Event, 256),
	}
	setCaptchaMode(cfg.CaptchaMode)
	if cfg.Fingerprint != "" {
		SetActiveFingerprint(cfg.Fingerprint)
	}
	if cfg.ClientIDs != "" {
		SetActiveClientIds(cfg.ClientIDs)
	}
	return c
}

// Start запускает ядро. Возвращает канал событий (закрывается при завершении).
// ctx используется как родительский — если он отменится, ядро тоже остановится.
func (c *Core) Start(ctx context.Context) (<-chan Event, error) {
	if c.cfg.PeerAddr == "" {
		return nil, fmt.Errorf("PeerAddr is required")
	}
	if len(c.cfg.Hashes) == 0 {
		return nil, fmt.Errorf("Hashes are required")
	}
	if c.cfg.Password == "" {
		return nil, fmt.Errorf("Password is required")
	}

	setupGlobalResolver()

	c.ctx, c.cancel = context.WithCancel(ctx)
	ctx = c.ctx

	peer, err := net.ResolveUDPAddr("udp", c.cfg.PeerAddr)
	if err != nil {
		c.cancel()
		return nil, fmt.Errorf("resolve peer: %w", err)
	}

	wrapKey, err := deriveWrapKey(c.cfg.Password)
	if err != nil {
		c.cancel()
		return nil, fmt.Errorf("derive wrap key: %w", err)
	}

	maxWorkers := 108
	n := c.cfg.Workers
	if n > maxWorkers {
		n = maxWorkers
	}
	if n < workersPerGroup {
		n = workersPerGroup
	}
	// Округляем ВВЕРХ до ближайшего кратного workersPerGroup (9).
	// Раньше было (n/9)*9 (вниз) — из-за чего 16 превращалось в 9.
	// 10..18 → 18, 19..27 → 27 и т.д.
	if n%workersPerGroup != 0 {
		n = ((n / workersPerGroup) + 1) * workersPerGroup
	}

	tp := &TurnParams{
		Host:    c.cfg.TurnHost,
		Port:    c.cfg.TurnPort,
		Hashes:  c.cfg.Hashes,
		WrapKey: wrapKey,
	}

	// Слушаем с retry (5 попыток по 1с) на случай если старый процесс
	// ещё не отпустил порт. Без этого периодически падаем на старте.
	var localConn net.PacketConn
	actualListenAddr := c.cfg.Listen
	for i := 0; i < 5; i++ {
		localConn, err = net.ListenPacket("udp", actualListenAddr)
		if err == nil {
			break
		}
		log.Printf("[CORE] Порт %s занят, жду... (%d/5)", actualListenAddr, i+1)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		// Fallback: динамический порт
		actualListenAddr = "127.0.0.1:0"
		localConn, err = net.ListenPacket("udp", actualListenAddr)
		if err != nil {
			c.cancel()
			return nil, fmt.Errorf("listen fallback: %w", err)
		}
	}
	if uc, ok := localConn.(*net.UDPConn); ok {
		_ = uc.SetReadBuffer(socketBufSize)
		_ = uc.SetWriteBuffer(socketBufSize)
	}
	stopLocalConn := context.AfterFunc(ctx, func() { _ = localConn.Close() })
	defer stopLocalConn()

	_, localPort, _ := net.SplitHostPort(localConn.LocalAddr().String())
	if localPort == "" {
		localPort = "9000"
	}
	if actualListenAddr != c.cfg.Listen {
		log.Printf("[CORE] Fallback на динамический порт: %s (запрошено %s)", localConn.LocalAddr().String(), c.cfg.Listen)
	}

	numGroups := n / workersPerGroup

	wrapStatus := "OFF"
	if len(wrapKey) == wrapKeyLen {
		wrapStatus = "ON (password HKDF + RTP AEAD)"
	}
	activeMode := getCaptchaMode()
	captchaStatus := "AUTO: Go v2 x2 -> WBV Auto x2 -> Go v2 x1 -> Manual WBV"
	switch activeMode {
	case "wv":
		captchaStatus = "WBV selected in Android"
	case "rjs":
		captchaStatus = "RJS Go v2 with WBV Auto fallback"
	}

	log.Println("[CORE] ═══════════════════════════════════════")
	log.Printf("[CORE] VK Creds: %s", GetActiveClientIdsString())
	log.Printf("[CORE] TLS: %s fingerprint", GetActiveFingerprint())
	log.Printf("[CORE] Воркеров: %d (групп: %d, по %d)", n, numGroups, workersPerGroup)
	log.Printf("[CORE] Хешей: %d", len(c.cfg.Hashes))
	log.Printf("[CORE] Слушаю: %s | Пир: %s", c.cfg.Listen, c.cfg.PeerAddr)
	log.Printf("[CORE] Протокол: UDP")
	log.Printf("[CORE] WRAP: %s", wrapStatus)
	log.Printf("[CORE] Device ID: %s", c.cfg.DeviceID)
	log.Printf("[CORE] Captcha: %s", captchaStatus)
	if c.cfg.AutoWG {
		log.Printf("[CORE] Windows WG: ON (iface=%s)", c.cfg.WGInterface)
		if !c.cfg.NoDNSProxy {
			dns := c.cfg.DNSUpstream
			if len(dns) == 0 {
				dns = []string{"8.8.8.8", "1.1.1.1"}
			}
			log.Printf("[CORE] DNS-прокси: ON → %v", dns)
		} else {
			log.Printf("[CORE] DNS-прокси: OFF (системный DNS)")
		}
	}
	log.Println("[CORE] ═══════════════════════════════════════")

	c.emit(Event{Type: EventState, Status: "starting"})

	stats := NewStats()
	c.stats = stats

	shutdownCh := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(shutdownCh)
	}()
	go stats.RunLoop(shutdownCh)

	disp := NewDispatcher(ctx, localConn, stats)

	configCh := make(chan string, 1)
	configDone := make(chan struct{})

	customDNS := c.cfg.DNSUpstream
	if !c.cfg.NoDNSProxy && len(customDNS) == 0 {
		customDNS = []string{"8.8.8.8", "1.1.1.1"}
	}

	go func() {
		defer close(configDone)
		defer TeardownWindowsWireGuard()
		applyConfig := func(rawConf string, fromCache bool) {
			finalConf := patchWGConfig(rawConf)
			if c.cfg.WGConfigMTU > 0 {
				finalConf = injectMTU(finalConf, c.cfg.WGConfigMTU)
			}
			log.Println("╔══════════════ WireGuard Конфиг ══════════════╗")
			for _, line := range strings.Split(finalConf, "\n") {
				log.Printf("║ %-44s ║", line)
			}
			log.Println("╚══════════════════════════════════════════════╝")
			c.emit(Event{Type: EventEvent, Name: "wg_config", Data: finalConf})

			if c.cfg.AutoWG {
				if err := SetupWindowsWireGuard(finalConf, c.cfg.WGInterface, customDNS); err != nil {
					log.Printf("[CORE] WG setup error: %v", err)
					c.emit(Event{Type: EventError, Msg: err.Error()})
					return
				}
				if c.cfg.NoDNSProxy {
					log.Printf("[CORE] WG %s поднят (DNS-прокси OFF)", c.cfg.WGInterface)
				} else {
					log.Printf("[CORE] WG %s поднят (DNS-прокси ON → %v)", c.cfg.WGInterface, customDNS)
				}
				c.emit(Event{Type: EventState, Status: "connected"})
			}
		}

		// Сначала пробуем кэш — если конфиг свежий, туннель поднимается мгновенно.
		if cached, ok := c.wgCacheGet(); ok {
			log.Printf("[CORE] WG конфиг из кэша (%.0fs назад) — пропускаем DTLS exchange", time.Since(c.wgCacheAt).Seconds())
			applyConfig(cached, true)
		} else {
			select {
			case rawConf, ok := <-configCh:
				if !ok || rawConf == "" {
					return
				}
				c.wgCachePut(rawConf)
				applyConfig(rawConf, false)
			case <-ctx.Done():
				return
			}
		}

		// Ждём отмены контекста перед teardown. Без этого WG-интерфейс
		// сносился бы сразу после поднятия, потому что defer выше исполняется
		// при выходе из горутины, а без этого блока горутина выходит
		// сразу после SetupWindowsWireGuard.
		<-ctx.Done()
	}()

	var wg sync.WaitGroup
	workerIDCounter := 1
	var prevWaitReady <-chan struct{}

	for g := 0; g < numGroups; g++ {
		isFirst := g == 0
		var myWaitReady <-chan struct{}
		var mySignalReady chan<- struct{}

		if g > 0 {
			myWaitReady = prevWaitReady
		}
		if g < numGroups-1 {
			ch := make(chan struct{})
			mySignalReady = ch
			prevWaitReady = ch
		}

		ids := make([]int, workersPerGroup)
		for i := range ids {
			ids[i] = workerIDCounter
			workerIDCounter++
		}

		gID := g + 1
		var cc chan<- string
		if isFirst {
			cc = configCh
		}

		wg.Add(1)
		go func(groupID int, isFirstGroup bool, configChan chan<- string, workerIds []int, startHashIndex int, waitR <-chan struct{}, sigR chan<- struct{}) {
			defer wg.Done()
			WorkerGroup(ctx, groupID, startHashIndex, tp, peer, disp, localPort,
				isFirstGroup, configChan, workerIds, &c.pauseFlag,
				c.cfg.DeviceID, c.cfg.Password, stats, waitR, sigR)
		}(gID, isFirst, cc, ids, g, myWaitReady, mySignalReady)
	}

	go func() {
		defer close(c.events)
		defer c.cancel()
		defer func() { _ = localConn.Close() }()
		defer disp.Shutdown()

		wg.Wait()
		close(configCh)
		<-configDone
		c.emit(Event{Type: EventState, Status: "stopped"})
		log.Println("[CORE] все воркеры завершены")
	}()

	return c.events, nil
}

// Stop останавливает ядро.
func (c *Core) Stop() {
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
}

// Pause приостанавливает воркеров (doze-mode).
func (c *Core) Pause() { atomic.StoreInt32(&c.pauseFlag, 1) }

// Resume возобновляет воркеров.
func (c *Core) Resume() { atomic.StoreInt32(&c.pauseFlag, 0) }

// SolveCaptcha передаёт токен капчи в ядро (через общий CaptchaResultChan).
func (c *Core) SolveCaptcha(token string) {
	drainCaptchaResult()
	CaptchaResultChan <- token
}

// AddTurnIPs регистрирует TURN IP-адреса для исключения из туннеля
// (чтобы трафик к ним не зацикливался через WG).
func (c *Core) AddTurnIPs(urls []string) {
	c.turnIPsMu.Lock()
	defer c.turnIPsMu.Unlock()
	seen := make(map[string]struct{}, len(c.turnIPs))
	for _, ip := range c.turnIPs {
		seen[ip] = struct{}{}
	}
	for _, u := range urls {
		host, _, _ := net.SplitHostPort(strings.TrimPrefix(u, "turn:"))
		if host == "" {
			host = u
		}
		if host == "" {
			continue
		}
		if _, ok := seen[host]; !ok {
			seen[host] = struct{}{}
			c.turnIPs = append(c.turnIPs, host)
			AddTurnExcludeIP(host)
		}
	}
}

// GetTurnIPs возвращает все зарегистрированные TURN IP.
func (c *Core) GetTurnIPs() []string {
	c.turnIPsMu.Lock()
	defer c.turnIPsMu.Unlock()
	out := make([]string, len(c.turnIPs))
	copy(out, c.turnIPs)
	return out
}

// Snapshot — снимок текущей статистики для UI.
type Snapshot struct {
	ActiveConnections int32
	TotalBytesUp      int64
	TotalBytesDown    int64
}

// Stats возвращает текущий снимок (для UI). Безопасно для вызова до Start()
// (вернёт нули), но Start() уже должен был запустить stats.RunLoop.
func (c *Core) Stats() Snapshot {
	if c.stats == nil {
		return Snapshot{}
	}
	return Snapshot{
		ActiveConnections: atomic.LoadInt32(&c.stats.ActiveConnections),
		TotalBytesUp:      atomic.LoadInt64(&c.stats.TotalBytesUp),
		TotalBytesDown:    atomic.LoadInt64(&c.stats.TotalBytesDown),
	}
}

func (c *Core) emit(ev Event) {
	select {
	case c.events <- ev:
	default:
		if ev.Type != EventLog {
			// Drain one stale entry to make room for important event
			select {
			case <-c.events:
			default:
			}
			select {
			case c.events <- ev:
			default:
			}
		}
	}
}

// injectMTU — вписывает/заменяет MTU в WG-конфиге (если его там нет).
func injectMTU(conf string, mtu int) string {
	lines := strings.Split(conf, "\n")
	hasMTU := false
	for i, l := range lines {
		tl := strings.TrimSpace(l)
		if strings.HasPrefix(strings.ToLower(tl), "mtu =") || strings.HasPrefix(strings.ToLower(tl), "mtu=") {
			lines[i] = fmt.Sprintf("MTU = %d", mtu)
			hasMTU = true
		}
	}
	if !hasMTU {
		lines = append(lines, fmt.Sprintf("MTU = %d", mtu))
	}
	return strings.Join(lines, "\n")
}
