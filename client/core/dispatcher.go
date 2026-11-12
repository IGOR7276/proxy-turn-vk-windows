package core

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var pktPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 2048)
	},
}

func getPktBuf(size int) []byte {
	b := pktPool.Get().([]byte)
	if cap(b) < size {
		b = make([]byte, size)
	}
	return b[:size]
}

func putPktBuf(b []byte) {
	if cap(b) < 2048 {
		return
	}
	pktPool.Put(b[:cap(b)])
}

const (
	returnChBuf = 384

	// localWriteTimeout ограничивает запись в локальный UDP-сокет (WG):
	// без deadline одно залипание останавливает writeLoop и весь пул.
	localWriteTimeout = 2 * time.Second

	// chunkSize — стартовое количество последовательных пакетов, отправляемых
	// в один worker перед переключением на следующий.
	//
	// Зачем: при round-robin (chunk=1) каждый пакет летит через разный TURN relay
	// с разным latency, что приводит к reorder на сервере. TCP внутри WireGuard
	// интерпретирует reorder как потери → cwnd collapse → скорость single-flow
	// падает до ~8 KB/s.
	//
	// С chunk=8: пакеты в пределах одного TCP congestion window (~10 пакетов при
	// initial cwnd) уходят через один TURN relay → прилетают по порядку.
	// Reorder возможен только между chunk-границами, что покрывается WG replay
	// window (2048 пакетов).
	//
	// Агрегатная пропускная способность не меняется — все workers загружены
	// равномерно по-прежнему (каждый получает 1/N от общего трафика за время).
	//
	// chunkSize теперь адаптивный (см. computeChunkSize): при потерях chunk
	// уменьшается, чтобы быстрее объезжать проблемный relay, при низком RTT —
	// растёт, чтобы меньше страдать от reorder.
	chunkSize = 8

	chunkMin = 4
	chunkMax = 16

	// Параметры EWMA для RTT/потерь (активные пробы — DTLS keepalive 0xFF).
	rttEWMAAlpha  = 5 // 1/5: new = old*(alpha-1)/alpha + sample/alpha
	lossEWMAAlpha = 5

	// healthInterval — как часто пересчитывается адаптивный chunkSize.
	healthInterval = 3 * time.Second
)

// WorkerSlot — один воркер + его health-метрики.
//
// Пробы: сессия шлёт DTLS keepalive (байт 0xFF) каждые 10с и ждёт pong.
// По pong считается RTT (EWMA), по отсутствию pong — потери (EWMA).
// Health используется планировщиком: пакеты в первую очередь идут через
// воркеры с низким RTT и без потерь, «протухшие» воркеры обходятся.
type WorkerSlot struct {
	ID     int
	SendCh chan []byte

	rttEWMA   atomic.Int64  // микросекунды
	lossEWMA  atomic.Uint32 // basis points: 0..10000
	probesOK  atomic.Uint64
	probesBad atomic.Uint64
	lastRx    atomic.Int64 // unixnano последнего пакета от сервера
	lastProbe atomic.Int64 // unixnano отправки последней пробы
	pending   atomic.Bool  // проба отправлена, pong ещё не пришёл
}

// observeRTT обновляет EWMA RTT.
func (w *WorkerSlot) observeRTT(d time.Duration) {
	if d <= 0 {
		return
	}
	sample := d.Microseconds()
	old := w.rttEWMA.Load()
	if old <= 0 {
		w.rttEWMA.Store(sample)
		return
	}
	w.rttEWMA.Store(old + (sample-old)/rttEWMAAlpha)
}

// recordProbeLost обновляет EWMA потерь (100% = 10000 bp).
func (w *WorkerSlot) recordProbeLost() {
	old := w.lossEWMA.Load()
	w.lossEWMA.Store(uint32(int64(old) + (10000-int64(old))/lossEWMAAlpha))
}

// recordProbeOK обновляет EWMA потерь в сторону нуля.
func (w *WorkerSlot) recordProbeOK() {
	old := w.lossEWMA.Load()
	w.lossEWMA.Store(uint32(int64(old) - int64(old)/lossEWMAAlpha))
}

// Health — интегральная оценка 1..1000: чем выше, тем лучше воркер.
// Свежий воркер без проб получает нейтральные ~650, чтобы он получил шанс.
func (w *WorkerSlot) Health() int {
	rtt := w.rttEWMA.Load()
	if rtt <= 0 {
		rtt = 80_000 // неизвестно → считаем 80мс
	}
	loss := float64(w.lossEWMA.Load()) / 10000.0
	rttScore := 1000.0 / (1.0 + float64(rtt)/150_000.0)
	score := int(rttScore * (1.0 - 0.85*loss))
	if score < 1 {
		return 1
	}
	if score > 1000 {
		return 1000
	}
	return score
}

// RTTMillis и LossPercent — для логов/диагностики.
func (w *WorkerSlot) RTTMillis() float64 { return float64(w.rttEWMA.Load()) / 1000.0 }
func (w *WorkerSlot) LossPercent() float64 {
	return float64(w.lossEWMA.Load()) / 100.0
}

type Dispatcher struct {
	localConn  net.PacketConn
	clientAddr atomic.Pointer[net.Addr]
	workers    atomic.Pointer[[]*WorkerSlot]
	mu         sync.Mutex // Используется только для записи
	rrIndex    int
	rrCount    int
	// droppedPackets — счётчик пакетов, дропнутых при переполнении всех workers.
	// lastDropLog — unixnano последнего лога о дропах (для троттлинга, не чаще раза в 10с).
	droppedPackets int64
	lastDropLog    int64
	// droppedReturns — счётчик пакетов, дропнутых Reader'ами из-за
	// переполнения ReturnCh (writeLoop не успевает писать в localConn).
	droppedReturns int64
	droppedWrites  int64
	ReturnCh       chan []byte
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	stats          *Stats

	// chunkCur — текущий адаптивный размер chunk'а (обновляется healthLoop).
	chunkCur atomic.Int32
}

func NewDispatcher(ctx context.Context, localConn net.PacketConn, stats *Stats) *Dispatcher {
	dctx, dcancel := context.WithCancel(ctx)
	d := &Dispatcher{
		localConn: localConn,
		ReturnCh:  make(chan []byte, returnChBuf),
		ctx:       dctx,
		cancel:    dcancel,
		stats:     stats,
	}
	
	empty := make([]*WorkerSlot, 0)
	d.workers.Store(&empty)
	d.chunkCur.Store(chunkSize)

	d.wg.Add(3)
	go d.readLoop()
	go d.writeLoop()
	go d.healthLoop()
	return d
}

func (d *Dispatcher) Shutdown() {
	d.cancel()
	d.wg.Wait()
}

func (d *Dispatcher) Register(w *WorkerSlot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	oldWorkers := d.workers.Load()
	newWorkers := make([]*WorkerSlot, len(*oldWorkers)+1)
	copy(newWorkers, *oldWorkers)
	newWorkers[len(*oldWorkers)] = w
	d.workers.Store(&newWorkers)
	log.Printf("[ДИСП] Воркер #%d зарегистрирован (всего: %d)", w.ID, len(newWorkers))
}

func (d *Dispatcher) Unregister(slot *WorkerSlot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	oldWorkers := d.workers.Load()
	newWorkers := make([]*WorkerSlot, 0, len(*oldWorkers))
	for _, w := range *oldWorkers {
		if w != slot {
			newWorkers = append(newWorkers, w)
		}
	}
	d.workers.Store(&newWorkers)
	log.Printf("[ДИСП] Воркер #%d отключён (осталось: %d)", slot.ID, len(newWorkers))
}

// healthLoop периодически пересчитывает адаптивный chunkSize по агрегатным
// RTT/потерям воркеров.
func (d *Dispatcher) healthLoop() {
	defer d.wg.Done()
	t := time.NewTicker(healthInterval)
	defer t.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
			d.chunkCur.Store(int32(d.computeChunkSize()))
		}
	}
}

// computeChunkSize выбирает размер chunk'а:
//   - потери > 10% → 4 (быстро объезжаем проблемные relay);
//   - потери 5-10% → 6;
//   - потери 2-5%  → 8;
//   - потерь почти нет и RTT < 60мс → 16 (меньше reorder-границ);
//   - RTT < 150мс → 12;
//   - иначе 8.
func (d *Dispatcher) computeChunkSize() int {
	ws := *d.workers.Load()
	var rttSum int64
	var lossSum uint64
	n := 0
	for _, w := range ws {
		rtt := w.rttEWMA.Load()
		if rtt <= 0 {
			continue
		}
		rttSum += rtt
		lossSum += uint64(w.lossEWMA.Load())
		n++
	}
	if n == 0 {
		return chunkSize
	}
	avgRTT := rttSum / int64(n)
	avgLoss := float64(lossSum) / float64(n) / 10000.0
	switch {
	case avgLoss > 0.10:
		return 4
	case avgLoss > 0.05:
		return 6
	case avgLoss > 0.02:
		return 8
	case avgRTT < 60_000:
		return 16
	case avgRTT < 150_000:
		return 12
	default:
		return 8
	}
}

// bestWorker выбирает индекс самого здорового воркера с свободным местом
// в канале. skip исключается (обычно — текущий). Сканирование начинается
// с d.rrIndex, поэтому при равном health воркеры чередуются, а не
// залипают на первом.
func (d *Dispatcher) bestWorker(ws []*WorkerSlot, skip int) int {
	nw := len(ws)
	if nw == 0 {
		return -1
	}
	start := d.rrIndex
	if start < 0 || start >= nw {
		start = 0
	}
	best := -1
	bestScore := -1
	for k := 0; k < nw; k++ {
		i := (start + k) % nw
		if i == skip {
			continue
		}
		w := ws[i]
		free := cap(w.SendCh) - len(w.SendCh)
		if free <= 0 {
			continue
		}
		// health — главный критерий, свободное место — тайбрейкер.
		score := w.Health()*1000 + free
		if score > bestScore {
			bestScore, best = score, i
		}
	}
	return best
}

// WorkersTotal — число зарегистрированных воркеров (живых + переподключающихся).
func (d *Dispatcher) WorkersTotal() int {
	return len(*d.workers.Load())
}

// AvgRTT возвращает средний EWMA RTT по воркерам, у которых есть пробы.
// 0 — данных ещё нет (сразу после старта).
func (d *Dispatcher) AvgRTT() float64 {
	ws := *d.workers.Load()
	var sum int64
	var n int64
	for _, w := range ws {
		if rtt := w.rttEWMA.Load(); rtt > 0 {
			sum += rtt
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n) / 1000.0
}

// readLoop читает WireGuard-пакеты и распределяет по workers chunk'ами.
//
// Логика: отправляем chunkSize подряд пакетов в один worker, потом переходим
// к следующему. Если текущий worker перегружен (канал полный) — немедленно
// ищем свободный worker и начинаем новый chunk на нём. Это гарантирует:
//   - В рамках chunk пакеты идут через один TURN relay → in-order delivery
//   - Между chunks — разные relay → максимальная агрегатная скорость
//   - Нет блокировки, нет буферизации, нет дополнительного latency
func (d *Dispatcher) readLoop() {
	defer d.wg.Done()

	// Stack-allocated большой буфер для ReadFrom (один раз, без GC).
	// Копируем в pool нужного размера — экономит память на типичных
	// пакетах <200 байт (раньше тянули 2048-байтный буфер из пула).
	buf := make([]byte, readBufSize)
	for {
		if err := d.ctx.Err(); err != nil {
			return
		}

		n, addr, err := d.localConn.ReadFrom(buf)
		if err != nil {
			if d.ctx.Err() != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		pkt := getPktBuf(n)
		copy(pkt, buf[:n])
		pkt = pkt[:n]

		d.clientAddr.Store(&addr)
		atomic.AddInt64(&d.stats.TotalBytesUp, int64(n))

		workersPtr := d.workers.Load()
		if workersPtr == nil || len(*workersPtr) == 0 {
			putPktBuf(pkt)
			continue
		}

		ws := *workersPtr
		nw := len(ws)

		chunk := int(d.chunkCur.Load())
		if chunk < chunkMin {
			chunk = chunkMin
		}
		if chunk > chunkMax {
			chunk = chunkMax
		}

		// Начало нового chunk'а — выбираем самого здорового воркера
		// (низкий RTT, без потерь) с свободным каналом.
		if d.rrCount == 0 || d.rrIndex < 0 || d.rrIndex >= nw {
			d.rrIndex = d.bestWorker(ws, -1)
			d.rrCount = 0
		}

		sent := false
		if d.rrIndex >= 0 {
			// Пробуем текущий worker (chunk affinity)
			w := ws[d.rrIndex]
			select {
			case w.SendCh <- pkt:
				sent = true
				d.rrCount++
				if d.rrCount >= chunk {
					// Chunk закончен — следующий начинаем на другом лучшем.
					d.rrIndex = d.bestWorker(ws, d.rrIndex)
					d.rrCount = 0
				}
			default:
				// Текущий worker перегружен — берём лучшего свободного,
				// начинаем новый chunk на нём.
				if alt := d.bestWorker(ws, d.rrIndex); alt >= 0 {
					select {
					case ws[alt].SendCh <- pkt:
						sent = true
						d.rrIndex = alt
						d.rrCount = 1 // первый пакет нового chunk'а уже отправлен
					default:
					}
				}
			}
		}

		if !sent {
			// Все workers перегружены — сдвигаем указатель, пакет дропается.
			// Инкрементируем счётчик дропов и логируем троттлированно (не чаще раза в 10с),
			// чтобы избежать спама при перегрузке.
			d.rrCount = 0
			putPktBuf(pkt)
			dropped := atomic.AddInt64(&d.droppedPackets, 1)
			now := time.Now().UnixNano()
			last := atomic.LoadInt64(&d.lastDropLog)
			if now-last > int64(10*time.Second) && atomic.CompareAndSwapInt64(&d.lastDropLog, last, now) {
				log.Printf("[ДИСП] Все %d workers перегружены, дропнуто пакетов: %d (всего)", nw, dropped)
			}
		}
	}
}

func (d *Dispatcher) writeLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.ctx.Done():
			return
		case pkt := <-d.ReturnCh:
			addrPtr := d.clientAddr.Load()
			if addrPtr == nil {
				putPktBuf(pkt)
				continue
			}
			addr := *addrPtr
			// Deadline защищает от залипания сокета WG: иначе writeLoop
			// мог стоять минутами, а Reader'ы воркеров — на ReturnCh.
			_ = d.localConn.SetWriteDeadline(time.Now().Add(localWriteTimeout))
			if _, err := d.localConn.WriteTo(pkt, addr); err != nil {
				if d.ctx.Err() != nil {
					putPktBuf(pkt)
					return
				}
				if n := atomic.AddInt64(&d.droppedWrites, 1); n%1000 == 1 {
					log.Printf("[ДИСП] Ошибка записи в локальный сокет (всего %d): %v", n, err)
				}
			} else {
				atomic.AddInt64(&d.stats.TotalBytesDown, int64(len(pkt)))
			}
			putPktBuf(pkt)
		}
	}
}

// noteReturnDrop считает пакеты, дропнутые из-за переполнения ReturnCh,
// и пишет лог не чаще раза в 10 секунд.
func (d *Dispatcher) noteReturnDrop() {
	dropped := atomic.AddInt64(&d.droppedReturns, 1)
	now := time.Now().UnixNano()
	last := atomic.LoadInt64(&d.lastDropLog)
	if now-last > int64(10*time.Second) && atomic.CompareAndSwapInt64(&d.lastDropLog, last, now) {
		log.Printf("[ДИСП] ReturnCh переполнен (writeLoop не успевает), дропнуто: %d", dropped)
	}
}

