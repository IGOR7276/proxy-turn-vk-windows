package backend

import (
	"context"
	"net"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Dashboard — снимок рантайма для live-дашборда (U1).
//
// Единственный источник правды — бэкенд (Q1): значения собираются из
// core.Stats() и полей оркестратора, фронт только читает их через событие
// "stats" (раз в секунду) или метод GetDashboard при первом рендере.
type Dashboard struct {
	State        string  `json:"state"`
	WorkersAlive int32   `json:"workersAlive"`
	WorkersTotal int32   `json:"workersTotal"`
	SpeedRX      int64   `json:"speedRx"` // байт/с
	SpeedTX      int64   `json:"speedTx"` // байт/с
	TotalRX      int64   `json:"totalRx"` // байт с начала сессии
	TotalTX      int64   `json:"totalTx"`
	AvgRTTMs     float64 `json:"avgRttMs"`
	UptimeSec    int64   `json:"uptimeSec"`
	Reconnects   int64   `json:"reconnects"`
	LastError    string  `json:"lastError"`
	DNSResponses uint64  `json:"dnsResponses"`
	DNSFailures  uint64  `json:"dnsFailures"`
	UpdatedAt    int64   `json:"updatedAt"`
}

// ConnectionCheck — результат кнопки «Проверить соединение».
type ConnectionCheck struct {
	OK           bool    `json:"ok"`
	WorkersAlive int32   `json:"workersAlive"`
	LatencyMs    float64 `json:"latencyMs"`
	Resolved     int     `json:"resolved"`
	Error        string  `json:"error"`
	CheckedAt    int64   `json:"checkedAt"`
}

// emitState — единая точка смены состояния туннеля. Пишет в дашборд и
// отправляет событие в UI, чтобы состояние не расходилось между каналами.
func (o *Orchestrator) emitState(status string) {
	o.state.Store(status)
	runtime.EventsEmit(o.appCtx, "state_changed", status, "")
}

func (o *Orchestrator) stateString() string {
	if v, ok := o.state.Load().(string); ok && v != "" {
		return v
	}
	return "idle"
}

func (o *Orchestrator) lastErrorString() string {
	if v, ok := o.lastErr.Load().(string); ok {
		return v
	}
	return ""
}

// setLastError запоминает последнюю ошибку для дашборда. Пустая строка
// очищает поле (например, после успешного wg_up).
func (o *Orchestrator) setLastError(msg string) {
	o.lastErr.Store(msg)
}

// Dashboard собирает текущий снимок. Безопасен при отсутствии сессии.
func (o *Orchestrator) Dashboard() Dashboard {
	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()

	d := Dashboard{
		State:      o.stateString(),
		Reconnects: o.reconnects.Load(),
		LastError:  o.lastErrorString(),
		SpeedRX:    o.speedRx.Load(),
		SpeedTX:    o.speedTx.Load(),
	}
	if sess != nil && sess.c != nil {
		snap := sess.c.Stats()
		d.WorkersAlive = snap.ActiveConnections
		d.WorkersTotal = snap.WorkersTotal
		d.TotalRX = snap.TotalBytesDown
		d.TotalTX = snap.TotalBytesUp
		d.AvgRTTMs = snap.AvgRTTMillis
		d.UptimeSec = snap.UptimeSeconds
		d.DNSResponses = snap.DNSResponses
		d.DNSFailures = snap.DNSFailures
	}
	return d
}

// dashboardLoop раз в секунду считает скорость по дельте байтов и рассылает
// событие "stats" — отдельный канал, не смешанный с логами (U8).
func (o *Orchestrator) dashboardLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()

	var prevRx, prevTx int64
	prevAt := time.Now()
	for range t.C {
		d := o.Dashboard()
		now := time.Now()
		dt := now.Sub(prevAt).Seconds()
		if dt > 0 {
			rx := int64(float64(d.TotalRX-prevRx) / dt)
			tx := int64(float64(d.TotalTX-prevTx) / dt)
			if rx < 0 {
				rx = 0 // сессия пересоздалась, счётчики сбросились
			}
			if tx < 0 {
				tx = 0
			}
			o.speedRx.Store(rx)
			o.speedTx.Store(tx)
		}
		prevRx, prevTx, prevAt = d.TotalRX, d.TotalTX, now

		d.SpeedRX = o.speedRx.Load()
		d.SpeedTX = o.speedTx.Load()
		d.UpdatedAt = now.Unix()
		runtime.EventsEmit(o.appCtx, "stats", d)
	}
}

// CheckConnection — активная проверка: есть ли живые воркеры и резолвится ли
// домен через DNS-прокси/туннель. Возвращает задержку проверки.
func (o *Orchestrator) CheckConnection() ConnectionCheck {
	start := time.Now()
	res := ConnectionCheck{CheckedAt: start.Unix()}

	o.mu.Lock()
	sess := o.sess
	o.mu.Unlock()
	if sess == nil || sess.c == nil {
		res.Error = "туннель не запущен"
		return res
	}

	snap := sess.c.Stats()
	res.WorkersAlive = snap.ActiveConnections
	if snap.ActiveConnections == 0 {
		res.Error = "нет живых воркеров"
		return res
	}

	// Резолвим через 8.8.8.8 напрямую: default route при поднятом WG ведёт в
	// туннель, поэтому успешный ответ означает, что трафик реально проходит.
	ctx, cancel := context.WithTimeout(o.appCtx, 6*time.Second)
	defer cancel()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 4 * time.Second}
			return d.DialContext(ctx, "udp", "8.8.8.8:53")
		},
	}
	addrs, err := resolver.LookupHost(ctx, "api.vk.com")
	res.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Resolved = len(addrs)
	res.OK = len(addrs) > 0
	if !res.OK {
		res.Error = "DNS вернул пустой ответ"
	}
	return res
}
