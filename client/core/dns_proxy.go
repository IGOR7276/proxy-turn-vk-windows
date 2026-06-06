package core

import (
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// dnsProxy — локальный DNS-прокси, слушающий на :53 (UDP, все интерфейсы).
// Пересылает запросы на upstream DNS-серверы через WireGuard-туннель.
//
// Зачем: в РФ (и других странах с белыми списками) провайдер может
// перехватывать DNS-запросы и отказывать в резолве заблокированных доменов
// (YouTube, Google и т.д.). С локальным прокси приложения резолвят через
// localhost — провайдер физически не может вмешаться в трафик между
// локальными процессами. Upstream-ы (8.8.8.8, 1.1.1.1 и т.д.) доступны
// через туннель, поэтому обходят любые IP-блокировки.
//
// sourceIP — опциональный IP-адрес (например, 10.66.0.32 от WireGuard-интерфейса),
// который используется как source IP для исходящих DNS-запросов к upstream.
// Это гарантирует, что пакеты идут через туннель (default route 0.0.0.0/0 → WDTT),
// а не через локальный Ethernet напрямую (где 8.8.8.8 заблокирован ISP).
type dnsProxy struct {
	listener net.PacketConn
	upstream []string
	sourceIP string
	timeout  time.Duration

	mu      sync.Mutex
	running bool

	queries   atomic.Uint64
	forwards  atomic.Uint64
	responses atomic.Uint64
	failures  atomic.Uint64
}

// newDNSProxy создаёт прокси с заданным режимом (UDP) и upstream-ами.
// Если upstream пуст — подставляются Google (8.8.8.8) и Cloudflare (1.1.1.1).
// sourceIP — IP-адрес WireGuard-интерфейса для форсирования маршрута
// исходящих DNS через туннель; пустая строка = OS сама выберет source.
func newDNSProxy(upstream []string, sourceIP string) *dnsProxy {
	if len(upstream) == 0 {
		upstream = []string{"8.8.8.8", "1.1.1.1"}
	}
	return &dnsProxy{
		upstream: upstream,
		sourceIP: sourceIP,
		timeout:  5 * time.Second,
	}
}

// Start запускает прокси на :53 (UDP, все интерфейсы — ловит и 127.0.0.1,
// и [::1]). Если порт занят — возвращает ошибку.
func (p *dnsProxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return nil
	}
	listener, err := net.ListenPacket("udp", ":53")
	if err != nil {
		return err
	}
	p.listener = listener
	p.running = true
	go p.serve()
	if p.sourceIP != "" {
		log.Printf("[DNS] Локальный прокси запущен на :53 (UDP), upstream: %v, sourceIP: %s", p.upstream, p.sourceIP)
	} else {
		log.Printf("[DNS] Локальный прокси запущен на :53 (UDP), upstream: %v", p.upstream)
	}
	return nil
}

// Stop останавливает прокси. Безопасно вызывать многократно.
// В конце печатает финальную статистику.
func (p *dnsProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.listener.Close()
	p.running = false
	log.Printf("[DNS] Локальный прокси остановлен. Статистика: queries=%d forwards=%d responses=%d failures=%d",
		p.queries.Load(), p.forwards.Load(), p.responses.Load(), p.failures.Load())
}

// serve — основной цикл. На каждый запрос запускает handleQuery в горутине,
// чтобы медленные upstream-ы не блокировали быстрые. recover() защищает
// от паник (например, если ReadFrom вернёт неожиданный err).
func (p *dnsProxy) serve() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[DNS] serve() panic: %v", r)
		}
	}()
	buf := make([]byte, 4096)
	for {
		n, addr, err := p.listener.ReadFrom(buf)
		if err != nil {
			return
		}
		p.queries.Add(1)
		go p.handleQuery(buf[:n:n], addr)
	}
}

// handleQuery параллельно опрашивает все upstream-ы и возвращает первый
// успешный ответ. Это критично для ситуаций, когда туннель/TURN-релей
// теряет часть UDP-пакетов: вместо ожидания каждого upstream-а по очереди
// (5s × N = 5-10s) мы ждём только 5s и берём самый быстрый ответ.
//
// Если все upstream-ы не ответили — запрос тихо отбрасывается (клиент
// получит таймаут на своей стороне). На каждый запрос пишем лог с
// qid (transaction ID из DNS-заголовка) для отладки.
func (p *dnsProxy) handleQuery(query []byte, clientAddr net.Addr) {
	qid := uint16(0)
	if len(query) >= 2 {
		qid = uint16(query[0])<<8 | uint16(query[1])
	}

	type result struct {
		server string
		resp   []byte
		err    error
	}
	// Буферизованный канал: даже если мы уже вернули ответ, фоновые
	// горутины смогут отправить свой результат и завершиться.
	ch := make(chan result, len(p.upstream))

	for _, server := range p.upstream {
		p.forwards.Add(1)
		go func(server string) {
			resp, err := p.forward(query, server)
			ch <- result{server, resp, err}
		}(server)
	}

	pending := len(p.upstream)
	timeoutCh := time.After(p.timeout)
	for pending > 0 {
		select {
		case r := <-ch:
			pending--
			if r.err == nil {
				p.responses.Add(1)
				p.mu.Lock()
				listener := p.listener
				p.mu.Unlock()
				if listener != nil {
					if _, werr := listener.WriteTo(r.resp, clientAddr); werr != nil {
						log.Printf("[DNS] qid=0x%04x ответ %d байт от %s, но WriteTo %s ошибка: %v", qid, len(r.resp), r.server, clientAddr, werr)
					}
				}
				return
			}
			log.Printf("[DNS] qid=0x%04x upstream=%s ошибка: %v", qid, r.server, r.err)
		case <-timeoutCh:
			log.Printf("[DNS] qid=0x%04x общий таймаут %v (осталось %d upstream-ов в полёте)", qid, p.timeout, pending)
			p.failures.Add(1)
			return
		}
	}
	p.failures.Add(1)
}

// forward отправляет DNS-запрос на один upstream UDP-сервер и ждёт ответ.
// Если задан sourceIP — биндим исходящий сокет на него, чтобы пакет
// гарантированно ушёл через WireGuard-интерфейс (default route), а не
// через локальный Ethernet (где 8.8.8.8 может быть заблокирован ISP).
func (p *dnsProxy) forward(query []byte, server string) ([]byte, error) {
	rAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(server, "53"))
	if err != nil {
		return nil, err
	}
	var upstream *net.UDPConn
	if p.sourceIP != "" {
		lAddr := &net.UDPAddr{IP: net.ParseIP(p.sourceIP), Port: 0}
		upstream, err = net.DialUDP("udp", lAddr, rAddr)
		if err != nil {
			// WDTT-интерфейс мог быть ещё не готов при первом запросе после старта.
			// Фоллбек на авто-выбор source IP (OS подберёт по routing table).
			upstream, err = net.DialUDP("udp", nil, rAddr)
		}
	} else {
		upstream, err = net.DialUDP("udp", nil, rAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", server, err)
	}
	defer upstream.Close()

	upstream.SetDeadline(time.Now().Add(p.timeout))
	if _, err := upstream.Write(query); err != nil {
		return nil, fmt.Errorf("write %s: %w", server, err)
	}

	resp := make([]byte, 4096)
	n, err := upstream.Read(resp)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", server, err)
	}
	return resp[:n], nil
}
