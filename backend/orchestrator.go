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
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"wg-turn-client/core"
)

// wailsLogWriter перехватывает log.Printf и направляет в Wails-события.
// Буферизует записи и флашит каждые 100ms чтобы не блокировать core.
type wailsLogWriter struct {
	ctx  context.Context
	mu   sync.Mutex
	buf  []logEntry
	stop chan struct{}
}

type logEntry struct{ level, msg string }

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
	w.mu.Lock()
	w.buf = append(w.buf, logEntry{level, msg})
	w.mu.Unlock()
	return len(p), nil
}

func classifyLevel(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "ошибка") ||
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
}

// ConnectParams — runtime параметры от UI.
type ConnectParams struct {
	Profile     string   `json:"profile"`
	CaptchaMode string   `json:"captchaMode"`
	Workers     int      `json:"workers,omitempty"`
	MTU         int      `json:"mtu,omitempty"`
	Hashes      []string `json:"hashes,omitempty"`

	// Флаги окружения (наш уникальный функционал)
	AutoWG      bool     `json:"autoWG,omitempty"`
	DNSUpstream []string `json:"dnsUpstream,omitempty"`
	NoDNSProxy  bool     `json:"noDNSProxy,omitempty"`
	WGInterface string   `json:"wgInterface,omitempty"`
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
}

// Orchestrator — тонкий прокси между Wails UI и core.Core.
type Orchestrator struct {
	appCtx        context.Context
	mu            sync.Mutex
	sess          *coreSession
	prevLogWriter io.Writer
}

func NewOrchestrator(ctx context.Context) *Orchestrator {
	return &Orchestrator{appCtx: ctx}
}

// Start запускает сессию. Возвращает ошибку, если уже запущена.
func (o *Orchestrator) Start(p ConnectParams) error {
	o.mu.Lock()
	if o.sess != nil {
		o.mu.Unlock()
		return fmt.Errorf("already running")
	}
	placeholder := &coreSession{}
	o.sess = placeholder
	o.mu.Unlock()

	sess, err := o.launch(p)
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

func (o *Orchestrator) launch(p ConnectParams) (*coreSession, error) {
	// Перехватываем стандартный логгер → Wails события
	if _, already := log.Writer().(*wailsLogWriter); !already {
		o.prevLogWriter = log.Writer()
	}
	lw := &wailsLogWriter{ctx: o.appCtx}
	lw.start()
	log.SetOutput(lw)

	prof, err := loadProfile(p.Profile)
	if err != nil {
		return nil, err
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

	cfg := core.Config{
		PeerAddr:    prof.PeerAddr,
		Password:    prof.Password,
		Hashes:      hashes,
		Listen:      prof.Listen,
		TurnHost:    prof.TurnHost,
		TurnPort:    prof.TurnPort,
		DeviceID:    prof.DeviceID,
		Fingerprint: prof.Fingerprint,
		ClientIDs:   prof.ClientIDs,
		Workers:     workers,
		CaptchaMode: p.CaptchaMode,
		WGConfigMTU: p.MTU,

		// Наши уникальные фичи
		AutoWG:      autoWG,
		DNSUpstream: dnsUpstream,
		NoDNSProxy:  noDNS,
		WGInterface: wgIfaceName,
	}

	c := core.New(cfg)
	events, err := c.Start(o.appCtx)
	if err != nil {
		return nil, fmt.Errorf("core start: %w", err)
	}

	sess := &coreSession{c: c, doneCh: events}
	go o.forwardEvents(sess)
	return sess, nil
}

func (o *Orchestrator) forwardEvents(sess *coreSession) {
	for ev := range sess.doneCh {
		switch ev.Type {
		case core.EventState:
			runtime.EventsEmit(o.appCtx, "state_changed", ev.Status, "")
			runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("[СОСТОЯНИЕ] %s", ev.Status))
		case core.EventLog:
			runtime.EventsEmit(o.appCtx, "log", ev.Level, ev.Msg)
		case core.EventError:
			runtime.EventsEmit(o.appCtx, "error", ev.Msg)
			runtime.EventsEmit(o.appCtx, "log", "ERROR", fmt.Sprintf("[ОШИБКА] %s", ev.Msg))
		case core.EventEvent:
			if ev.Name == "wg_config" {
				runtime.EventsEmit(o.appCtx, "log", "INFO", "[WG] Конфиг применён, туннель активен ✓")
				runtime.EventsEmit(o.appCtx, "state_changed", "connected", "")
			}
			if ev.Name == "captcha_required" {
				runtime.EventsEmit(o.appCtx, "captcha_required", ev.Data)
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
	}
	if o.prevLogWriter != nil {
		log.SetOutput(o.prevLogWriter)
	}
	ts := time.Now().Format("15:04:05")
	runtime.EventsEmit(o.appCtx, "log", "INFO", fmt.Sprintf("[%s] Сессия завершена", ts))
	o.mu.Lock()
	if o.sess == sess {
		o.sess = nil
	}
	o.mu.Unlock()
	runtime.EventsEmit(o.appCtx, "state_changed", "disconnected", "")
}

// Stop останавливает текущую сессию (если есть).
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		return
	}
	sess.c.Stop()
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

func (o *Orchestrator) IsRunning() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sess != nil && o.sess.c != nil
}
