package core

import (
	"fmt"
	"log"
	"net"
	"strings"
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

	// onExcludedDomain вызывается для DNS-запросов к исключённым доменам
	// (перед отправкой на upstream через туннель). Должен вернуть список
	// резолвнутых IP — excluder добавит /32 маршруты для них.
	onExcludedDomain func(hostname string) []net.IP
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
		// Таймаут 10s: DNS-запросы идут через TURN-туннель (2× round-trip),
		// при перегрузке relay-ов RTT может достигать 1-2s. 5s было слишком мало.
		timeout: 10 * time.Second,
	}
}

// SetExcludedDomainHandler устанавливает callback, вызываемый для DNS-запросов
// к исключённым доменам (до отправки на upstream). Используется для добавления
// /32 маршрутов через оригинальный шлюз.
func (p *dnsProxy) SetExcludedDomainHandler(fn func(hostname string) []net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onExcludedDomain = fn
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
//
// Для исключённых доменов (wildcard-паттерны) вызывается onExcludedDomain
// callback: excluder резолвит поддомен через upstream DNS (через туннель),
// добавляет /32 маршрут через оригинальный шлюз, и возвращает IP.
// Мы строим DNS-ответ из этих IP, чтобы клиент получил ответ сразу —
// последующий трафик пойдёт через /32 маршрут (мимо туннеля).
func (p *dnsProxy) handleQuery(query []byte, clientAddr net.Addr) {
	qid := uint16(0)
	if len(query) >= 2 {
		qid = uint16(query[0])<<8 | uint16(query[1])
	}

	// Проверяем, матчит ли домен wildcard-паттерн (или любой другой).
	// Для точных паттернов excluder уже пререзолвил домен при Start() —
	// /32 маршруты добавлены, можно просто форвардить запрос как обычно.
	p.mu.Lock()
	handler := p.onExcludedDomain
	listener := p.listener
	p.mu.Unlock()

	if handler != nil {
		if domain := extractDNSQueryDomain(query); domain != "" {
			ips := handler(domain)
			if len(ips) > 0 {
				// Домен матчит исключение (wildcard) и зарезолвлен — строим
				// ответ вручную из полученных IP, чтобы клиент получил
				// ответ сразу. /32 маршрут уже добавлен excluder-ом.
				resp := buildDNSResponseWithA(query, ips)
				if resp != nil {
					p.responses.Add(1)
					if listener != nil {
						listener.WriteTo(resp, clientAddr)
					}
					return
				}
			}
		}
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

// extractDNSQueryDomain извлекает доменное имя из DNS-запроса (QNAME).
// Поддерживает стандартный формат с length-prefixed labels.
// Возвращает пустую строку при ошибке парсинга.
func extractDNSQueryDomain(query []byte) string {
	if len(query) < 13 {
		return ""
	}
	// Header = 12 bytes, QNAME начинается с offset 12.
	offset := 12
	var labels []string
	visited := 0
	for offset < len(query) {
		l := int(query[offset])
		if l == 0 {
			break
		}
		if l&0xC0 != 0 {
			// Compression pointer — не ожидаем в query, но защищаемся.
			return ""
		}
		offset++
		if offset+l > len(query) {
			return ""
		}
		labels = append(labels, string(query[offset:offset+l]))
		offset += l
		visited++
		if visited > 64 {
			return "" // защита от слишком длинных имён
		}
	}
	return strings.Join(labels, ".")
}

// buildDNSResponseWithA собирает минимальный DNS-ответ с A-записями
// на основе исходного запроса. Используется для исключённых доменов,
// резолвленных через оригинальный DNS — чтобы клиент получил ответ сразу,
// без обращения к upstream через туннель.
//
// Копирует ID из запроса, выставляет флаги QR=1 (response), AA=1, RD=1, RA=1,
// RCODE=0. QDCOUNT=1 (echo question), ANCOUNT=N (число IP).
func buildDNSResponseWithA(query []byte, ips []net.IP) []byte {
	if len(query) < 13 || len(ips) == 0 {
		return nil
	}

	// Header: 12 bytes
	resp := make([]byte, 12, 64+len(ips)*16)
	// ID — копия из запроса
	resp[0] = query[0]
	resp[1] = query[1]
	// Flags: QR=1, RD=1, RA=1, RCODE=0 → 0x8180
	resp[2] = 0x81
	resp[3] = 0x80
	// QDCOUNT = 1
	resp[4] = 0x00
	resp[5] = 0x01
	// ANCOUNT = N
	ancount := uint16(len(ips))
	resp[6] = byte(ancount >> 8)
	resp[7] = byte(ancount & 0xFF)
	// NSCOUNT, ARCOUNT = 0
	resp[8] = 0x00
	resp[9] = 0x00
	resp[10] = 0x00
	resp[11] = 0x00

	// Question Section: копируем QNAME + QTYPE + QCLASS из запроса.
	qEnd := 12
	for qEnd < len(query) {
		l := int(query[qEnd])
		if l == 0 {
			qEnd++ // включая нулевой терминатор
			break
		}
		if l&0xC0 != 0 {
			return nil
		}
		qEnd += 1 + l
	}
	if qEnd+4 > len(query) {
		return nil
	}
	resp = append(resp, query[12:qEnd+4]...)

	// Answer Section: по одной A-записи на каждый IP.
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			continue // пропускаем IPv6 (для A-записи не подходит)
		}
		// NAME: pointer на QNAME в offset 12 → 0xC00C
		resp = append(resp, 0xC0, 0x0C)
		// TYPE = A (1)
		resp = append(resp, 0x00, 0x01)
		// CLASS = IN (1)
		resp = append(resp, 0x00, 0x01)
		// TTL = 300 (5 минут)
		resp = append(resp, 0x00, 0x00, 0x01, 0x2C)
		// RDLENGTH = 4
		resp = append(resp, 0x00, 0x04)
		// RDATA = IPv4 address
		resp = append(resp, v4...)
	}

	return resp
}

