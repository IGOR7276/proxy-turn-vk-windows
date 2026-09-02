package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"wg-turn-client/core"
)

// wailsLogWriter перехватывает log.Printf и направляет в Wails-события.
// Параллельно пишет полный лог в файл <config>/wdtt/logs/<session>.log.
// Буферизует UI-записи и флашит каждые 100ms чтобы не блокировать core.
type wailsLogWriter struct {
	ctx  context.Context
	mu   sync.Mutex
	buf  []logEntry
	stop chan struct{}
	file *os.File
}

type logEntry struct{ level, msg string }

const maxLogBuf = 500

func newSessionLogFile(peerIP string) *os.File {
	dir := filepath.Join(configDir(), "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil
	}
	ts := time.Now().Format("2006-01-02_15-04-05")
	name := ts + "_" + peerIP + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil
	}
	return f
}

func (w *wailsLogWriter) start() {
	w.stop = make(chan struct{})
	go func() {
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				w.flush()
			case <-w.stop:
				w.flush()
				return
			}
		}
	}()
}

func (w *wailsLogWriter) flush() {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	batch := w.buf
	w.buf = nil
	w.mu.Unlock()
	for _, e := range batch {
		runtime.EventsEmit(w.ctx, "log", e.level, e.msg)
	}
}

func (w *wailsLogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	// Обрезаем timestamp "2026/06/06 18:59:27.123456" из log.SetFlags
	if len(msg) > 20 && msg[4] == '/' {
		msg = strings.TrimSpace(msg[20:])
	}
	level := classifyLevel(msg)

	// Пишем в файл сразу (без буфера)
	if w.file != nil {
		ts := time.Now().Format("15:04:05")
		fmt.Fprintf(w.file, "[%s] [%s] %s\n", ts, level, msg)
	}

	w.mu.Lock()
	if len(w.buf) >= maxLogBuf {
		w.buf = w.buf[1:]
	}
	w.buf = append(w.buf, logEntry{level, msg})
	w.mu.Unlock()
	return len(p), nil
}

func classifyLevel(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "fatal_auth") ||
		strings.Contains(low, "ошибка") ||
		strings.Contains(low, "error") ||
		strings.Contains(low, "fatal") ||
		strings.Contains(low, "фатальн"):
		return "ERROR"
	case strings.Contains(low, "warn") ||
		strings.Contains(low, "не удалось") ||
		strings.Contains(low, "повторим") ||
		strings.Contains(low, "повторяем") ||
		strings.Contains(low, "retry"):
		return "WARN"
	case strings.Contains(low, "debug") ||
		strings.Contains(low, "obfs") ||
		strings.Contains(low, "unwrap") ||
		strings.Contains(low, "wrap:"):
		return "DEBUG"
	default:
		return "INFO"
	}
}

func configDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.Getenv("HOME")
	}
	dir := filepath.Join(base, "wdtt")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func profilePath(name string) string {
	return filepath.Join(configDir(), "profiles", name+".json")
}

// ProfileData — хранится в <config>/wdtt/profiles/<name>.json
type ProfileData struct {
	PeerAddr    string   `json:"peer"`
	Password    string   `json:"password"`
	Hashes      []string `json:"hashes"`
	Listen      string   `json:"listen,omitempty"`
	TurnHost    string   `json:"turn,omitempty"`
	TurnPort    string   `json:"port,omitempty"`
	DeviceID    string   `json:"device_id,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	ClientIDs   string   `json:"client_ids,omitempty"`
	ObfsMode    string   `json:"obfsMode,omitempty"`
}

// ConnectParams — runtime параметры от UI.
type ConnectParams struct {
	Profile     string   `json:"profile"`
	CaptchaMode string   `json:"captchaMode"`
	VkAuthMode  string   `json:"vkAuthMode,omitempty"`
	Workers     int      `json:"workers,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	MTU         int      `json:"mtu,omitempty"`
	Hashes      []string `json:"hashes,omitempty"`
	ObfsMode    string   `json:"obfsMode,omitempty"`

	// Runtime profile data (used for subscription-sourced profiles that have no file in profiles/).
	PeerAddr string   `json:"peer,omitempty"`
	Password string   `json:"password,omitempty"`
	Listen   string   `json:"listen,omitempty"`
	TurnHost string   `json:"turn,omitempty"`
	TurnPort string   `json:"port,omitempty"`
	DeviceID string   `json:"device_id,omitempty"`
	PHashes  []string `json:"profileHashes,omitempty"`

	// Флаги окружения (наш уникальный функционал)
	AutoWG      bool     `json:"autoWG,omitempty"`
	DNSUpstream []string `json:"dnsUpstream,omitempty"`
	NoDNSProxy  bool     `json:"noDNSProxy,omitempty"`
	WGInterface string   `json:"wgInterface,omitempty"`

	// ExcludeDomains — паттерны доменов для исключения из туннеля (wildcards поддерживаются).
	ExcludeDomains []string `json:"excludeDomains,omitempty"`

	// AutoReconnect — если ядро завершилось само (не по кнопке «Отключить»
	// и не из-за фатальной ошибки авторизации), поднять сессию заново с
	// экспоненциальной задержкой. Фронт передаёт значение из настроек.
	AutoReconnect bool `json:"autoReconnect,omitempty"`
}

func loadProfile(name string) (*ProfileData, error) {
	data, err := os.ReadFile(profilePath(name))
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	var p ProfileData
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("profile %q parse: %w", name, err)
	}
	return &p, nil
}

// coreSession — обёртка над запущенным core.
type coreSession struct {
	c      *core.Core
	doneCh <-chan core.Event // закрывается когда core завершился
	done   chan struct{}    // закрывается когда forwardEvents полностью вышел (включая WG-teardown)

	params      ConnectParams // для автопереподключения
	isReconnect bool          // сессия поднята автопереподключением
	userStop    atomic.Bool   // остановлена пользователем (Disconnect/выход)
	fatal       atomic.Bool   // фатальная ошибка авторизации — не переподключаемся
	wasUp       atomic.Bool   // туннель хотя бы раз дошёл до wg_up
	upAt        atomic.Int64  // unix-время wg_up (для сброса backoff)
}

// connectionStep — этапы подключения, отслеживаемые pipeline.
type connectionStep string

const (
	stepStart    connectionStep = "start"
	stepDNS      connectionStep = "dns"
	stepVK       connectionStep = "vk"
	stepCaptcha  connectionStep = "captcha"
	stepWrap     connectionStep = "wrap"
	stepTurn     connectionStep = "turn"
	stepDTLS     connectionStep = "dtls"
	stepWorkers  connectionStep = "workers"
	stepWG       connectionStep = "wg"
	stepDone     connectionStep = "done"
	stepFailed   connectionStep = "failed"
)

// pipelineState — текущее состояние схемы подключения.
type pipelineState struct {
	Visible    bool           `json:"visible"`
	Current    connectionStep `json:"current"`
	Completed  []string       `json:"completed"`
	Failed     *string        `json:"failed,omitempty"`
	TimedOut   bool           `json:"timedOut"`
	TimeoutSec int            `json:"timeoutSec"`
}

// pipelineController следит за этапами подключения и останавливает туннель при ошибках/таймаутах.
//
// Важно: контроллер описывает ТОЛЬКО начальное подключение. После finish()
// (wg_up) он становится инертным. Раньше любое событие dtls_error /
// wrap_auth_timeout от любого воркера — в том числе при обычном
// переподключении воркера после обрыва сети — вызывало markFailed → Stop()
// и сносило работающий туннель целиком. Плюс каждый turn_allocated
// повторно взводил 12-секундный таймер этапа DTLS.
type pipelineController struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	state    pipelineState
	sess     *coreSession
	stopFunc func()
	timer    *time.Timer

	done         bool // wg_up получен — этапы больше не отслеживаем
	dtlsOK       bool // хотя бы один воркер прошёл DTLS
	dtlsFailures int  // провалов DTLS до первого успеха
}

// dtlsFailuresBeforeFatal — сколько провалов DTLS-хендшейка (до первого
// успешного) считаем признаком неверного пароля/недоступного сервера.
// Один провал может быть транзиентным (потеря UDP), два подряд при
// одновременных попытках нескольких воркеров — уже вряд ли.
const dtlsFailuresBeforeFatal = 2

func newPipelineController(stopFunc func()) *pipelineController {
	ctx, cancel := context.WithCancel(context.Background())
	return &pipelineController{
		ctx:      ctx,
		cancel:   cancel,
		stopFunc: stopFunc,
		state: pipelineState{
			Visible:   true,
			Current:   stepDNS,
			Completed: []string{},
		},
	}
}

func (pc *pipelineController) emitState(appCtx context.Context) {
	runtime.EventsEmit(appCtx, "pipeline_state", pc.state)
}

func (pc *pipelineController) isCompletedLocked(step connectionStep) bool {
	for _, c := range pc.state.Completed {
		if c == string(step) {
			return true
		}
	}
	return false
}

// setCurrent переводит схему на этап step. Этапы двигаются только вперёд:
// повторные события от следующих воркеров (turn_allocated, dtls_ok…) не
// откатывают схему назад и не перевзводят таймер.
func (pc *pipelineController) setCurrent(appCtx context.Context, step connectionStep) {
	pc.mu.Lock()
	if pc.state.Failed != nil || pc.done || pc.isCompletedLocked(step) || pc.state.Current == step {
		pc.mu.Unlock()
		return
	}
	pc.state.Current = step
	pc.state.TimedOut = false
	pc.armTimeoutLocked(appCtx, step)
	pc.mu.Unlock()
	pc.emitState(appCtx)
}

func (pc *pipelineController) markCompleted(appCtx context.Context, step connectionStep) {
	pc.mu.Lock()
	if pc.state.Failed != nil || pc.done || pc.isCompletedLocked(step) {
		pc.mu.Unlock()
		return
	}
	pc.state.Completed = append(pc.state.Completed, string(step))
	pc.mu.Unlock()
	pc.emitState(appCtx)
}

// markFailed фиксирует ошибку начального подключения и останавливает сессию.
// После finish() — no-op (кроме фатальной авторизации, см. markFatal).
func (pc *pipelineController) markFailed(appCtx context.Context, step connectionStep, timedOut bool, timeoutSec int, reason string) {
	pc.mu.Lock()
	if pc.done {
		pc.mu.Unlock()
		return
	}
	pc.failLocked(appCtx, step, timedOut, timeoutSec, reason)
}

// markFatal — ошибка авторизации (неверный/истёкший пароль): останавливаем
// даже работающий туннель, переподключаться бессмысленно.
func (pc *pipelineController) markFatal(appCtx context.Context, reason string) {
	pc.mu.Lock()
	pc.failLocked(appCtx, stepDTLS, false, 0, reason)
}

// failLocked — общая часть markFailed/markFatal. Вызывается с захваченным pc.mu
// и освобождает его сам.
func (pc *pipelineController) failLocked(appCtx context.Context, step connectionStep, timedOut bool, timeoutSec int, reason string) {
	if pc.state.Failed != nil {
		pc.mu.Unlock()
		return
	}
	failed := string(step)
	pc.state.Failed = &failed
	pc.state.TimedOut = timedOut
	pc.state.TimeoutSec = timeoutSec
	pc.state.Current = step
	pc.stopTimerLocked()
	pc.mu.Unlock()
	pc.emitState(appCtx)
	runtime.EventsEmit(appCtx, "log", "ERROR", fmt.Sprintf("[СХЕМА] Ошибка на этапе %s: %s", step, reason))
	go pc.stopFunc()
}

// onDTLSResult учитывает результат DTLS-хендшейка воркера. Возвращает true,
// если провал нужно считать фатальным для начального подключения.
func (pc *pipelineController) onDTLSResult(ok bool) (fatal bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if ok {
		pc.dtlsOK = true
		return false
	}
	if pc.done || pc.dtlsOK {
		return false
	}
	pc.dtlsFailures++
	return pc.dtlsFailures >= dtlsFailuresBeforeFatal
}

func (pc *pipelineController) finish(appCtx context.Context) {
	pc.mu.Lock()
	if pc.done {
		pc.mu.Unlock()
		return
	}
	pc.done = true
	pc.state.Current = stepDone
	pc.state.Completed = append(pc.state.Completed, string(stepDone))
	pc.stopTimerLocked()
	pc.mu.Unlock()
	pc.emitState(appCtx)
}

func (pc *pipelineController) stopTimerLocked() {
	if pc.timer != nil {
		pc.timer.Stop()
		pc.timer = nil
	}
}

func (pc *pipelineController) hide() {
	pc.mu.Lock()
	pc.state.Visible = false
	pc.mu.Unlock()
}

// armTimeoutLocked взводит таймер этапа. Вызывается с захваченным pc.mu.
func (pc *pipelineController) armTimeoutLocked(appCtx context.Context, step connectionStep) {
	pc.stopTimerLocked()
	// Workers и captcha могут ждать пользователя; WG — зависит от системы.
	if step == stepWorkers || step == stepCaptcha || step == stepWG {
		return
	}
	timeout := 12 * time.Second
	switch step {
	case stepVK:
		timeout = 30 * time.Second
	case stepTurn, stepDTLS:
		// TURN allocate + DTLS через relay: 3 параллельных хендшейка по 20с
		// плюс retry воркеров. 12с было мало на медленных/лоссовых каналах.
		timeout = 40 * time.Second
	}
	pc.timer = time.AfterFunc(timeout, func() {
		pc.markFailed(appCtx, step, true, int(timeout.Seconds()), "таймаут этапа")
	})
}

func (pc *pipelineController) close() {
	pc.cancel()
	pc.mu.Lock()
	pc.stopTimerLocked()
	pc.mu.Unlock()
}

// Orchestrator — тонкий прокси между Wails UI и core.Core.
type Orchestrator struct {
	appCtx        context.Context
	mu            sync.Mutex
	sess          *coreSession
	prevLogWriter io.Writer
	onTray        func(connected bool, rx, tx int64, workers int32)
	pipeline      *pipelineController

	// Автопереподключение.
	reconnectTimer   *time.Timer
	reconnectPending bool
	reconnectAttempt int
}

// Параметры backoff автопереподключения.
const (
	reconnectMinDelay = 3 * time.Second
	reconnectMaxDelay = 60 * time.Second
	// Если туннель продержался дольше — считаем предыдущие попытки
	// «успешными» и начинаем backoff заново.
	reconnectStableAfter = 2 * time.Minute
)

func NewOrchestrator(ctx context.Context, onTray func(bool, int64, int64, int32)) *Orchestrator {
	return &Orchestrator{appCtx: ctx, onTray: onTray}
}

// Start запускает сессию по кнопке пользователя. Возвращает ошибку, если уже запущена.
func (o *Orchestrator) Start(p ConnectParams) error {
	o.cancelReconnect()
	o.mu.Lock()
	o.reconnectAttempt = 0
	o.mu.Unlock()
	return o.start(p, false)
}

func (o *Orchestrator) start(p ConnectParams, isReconnect bool) error {
	o.mu.Lock()
	if o.sess != nil {
		o.mu.Unlock()
		return fmt.Errorf("already running")
	}
	placeholder := &coreSession{}
	o.sess = placeholder
	o.mu.Unlock()

	sess, err := o.launch(p, isReconnect)
	if err != nil {
		o.mu.Lock()
		if o.sess == placeholder {
			o.sess = nil
		}
		o.mu.Unlock()
		return err
	}

	o.mu.Lock()
	o.sess = sess
	o.mu.Unlock()
	return nil
}

func (o *Orchestrator) launch(p ConnectParams, isReconnect bool) (*coreSession, error) {
	// Перехватываем стандартный логгер → Wails события
	if _, already := log.Writer().(*wailsLogWriter); !already {
		o.prevLogWriter = log.Writer()
	}
	lw := &wailsLogWriter{ctx: o.appCtx, file: newSessionLogFile(p.Profile)}
	lw.start()
	log.SetOutput(lw)

	var prof *ProfileData
	if p.PeerAddr != "" {
		// Runtime profile data provided (e.g. subscription profiles) — use it directly.
		prof = &ProfileData{
			PeerAddr: p.PeerAddr,
			Password: p.Password,
			Hashes:   p.PHashes,
			Listen:   p.Listen,
			TurnHost: p.TurnHost,
			TurnPort: p.TurnPort,
			DeviceID: p.DeviceID,
		}
	} else {
		var err error
		prof, err = loadProfile(p.Profile)
		if err != nil {
			return nil, err
		}
	}

	workers := p.Workers
	if workers <= 0 {
		workers = 24
	}

	hashes := prof.Hashes
	if len(p.Hashes) > 0 {
		hashes = p.Hashes
	}

	wgIfaceName := p.WGInterface
	if wgIfaceName == "" {
		wgIfaceName = "WDTT"
	}

	// Дефолты AutoWG=ON и DNS-прокси=ON заданы на стороне фронта (DEFAULT_SETTINGS).
	// Здесь просто уважаем выбор пользователя; если пакет AutoWG пуст/не передан,
	// CLI-сборка не работала без WG, а Wails оставляет туннель «готовым» без трафика.
	autoWG := p.AutoWG
	if !autoWG {
		autoWG = true
	}
	noDNS := p.NoDNSProxy
	var dnsUpstream []string
	if !noDNS {
		if len(p.DNSUpstream) > 0 {
			dnsUpstream = p.DNSUpstream
		} else {
			dnsUpstream = []string{"8.8.8.8", "1.1.1.1"}
		}
	}

	// Fingerprint: глобальный из настроек, если не передан — из профиля
	fingerprint := p.Fingerprint
	if fingerprint == "" {
		fingerprint = prof.Fingerprint
	}

	cfg := core.Config{
		PeerAddr:    prof.PeerAddr,
		Password:    prof.Password,
		Hashes:      hashes,
		Listen:      prof.Listen,
		TurnHost:    prof.TurnHost,
		TurnPort:    prof.TurnPort,
		DeviceID:    prof.DeviceID,
		Fingerprint: fingerprint,
		ClientIDs:   prof.ClientIDs,
		Workers:     workers,
		CaptchaMode: p.CaptchaMode,
		VkAuthMode:  p.VkAuthMode,
		WGConfigMTU: p.MTU,

		// Наши уникальные фичи
		AutoWG:         autoWG,
		DNSUpstream:    dnsUpstream,
		NoDNSProxy:     noDNS,
		WGInterface:    wgIfaceName,
		ExcludeDomains: p.ExcludeDomains,
		ObfsMode:       p.ObfsMode,
	}

	// Регистрируем WebView2-решатель капчи для Windows
	core.WebViewCaptchaHandler = SolveCaptchaWebView

	c := core.New(cfg)
	events, err := c.Start(o.appCtx)
	if err != nil {
		return nil, fmt.Errorf("core start: %w", err)
	}

	sess := &coreSession{c: c, doneCh: events, done: make(chan struct{}), params: p, isReconnect: isReconnect}
	o.mu.Lock()
	// Остановка по ошибке схемы — не «по кнопке»: для reconnect-сессий
	// она приведёт к следующей попытке с backoff.
	o.pipeline = newPipelineController(func() { o.stopSession(sess, false) })
	o.mu.Unlock()
	go o.forwardEvents(sess)
	// Polling-цикл статистики для tray-иконки.
	if o.onTray != nil {
		go o.statsLoop(sess)
	}
	return sess, nil
}

// statsLoop опрашивает core.Stats() каждые 2с и дёргает onTray callback.
// Работает пока жива сессия.
func (o *Orchestrator) statsLoop(sess *coreSession) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-sess.done:
			o.onTray(false, 0, 0, 0)
			return
		case <-t.C:
			snap := sess.c.Stats()
			o.onTray(true, snap.TotalBytesDown, snap.TotalBytesUp, snap.ActiveConnections)
		}
	}
}

func (o *Orchestrator) forwardEvents(sess *coreSession) {
	defer close(sess.done)
	for ev := range sess.doneCh {
		switch ev.Type {
		case core.EventState:
			runtime.EventsEmit(o.appCtx, "state_changed", ev.Status, "")
			runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("[СОСТОЯНИЕ] %s", ev.Status))
		case core.EventLog:
			runtime.EventsEmit(o.appCtx, "log", ev.Level, ev.Msg)
			if strings.Contains(ev.Msg, "VK_AUTH_REQUIRED|") {
				parts := strings.SplitN(ev.Msg, "VK_AUTH_REQUIRED|", 2)
				if len(parts) == 2 {
					hash := strings.TrimSpace(parts[1])
					runtime.EventsEmit(o.appCtx, "vk_auth_required", hash)
				}
			}
		case core.EventError:
			runtime.EventsEmit(o.appCtx, "error", ev.Msg)
			runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("[ОШИБКА] %s", ev.Msg))
		case core.EventEvent:
			o.handlePipelineEvent(ev)
			if ev.Name == "wg_config" {
				runtime.EventsEmit(o.appCtx, "log", "INFO", "[WG] Применение конфига...")
				runtime.EventsEmit(o.appCtx, "log", "INFO", "[WG] Конфиг применён, туннель активен ✓")
				runtime.EventsEmit(o.appCtx, "state_changed", "running", "")
			}
			if ev.Name == "captcha_required" {
				runtime.EventsEmit(o.appCtx, "captcha_required", ev.Data)
			}
			if ev.Name == "vk_auth_required" {
				runtime.EventsEmit(o.appCtx, "vk_auth_required", ev.Data)
			}
			if ev.Name == "fatal_auth" {
				sess.fatal.Store(true)
				runtime.EventsEmit(o.appCtx, "error", ev.Data)
			}
			if ev.Name == "wg_up" {
				sess.wasUp.Store(true)
				sess.upAt.Store(time.Now().Unix())
				if sess.isReconnect {
					runtime.EventsEmit(o.appCtx, "log", "INFO", "[RECONNECT] Туннель восстановлен ✓")
				}
			}
			runtime.EventsEmit(o.appCtx, "event", ev.Name, ev.Data)
		}
	}
	// Канал закрыт — core завершился
	core.TeardownWindowsWireGuard()
	if lw, ok := log.Writer().(*wailsLogWriter); ok {
		select {
		case <-lw.stop:
		default:
			close(lw.stop)
		}
		if lw.file != nil {
			lw.file.Close()
		}
	}
	if o.prevLogWriter != nil {
		log.SetOutput(o.prevLogWriter)
	}
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("[%s] Сессия завершена", ts))
	runtime.EventsEmit(o.appCtx, "log", "INFO", "[СОСТОЯНИЕ] Туннель остановлен")
	o.mu.Lock()
	if o.sess == sess {
		o.sess = nil
	}
	if o.pipeline != nil {
		o.pipeline.close()
		o.pipeline = nil
	}
	o.mu.Unlock()

	if o.scheduleReconnect(sess) {
		return
	}
	runtime.EventsEmit(o.appCtx, "state_changed", "disconnected", "")
}

// scheduleReconnect решает, нужно ли поднимать сессию заново, и если да —
// взводит таймер. Возвращает true, если переподключение запланировано.
//
// Переподключаемся, если:
//   - включено в настройках (params.AutoReconnect);
//   - сессию не останавливал пользователь;
//   - не было фатальной ошибки авторизации;
//   - туннель хотя бы раз был поднят ИЛИ это уже reconnect-сессия
//     (иначе ошибка первого подключения показывается пользователю как раньше).
func (o *Orchestrator) scheduleReconnect(sess *coreSession) bool {
	if !sess.params.AutoReconnect || sess.userStop.Load() || sess.fatal.Load() {
		return false
	}
	if !sess.wasUp.Load() && !sess.isReconnect {
		return false
	}

	o.mu.Lock()
	if sess.wasUp.Load() && time.Since(time.Unix(sess.upAt.Load(), 0)) > reconnectStableAfter {
		o.reconnectAttempt = 0
	}
	attempt := o.reconnectAttempt
	o.reconnectAttempt++
	delay := reconnectMaxDelay
	if attempt < 8 {
		delay = reconnectMinDelay << uint(attempt)
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}
	}
	o.reconnectPending = true
	params := sess.params
	o.reconnectTimer = time.AfterFunc(delay, func() { o.doReconnect(params) })
	o.mu.Unlock()

	runtime.EventsEmit(o.appCtx, "log", "WARN", fmt.Sprintf("[RECONNECT] Туннель упал, переподключение через %v (попытка %d)", delay, attempt+1))
	runtime.EventsEmit(o.appCtx, "state_changed", "reconnecting", "")
	return true
}

func (o *Orchestrator) doReconnect(params ConnectParams) {
	o.mu.Lock()
	if !o.reconnectPending {
		o.mu.Unlock()
		return
	}
	o.reconnectPending = false
	o.reconnectTimer = nil
	o.mu.Unlock()

	runtime.EventsEmit(o.appCtx, "log", "INFO", "[RECONNECT] Переподключение...")
	if err := o.start(params, true); err != nil {
		runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("[RECONNECT] Не удалось запустить: %v", err))
		// Поднять не удалось (порт занят, профиль пропал…) — планируем ещё раз.
		fake := &coreSession{params: params, isReconnect: true}
		if !o.scheduleReconnect(fake) {
			runtime.EventsEmit(o.appCtx, "state_changed", "disconnected", "")
		}
	}
}

// cancelReconnect отменяет запланированное переподключение (если есть).
// Возвращает true, если оно было запланировано.
func (o *Orchestrator) cancelReconnect() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.reconnectPending {
		return false
	}
	o.reconnectPending = false
	if o.reconnectTimer != nil {
		o.reconnectTimer.Stop()
		o.reconnectTimer = nil
	}
	return true
}

func (o *Orchestrator) handlePipelineEvent(ev core.Event) {
	o.mu.Lock()
	pc := o.pipeline
	o.mu.Unlock()
	if pc == nil {
		return
	}
	switch ev.Name {
	case "pipeline_start":
		pc.setCurrent(o.appCtx, stepDNS)
	case "vk_creds_ok":
		pc.markCompleted(o.appCtx, stepVK)
		pc.setCurrent(o.appCtx, stepWrap)
	case "captcha_required":
		pc.setCurrent(o.appCtx, stepCaptcha)
	case "wrap_ready":
		pc.markCompleted(o.appCtx, stepWrap)
		pc.setCurrent(o.appCtx, stepTurn)
	case "turn_allocated":
		pc.markCompleted(o.appCtx, stepTurn)
		pc.setCurrent(o.appCtx, stepDTLS)
	case "dtls_ok":
		pc.onDTLSResult(true)
		pc.markCompleted(o.appCtx, stepDTLS)
		pc.setCurrent(o.appCtx, stepWorkers)
	case "worker_ready":
		pc.markCompleted(o.appCtx, stepWorkers)
		pc.setCurrent(o.appCtx, stepWG)
	case "wg_up":
		pc.markCompleted(o.appCtx, stepWG)
		pc.finish(o.appCtx)
	case "wrap_auth_timeout":
		reason := "WRAP/DTLS не подтверждён: неверный пароль, сервер недоступен или UDP режет оператор"
		if pc.onDTLSResult(false) {
			runtime.EventsEmit(o.appCtx, "log", "ERROR", "[СХЕМА] Ошибка DTLS: "+reason)
			pc.markFailed(o.appCtx, stepDTLS, false, 0, reason)
		} else {
			runtime.EventsEmit(o.appCtx, "log", "WARN", "[DTLS] Таймаут хендшейка воркера, повторим ("+ev.Data+")")
		}
	case "dtls_error":
		if pc.onDTLSResult(false) {
			runtime.EventsEmit(o.appCtx, "log", "ERROR", "[СХЕМА] Ошибка DTLS: "+ev.Data)
			pc.markFailed(o.appCtx, stepDTLS, false, 0, ev.Data)
		} else {
			runtime.EventsEmit(o.appCtx, "log", "WARN", "[DTLS] Ошибка хендшейка воркера, повторим ("+ev.Data+")")
		}
	case "fatal_auth":
		runtime.EventsEmit(o.appCtx, "log", "ERROR", "[СХЕМА] Ошибка авторизации: "+ev.Data)
		pc.markFatal(o.appCtx, ev.Data)
	}
}

// Stop останавливает текущую сессию (если есть) и ЖДЁТ полного teardown.
// Без ожидания следующий Start() через миллисекунды получает "already running"
// потому что o.sess обнуляется только в forwardEvents после WG-teardown,
// а это занимает 5-15 секунд.
func (o *Orchestrator) Stop() {
	// Пользователь нажал «Отключить»: отменяем ожидающее переподключение.
	if o.cancelReconnect() {
		runtime.EventsEmit(o.appCtx, "log", "INFO", "[RECONNECT] Автопереподключение отменено")
		runtime.EventsEmit(o.appCtx, "state_changed", "disconnected", "")
	}
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		return
	}
	o.stopSession(sess, true)
}

// stopSession останавливает конкретную сессию. userInitiated=true помечает
// её как остановленную пользователем (автопереподключения не будет).
func (o *Orchestrator) stopSession(sess *coreSession, userInitiated bool) {
	if sess == nil || sess.c == nil {
		return
	}
	if userInitiated {
		sess.userStop.Store(true)
	}
	sess.c.Stop()
	if sess.done != nil {
		select {
		case <-sess.done:
		case <-time.After(8 * time.Second):
			log.Printf("[ORCH] Stop timeout: teardown is still running in background")
		}
	}
	// Force-clear session even if teardown hasn't completed,
	// so the next Start() doesn't get "already running".
	o.mu.Lock()
	if o.sess == sess {
		o.sess = nil
	}
	o.mu.Unlock()
	core.TeardownWindowsWireGuard()
}

// SendCaptchaResult передаёт токен капчи в ядро.
func (o *Orchestrator) SendCaptchaResult(token string) {
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		return
	}
	sess.c.SolveCaptcha(token)
}

// SendTurnCreds передаёт TURN-креды от VK-аккаунта в ядро.
func (o *Orchestrator) SendTurnCreds(payload string) {
	core.HandleTurnCredsPayload("TURN_CREDS|" + payload)
}

// Pause/Resume управляют doze-режимом воркеров.
func (o *Orchestrator) Pause() {
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		return
	}
	sess.c.Pause()
}

func (o *Orchestrator) Resume() {
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		return
	}
	sess.c.Resume()
}

// IsRunning — есть ли активная сессия или ожидающее автопереподключение.
func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return (o.sess != nil && o.sess.c != nil) || o.reconnectPending
}

