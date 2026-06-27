package core

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ─── Persistence ─────────────────────────────────────────────────────────────

// excludeDomainsPath возвращает путь к файлу exclude_domains.json.
func excludeDomainsPath(configDir string) string {
	return filepath.Join(configDir, "exclude_domains.json")
}

// LoadExcludeDomains читает список исключённых доменов из файла.
// Если файла нет — возвращает (nil, nil).
func LoadExcludeDomains(configDir string) ([]string, error) {
	data, err := os.ReadFile(excludeDomainsPath(configDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var domains []string
	if err := json.Unmarshal(data, &domains); err != nil {
		return nil, err
	}
	return domains, nil
}

// SaveExcludeDomains записывает список исключённых доменов в файл.
func SaveExcludeDomains(configDir string, domains []string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	// Валидация перед записью — не сохраняем мусор.
	clean := make([]string, 0, len(domains))
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if d == "" {
			continue
		}
		clean = append(clean, d)
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(excludeDomainsPath(configDir), data, 0644)
}

// ─── Pattern matching ────────────────────────────────────────────────────────

// MatchesDomain проверяет, матчит ли hostname паттерн.
// Паттерн может быть:
//   - Точный: "example.com" → матчит только "example.com"
//   - Wildcard: "*.example.com" → матчит "foo.example.com", "a.b.example.com"
//     и сам базовый домен "example.com"
func MatchesDomain(pattern, hostname string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	hostname = strings.ToLower(strings.TrimSpace(hostname))

	if pattern == "" || hostname == "" {
		return false
	}

	if !strings.HasPrefix(pattern, "*.") {
		return pattern == hostname
	}

	// Wildcard: *.example.com → suffix=".example.com", base="example.com"
	suffix := pattern[1:] // ".example.com"
	baseDomain := pattern[2:]
	return hostname == baseDomain || strings.HasSuffix(hostname, suffix)
}

// ─── Domain excluder ─────────────────────────────────────────────────────────

// domainExcluder управляет исключениями по доменам (IP-уровень).
//
// Принцип работы:
//  1. Резолвит исключённые домены через DNS-серверы (те же, что у DNS-прокси:
//     8.8.8.8, 1.1.1.1 и т.д. — через туннель).
//  2. Для каждого резолвнутого IP добавляет /32 маршрут через оригинальный
//     шлюз (не через туннель).
//  3. Windows routing всегда выбирает более специфичный маршрут, поэтому
//     /32 побеждает default route 0.0.0.0/0 через WireGuard.
//
// Это работает даже если приложение использует DoH/DoT/хардкод IP —
// сетевой уровень всегда выберет прямой маршрут.
//
// Wildcard-паттерны ("*.example.com") не резолвятся заранее (бесконечное
// число поддоменов) — обрабатываются on-demand при DNS-запросе через
// callback из DNS-прокси.
type domainExcluder struct {
	mu       sync.Mutex
	patterns []string // все паттерны (включая wildcards)
	// resolved: domain → []net.IP (кэш резолвов, для точных и wildcard-поддоменов)
	resolved map[string][]net.IP
	// ip → pattern (для teardown: какой маршрут какой паттерн добавил)
	routeToPattern map[string]string
	// pattern → set of IP strings (wildcard: какие IPs зарезолвлены для этого паттерна)
	wildcardIPs map[string]map[string]bool

	gateway    string   // оригинальный шлюз (для /32 маршрутов)
	iface      string   // оригинальный интерфейс (для логов)
	dnsServers []string // DNS-серверы для резолва (обычно upstream DNS-прокси)
	sourceIP   string   // source IP для DNS-запросов (IP туннеля — чтобы шло через WG)

	stopCh chan struct{}
	doneCh chan struct{}
}

func newDomainExcluder(patterns []string, gateway, iface string, dnsServers []string, sourceIP string) *domainExcluder {
	normalized := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			normalized = append(normalized, p)
		}
	}
	return &domainExcluder{
		patterns:       normalized,
		resolved:       make(map[string][]net.IP),
		routeToPattern: make(map[string]string),
		wildcardIPs:    make(map[string]map[string]bool),
		gateway:        gateway,
		iface:          iface,
		dnsServers:     dnsServers,
		sourceIP:       sourceIP,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
}

// Start запускает периодический refresh (каждые 5 минут) для точных доменов.
// Wildcard-паттерны обрабатываются on-demand при DNS-запросах.
func (de *domainExcluder) Start() {
	if len(de.patterns) == 0 {
		close(de.doneCh)
		return
	}

	exactCount := 0
	for _, p := range de.patterns {
		if !strings.HasPrefix(p, "*.") {
			exactCount++
		}
	}

	log.Printf("[DOMEXCL] Запущен: %d паттернов (%d точных + %d wildcard), gateway=%s, DNS=%v",
		len(de.patterns), exactCount, len(de.patterns)-exactCount, de.gateway, de.dnsServers)

	if exactCount > 0 {
		de.refreshExact()
	}

	go de.loop()
}

// Stop останавливает refresh loop и удаляет все добавленные маршруты.
func (de *domainExcluder) Stop() {
	if len(de.patterns) == 0 {
		return
	}
	close(de.stopCh)
	<-de.doneCh

	de.mu.Lock()
	defer de.mu.Unlock()

	removed := 0
	for ip := range de.routeToPattern {
		runRouteDelete(ip + "/32")
		removed++
	}
	de.routeToPattern = make(map[string]string)
	de.resolved = make(map[string][]net.IP)
	de.wildcardIPs = make(map[string]map[string]bool)

	log.Printf("[DOMEXCL] Остановлен, удалено %d /32 маршрутов", removed)
}

// IsExcluded проверяет, матчит ли hostname любой из паттернов.
func (de *domainExcluder) IsExcluded(hostname string) bool {
	if hostname == "" {
		return false
	}
	de.mu.Lock()
	defer de.mu.Unlock()
	for _, p := range de.patterns {
		if MatchesDomain(p, hostname) {
			return true
		}
	}
	return false
}

// MatchingPattern возвращает паттерн, который матчит hostname (для логов).
func (de *domainExcluder) MatchingPattern(hostname string) string {
	if hostname == "" {
		return ""
	}
	de.mu.Lock()
	defer de.mu.Unlock()
	for _, p := range de.patterns {
		if MatchesDomain(p, hostname) {
			return p
		}
	}
	return ""
}

// HandleExcludedDNS резолвит исключённый домен через DNS-серверы и добавляет
// /32 маршруты для полученных IP. Вызывается из DNS-прокси.
//
// Использует кэш: если домен уже зарезолвлен — возвращает сохранённые IP
// без повторного DNS-запроса и без логирования. Это критично, потому что
// приложения (Steam, браузеры) делают десятки DNS-запросов к одному домену
// в минуту — без кэша были бы десятки логов и route add/delete в секунду.
func (de *domainExcluder) HandleExcludedDNS(domain string) []net.IP {
	if de.gateway == "" {
		return nil
	}
	if len(de.dnsServers) == 0 {
		return nil
	}

	pattern := de.MatchingPattern(domain)
	if pattern == "" {
		return nil
	}

	// Cache hit — возвращаем уже зарезолвленные IP без DNS-запроса.
	de.mu.Lock()
	if ips, ok := de.resolved[domain]; ok && len(ips) > 0 {
		de.mu.Unlock()
		return ips
	}
	de.mu.Unlock()

	// Cache miss — резолвим и добавляем маршруты.
	ips, err := de.resolveAndRoute(domain, pattern)
	if err != nil {
		log.Printf("[DOMEXCL] %s: ошибка резолва: %v", domain, err)
		return nil
	}
	return ips
}

// resolveAndRoute резолвит домен и добавляет /32 маршруты для новых IP.
// Не удаляет существующие маршруты — это делает refreshExact() при смене IP,
// и Stop() при teardown. Пропускает IP, маршруты для которых уже добавлены,
// и не логирует дубликаты (иначе был бы спам при каждом DNS-запросе).
func (de *domainExcluder) resolveAndRoute(domain, pattern string) ([]net.IP, error) {
	var allIPs []net.IP
	var lastErr error

	for _, dns := range de.dnsServers {
		ips, err := resolveDomainA(domain, dns, de.sourceIP)
		if err != nil {
			lastErr = err
			continue
		}
		allIPs = append(allIPs, ips...)
		if len(ips) > 0 {
			break // используем первый успешный DNS
		}
	}

	if len(allIPs) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("нет IP для %s", domain)
	}

	de.mu.Lock()
	defer de.mu.Unlock()

	// Кэшируем результат резолва для этого домена (точные и wildcard-поддомены).
	de.resolved[domain] = allIPs
	if strings.HasPrefix(pattern, "*.") {
		if de.wildcardIPs[pattern] == nil {
			de.wildcardIPs[pattern] = make(map[string]bool)
		}
	}

	for _, ip := range allIPs {
		ipStr := normalizeIP(ip)
		if ipStr == "" {
			continue
		}
		if strings.HasPrefix(pattern, "*.") {
			de.wildcardIPs[pattern][ipStr] = true
		}
		if _, exists := de.routeToPattern[ipStr]; exists {
			continue // маршрут уже добавлен — тишина (не логируем)
		}
		if runRouteAdd(ipStr+"/32", de.gateway) {
			de.routeToPattern[ipStr] = pattern
			log.Printf("[DOMEXCL] %s → %s/32 через %s (паттерн: %s)", domain, ipStr, de.gateway, pattern)
		}
	}

	return allIPs, nil
}

func (de *domainExcluder) refreshExact() {
	de.mu.Lock()
	patterns := make([]string, len(de.patterns))
	copy(patterns, de.patterns)
	de.mu.Unlock()

	for _, p := range patterns {
		if strings.HasPrefix(p, "*.") {
			continue // wildcard — on-demand
		}

		// Запоминаем старые IP ДО резолва, чтобы потом найти устаревшие.
		de.mu.Lock()
		oldIPs := de.resolved[p]
		de.mu.Unlock()

		newIPs, err := de.resolveAndRoute(p, p)
		if err != nil {
			log.Printf("[DOMEXCL] Refresh %s: %v", p, err)
			continue
		}

		// Удаляем /32 маршруты для IP, которых больше нет в новом резолве.
		de.mu.Lock()
		newIPSet := make(map[string]bool, len(newIPs))
		for _, ip := range newIPs {
			if s := normalizeIP(ip); s != "" {
				newIPSet[s] = true
			}
		}
		for _, oldIP := range oldIPs {
			ipStr := normalizeIP(oldIP)
			if ipStr == "" || newIPSet[ipStr] {
				continue // IP всё ещё актуален
			}
			if de.routeToPattern[ipStr] == p {
				runRouteDelete(ipStr + "/32")
				delete(de.routeToPattern, ipStr)
				log.Printf("[DOMEXCL] Refresh %s: удалён устаревший маршрут %s/32", p, ipStr)
			}
		}
		de.mu.Unlock()
	}
}

func (de *domainExcluder) loop() {
	defer close(de.doneCh)
	// TTL большинства DNS-записей = 300с (5 минут). Рефрешим каждые 5 минут,
	// чтобы успеть подхватить смену IP до истечения кэша у приложений.
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()

	for {
		select {
		case <-de.stopCh:
			return
		case <-t.C:
			de.refreshExact()
		}
	}
}

// normalizeIP конвертирует IP в строковый вид (IPv4 без зоны).
func normalizeIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

// ─── DNS resolution ──────────────────────────────────────────────────────────

// resolveDomainA резолвит домен через указанный DNS-сервер, возвращает A-записи.
// Если задан sourceIP — биндит исходящий сокет на него, чтобы запрос пошёл
// через оригинальный интерфейс (не через туннель).
func resolveDomainA(domain, dnsServer, sourceIP string) ([]net.IP, error) {
	rAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(dnsServer, "53"))
	if err != nil {
		return nil, err
	}

	var conn *net.UDPConn
	if sourceIP != "" {
		lAddr := &net.UDPAddr{IP: net.ParseIP(sourceIP), Port: 0}
		conn, err = net.DialUDP("udp", lAddr, rAddr)
		if err != nil {
			conn, err = net.DialUDP("udp", nil, rAddr) // fallback без source IP
		}
	} else {
		conn, err = net.DialUDP("udp", nil, rAddr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", dnsServer, err)
	}
	defer conn.Close()

	// Собираем минимальный DNS-запрос (A-запись, рекурсия).
	query := buildDNSQuery(domain)
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	resp := make([]byte, 4096)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	return parseDNSARecords(resp[:n])
}

// buildDNSQuery собирает DNS-запрос для A-записи с recursion desired.
func buildDNSQuery(domain string) []byte {
	buf := make([]byte, 0, 64)

	// Header: ID=0, flags=0x0100 (RD=1), QDCOUNT=1
	buf = append(buf, 0x00, 0x00) // ID
	buf = append(buf, 0x01, 0x00) // flags: RD=1, opcode=0
	buf = append(buf, 0x00, 0x01) // QDCOUNT=1
	buf = append(buf, 0x00, 0x00) // ANCOUNT
	buf = append(buf, 0x00, 0x00) // NSCOUNT
	buf = append(buf, 0x00, 0x00) // ARCOUNT

	// QNAME: domain split by dots, each label prefixed by length
	parts := strings.Split(domain, ".")
	for _, part := range parts {
		if part == "" {
			continue
		}
		buf = append(buf, byte(len(part)))
		buf = append(buf, []byte(part)...)
	}
	buf = append(buf, 0x00) // terminator

	// QTYPE=A(1), QCLASS=IN(1)
	buf = append(buf, 0x00, 0x01)
	buf = append(buf, 0x00, 0x01)

	return buf
}

// parseDNSARecords извлекает IPv4-адреса из A-записей в DNS-ответе.
// Простой парсер: header → question → answer RRs.
func parseDNSARecords(resp []byte) ([]net.IP, error) {
	if len(resp) < 12 {
		return nil, fmt.Errorf("ответ слишком короткий: %d байт", len(resp))
	}

	// Проверяем RCODE
	rcode := resp[3] & 0x0F
	if rcode != 0 {
		return nil, fmt.Errorf("DNS RCODE=%d", rcode)
	}

	ancount := binary.BigEndian.Uint16(resp[6:8])
	if ancount == 0 {
		return nil, nil // нет answer записей
	}

	// Пропускаем Question Section.
	// QDCOUNT в [4:6].
	qdcount := binary.BigEndian.Uint16(resp[4:6])
	offset := 12
	for i := 0; i < int(qdcount); i++ {
		offset = skipDNSName(resp, offset)
		if offset < 0 || offset+4 > len(resp) {
			return nil, fmt.Errorf("битый question section")
		}
		offset += 4 // QTYPE + QCLASS
	}

	var ips []net.IP
	for i := 0; i < int(ancount); i++ {
		offset = skipDNSName(resp, offset)
		if offset < 0 || offset+10 > len(resp) {
			break
		}
		// TYPE(2) + CLASS(2) + TTL(4) + RDLENGTH(2) + RDATA
		rrType := binary.BigEndian.Uint16(resp[offset : offset+2])
		offset += 2 // TYPE
		offset += 2 // CLASS
		offset += 4 // TTL
		rdlength := binary.BigEndian.Uint16(resp[offset : offset+2])
		offset += 2
		if offset+int(rdlength) > len(resp) {
			break
		}

		if rrType == 1 && rdlength == 4 { // A record, IPv4
			ip := net.IP(resp[offset : offset+4])
			ips = append(ips, ip)
		}
		offset += int(rdlength)
	}

	return ips, nil
}

// skipDNSName пропускает DNS-имя в ответе (с поддержкой compression).
// Возвращает новый offset или -1 при ошибке.
func skipDNSName(resp []byte, offset int) int {
	visited := 0 // защита от циклов в compression
	for offset < len(resp) {
		l := int(resp[offset])
		if l == 0 {
			return offset + 1
		}
		if l&0xC0 == 0xC0 {
			// Pointer (compression): 2 bytes total.
			if offset+2 > len(resp) {
				return -1
			}
			return offset + 2
		}
		if l&0xC0 != 0 {
			return -1 // reserved
		}
		offset += 1 + l
		visited++
		if visited > 64 {
			return -1 // слишком длинное имя
		}
	}
	return -1
}
