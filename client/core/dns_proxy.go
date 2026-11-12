package core

import (
	"encoding/binary"
	"fmt"
	"io"
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

// Глобальные счётчики DNS-прокси для дашборда: переживают пересоздание
// прокси при soft restart и смене сети (per-instance счётчики обнулялись бы).
var (
	dnsTotalResponses atomic.Uint64
	dnsTotalFailures  atomic.Uint64
)

// countResponse/countFailure обновляют и локальные, и глобальные счётчики.
func (p *dnsProxy) countResponse() {
	p.responses.Add(1)
	dnsTotalResponses.Add(1)
}

func (p *dnsProxy) countFailure() {
	p.failures.Add(1)
	dnsTotalFailures.Add(1)
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
		// Копия обязательна: buf переиспользуется следующим ReadFrom, а
		// handleQuery работает асинхронно. Без копии запрос перетирался
		// чужим, и ответ upstream-а не совпадал по qid («qid mismatch»).
		query := make([]byte, n)
		copy(query, buf[:n])
		go p.handleQuery(query, addr)
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
				resp := buildDNSResponseForQuery(query, ips)
				if resp != nil {
					p.countResponse()
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
				p.countResponse()
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
			p.countFailure()
			return
		}
	}

	// Все UDP-upstream-ы отказали. Последняя попытка — TCP к первому
	// upstream-у: провайдеры часто режут UDP/53 (в том числе через TURN),
	// но TCP/53 пропускают.
	if len(p.upstream) > 0 {
		resp, tcpErr := p.forwardTCP(query, p.upstream[0])
		if tcpErr == nil {
			p.countResponse()
			if listener != nil {
				_, _ = listener.WriteTo(resp, clientAddr)
			}
			return
		}
		log.Printf("[DNS] qid=0x%04x TCP-fallback %s не удался: %v", qid, p.upstream[0], tcpErr)
	}
	p.countFailure()
}

// dnsMessageID возвращает transaction ID из DNS-заголовка.
func dnsMessageID(msg []byte) uint16 {
	if len(msg) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(msg[:2])
}

// dnsIsTruncated — выставлен ли в заголовке флаг TC (ответ не влез в UDP).
func dnsIsTruncated(msg []byte) bool {
	return len(msg) >= 4 && msg[2]&0x02 != 0
}

// forward отправляет DNS-запрос на один upstream UDP-сервер и ждёт ответ.
// Если задан sourceIP — биндим исходящий сокет на него, чтобы пакет
// гарантированно ушёл через WireGuard-интерфейс (default route), а не
// через локальный Ethernet (где 8.8.8.8 может быть заблокирован ISP).
//
// Ответ проверяется по qid: connected UDP-сокет отсекает чужие адреса, но
// не защищает от подмены/мусора внутри одного потока (DPI, битый NAT).
// При truncation (TC=1) или несовпадении qid повторяем запрос по TCP.
func (p *dnsProxy) forward(query []byte, server string) ([]byte, error) {
	resp, err := p.forwardUDP(query, server)
	if err != nil {
		return nil, err
	}
	if dnsMessageID(resp) != dnsMessageID(query) {
		tcpResp, tcpErr := p.forwardTCP(query, server)
		if tcpErr != nil {
			return nil, fmt.Errorf("udp %s: qid mismatch (tcp: %w)", server, tcpErr)
		}
		return tcpResp, nil
	}
	if dnsIsTruncated(resp) {
		tcpResp, tcpErr := p.forwardTCP(query, server)
		if tcpErr != nil {
			return nil, fmt.Errorf("udp %s: truncated (tcp: %w)", server, tcpErr)
		}
		return tcpResp, nil
	}
	return resp, nil
}

// forwardUDP — обычная UDP-пересылка на upstream.
func (p *dnsProxy) forwardUDP(query []byte, server string) ([]byte, error) {
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

// forwardTCP — DNS-over-TCP (RFC 1035 §4.2.2): 2-байтовый префикс длины.
// Используется как fallback при truncated-ответе, несовпадении qid и когда
// все UDP-upstream-ы молчат (провайдеры часто режут UDP/53, но не TCP/53).
func (p *dnsProxy) forwardTCP(query []byte, server string) ([]byte, error) {
	if len(query) > 0xFFFF {
		return nil, fmt.Errorf("tcp %s: запрос слишком длинный (%d)", server, len(query))
	}
	addr := net.JoinHostPort(server, "53")

	dialer := net.Dialer{Timeout: p.timeout}
	if p.sourceIP != "" {
		if ip := net.ParseIP(p.sourceIP); ip != nil {
			dialer.LocalAddr = &net.TCPAddr{IP: ip}
		}
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil && p.sourceIP != "" {
		// Интерфейс мог быть ещё не готов — пробуем без привязки source.
		conn, err = net.DialTimeout("tcp", addr, p.timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", server, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(p.timeout))

	msg := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(msg[:2], uint16(len(query)))
	copy(msg[2:], query)
	if _, err := conn.Write(msg); err != nil {
		return nil, fmt.Errorf("tcp write %s: %w", server, err)
	}

	var lenBuf [2]byte
	if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("tcp read len %s: %w", server, err)
	}
	respLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if respLen < 12 {
		return nil, fmt.Errorf("tcp %s: короткий ответ (%d байт)", server, respLen)
	}
	resp := make([]byte, respLen)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("tcp read body %s: %w", server, err)
	}
	if dnsMessageID(resp) != dnsMessageID(query) {
		return nil, fmt.Errorf("tcp %s: qid mismatch", server)
	}
	return resp, nil
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

// buildDNSResponseForQuery собирает минимальный DNS-ответ на основе исходного
// запроса. Используется для исключённых доменов, резолвленных через
// оригинальный DNS — чтобы клиент получил ответ сразу, без обращения к
// upstream через туннель.
//
// Тип записи берётся из QTYPE запроса:
//   - A (1)    → только IPv4-адреса;
//   - AAAA (28) → только IPv6-адреса (To16, включая IPv4-mapped → ::ffff:a.b.c.d);
//   - прочее   → nil (пусть запрос уйдёт на upstream как обычно).
//
// Раньше функция всегда строила A-записи и игнорировала QTYPE: на AAAA-запрос
// клиент получал ответ с A-записями (некорректно), а IPv6-адреса молча
// терялись. Теперь AAAA отвечается AAAA-записями, и IPv6 работает, когда
// маршруты ::/1 + 8000::/1 ведут в туннель.
//
// Копирует ID из запроса, выставляет флаги QR=1 (response), AA=1, RD=1, RA=1,
// RCODE=0. QDCOUNT=1 (echo question), ANCOUNT=N (число подходящих IP).
func buildDNSResponseForQuery(query []byte, ips []net.IP) []byte {
	if len(query) < 13 || len(ips) == 0 {
		return nil
	}

	// Question Section: QNAME + QTYPE + QCLASS. qEnd — смещение сразу после
	// QCLASS, qType — тип запроса.
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
	qType := binary.BigEndian.Uint16(query[qEnd : qEnd+2])
	if qType != 1 && qType != 28 {
		return nil
	}

	// Header: 12 bytes
	resp := make([]byte, 12, 64+len(ips)*20)
	// ID — копия из запроса
	resp[0] = query[0]
	resp[1] = query[1]
	// Flags: QR=1, RD=1, RA=1, RCODE=0 → 0x8180
	resp[2] = 0x81
	resp[3] = 0x80
	// QDCOUNT = 1
	resp[4] = 0x00
	resp[5] = 0x01
	// ANCOUNT посчитаем после фильтрации адресов по типу.
	resp[6] = 0x00
	resp[7] = 0x00
	// NSCOUNT, ARCOUNT = 0
	resp[8] = 0x00
	resp[9] = 0x00
	resp[10] = 0x00
	resp[11] = 0x00

	resp = append(resp, query[12:qEnd+4]...)

	// Answer Section: по одной записи на каждый подходящий IP.
	var answers uint16
	for _, ip := range ips {
		var rdata []byte
		var rdlen uint16
		if qType == 1 {
			v4 := ip.To4()
			if v4 == nil {
				continue // для A-записи IPv6 не подходит
			}
			rdata, rdlen = v4, 4
		} else {
			if ip.To4() != nil {
				continue // IPv4-адрес не подходит для AAAA-записи
			}
			v6 := ip.To16()
			if v6 == nil {
				continue
			}
			rdata, rdlen = v6, 16
		}
		// NAME: pointer на QNAME в offset 12 → 0xC00C
		resp = append(resp, 0xC0, 0x0C)
		// TYPE = A (1) или AAAA (28)
		resp = append(resp, byte(qType>>8), byte(qType))
		// CLASS = IN (1)
		resp = append(resp, 0x00, 0x01)
		// TTL = 300 (5 минут)
		resp = append(resp, 0x00, 0x00, 0x01, 0x2C)
		// RDLENGTH
		resp = append(resp, byte(rdlen>>8), byte(rdlen))
		// RDATA
		resp = append(resp, rdata...)
		answers++
	}
	if answers == 0 {
		// Для AAAA без IPv6-адресов отдаём пустой NOERROR, а не nil:
		// иначе запрос уйдёт на upstream и клиент получит AAAA, трафик по
		// которому всё равно уйдёт в IPv6-blackhole (C9). Пустой ответ
		// заставляет клиента сразу использовать A.
		if qType == 28 {
			return resp
		}
		return nil
	}
	resp[6] = byte(answers >> 8)
	resp[7] = byte(answers & 0xFF)

	return resp
}

