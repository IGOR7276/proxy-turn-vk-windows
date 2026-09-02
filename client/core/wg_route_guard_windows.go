//go:build windows
// +build windows

package core

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Route guard — страховка маршрутов-исключений при смене сети.
//
// Проблема: SetupWindowsWireGuard один раз определяет «физический» шлюз и
// вешает на него /32-маршруты к TURN-серверам, VK CIDR и подмену DNS.
// Если пользователь переключился с Wi-Fi на Ethernet, сменил точку доступа,
// вернулся из сна с другим DHCP-шлюзом или интерфейс просто моргнул (Windows
// при down/up сносит маршруты этого интерфейса) — исключения указывают в
// пустоту, трафик к TURN уходит в WG default route (0.0.0.0/0), а WG шлёт его
// обратно в диспетчер → воркеры никогда не переподключатся.
//
// Решение: пока туннель поднят, каждые routeGuardInterval опрашиваем таблицу
// адаптеров (GetAdaptersAddresses — без запуска процессов) и, если шлюз/
// интерфейс сменился или снова появился после пропажи, переставляем все
// исключения на актуальный шлюз.

const routeGuardInterval = 3 * time.Second

// wgRuntime — состояние поднятого туннеля, нужное route guard'у.
type wgRuntime struct {
	mu          sync.Mutex
	ifaceName   string          // имя WG-адаптера (исключается из поиска шлюза)
	gateway     string          // текущий физический шлюз ("" = не найден)
	iface       string          // интерфейс физического шлюза
	turnRoutes  map[string]bool // TURN IP → /32 добавлен через текущий шлюз
	dnsOverride bool            // подменяем ли системный DNS на 127.0.0.1
	stop        chan struct{}
	done        chan struct{}
}

var (
	activeWGMu sync.Mutex
	activeWG   *wgRuntime
)

// detectPhysicalGateway возвращает IPv4-шлюз по умолчанию и имя интерфейса,
// пропуская наш WG-адаптер, loopback и адаптеры без реального next-hop.
// При нескольких кандидатах берётся интерфейс с наименьшей метрикой.
func detectPhysicalGateway(excludeIface string) (gateway, ifaceName string) {
	const flags = windows.GAA_FLAG_INCLUDE_GATEWAYS |
		windows.GAA_FLAG_SKIP_ANYCAST |
		windows.GAA_FLAG_SKIP_MULTICAST |
		windows.GAA_FLAG_SKIP_DNS_SERVER

	size := uint32(16 * 1024)
	var buf []byte
	for attempt := 0; attempt < 4; attempt++ {
		buf = make([]byte, size)
		err := windows.GetAdaptersAddresses(windows.AF_INET, flags, 0,
			(*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])), &size)
		if err == nil {
			break
		}
		if err != windows.ERROR_BUFFER_OVERFLOW {
			return "", ""
		}
	}
	if len(buf) == 0 {
		return "", ""
	}

	bestMetric := ^uint32(0)
	for a := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0])); a != nil; a = a.Next {
		if a.OperStatus != windows.IfOperStatusUp {
			continue
		}
		if a.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
			continue
		}
		name := windows.UTF16PtrToString(a.FriendlyName)
		if name == "" || name == excludeIface {
			continue
		}
		if a.FirstUnicastAddress == nil {
			continue
		}
		var gw string
		for g := a.FirstGatewayAddress; g != nil; g = g.Next {
			ip := g.Address.IP()
			if ip == nil || ip.IsUnspecified() || ip.To4() == nil {
				continue
			}
			gw = ip.String()
			break
		}
		if gw == "" {
			continue
		}
		if a.Ipv4Metric < bestMetric {
			bestMetric = a.Ipv4Metric
			gateway, ifaceName = gw, name
		}
	}
	return gateway, ifaceName
}

// startRouteGuard запускает мониторинг после успешного поднятия WG.
func startRouteGuard(rt *wgRuntime) {
	activeWGMu.Lock()
	activeWG = rt
	activeWGMu.Unlock()
	rt.stop = make(chan struct{})
	rt.done = make(chan struct{})
	go rt.loop()
}

// stopRouteGuard останавливает мониторинг (вызывается из teardown).
func stopRouteGuard() {
	activeWGMu.Lock()
	rt := activeWG
	activeWG = nil
	activeWGMu.Unlock()
	if rt == nil {
		return
	}
	close(rt.stop)
	<-rt.done
}

func (rt *wgRuntime) loop() {
	defer close(rt.done)
	t := time.NewTicker(routeGuardInterval)
	defer t.Stop()
	warnedAbsent := false
	for {
		select {
		case <-rt.stop:
			return
		case <-t.C:
		}

		gw, iface := detectPhysicalGateway(rt.ifaceName)

		rt.mu.Lock()
		curGw, curIface := rt.gateway, rt.iface
		rt.mu.Unlock()

		if gw == "" {
			if curGw != "" {
				log.Printf("[ROUTE-GUARD] Физический шлюз пропал (%s via %s) — ждём сеть", curGw, curIface)
				rt.mu.Lock()
				rt.gateway, rt.iface = "", ""
				rt.mu.Unlock()
				warnedAbsent = true
			}
			continue
		}
		if gw == curGw && iface == curIface {
			continue
		}

		if curGw == "" && !warnedAbsent {
			log.Printf("[ROUTE-GUARD] Найден физический шлюз: %s via %s", gw, iface)
		} else {
			log.Printf("[ROUTE-GUARD] Смена сети: %s via %q → %s via %q, переставляем исключения",
				curGw, curIface, gw, iface)
		}
		warnedAbsent = false
		rt.switchGateway(curIface, gw, iface)
	}
}

// switchGateway снимает исключения со старого интерфейса и ставит на новый.
func (rt *wgRuntime) switchGateway(oldIface, gw, iface string) {
	rt.removeTurnRoutes(oldIface)
	removeExcludeRoutes()

	rt.mu.Lock()
	rt.gateway, rt.iface = gw, iface
	rt.mu.Unlock()

	rt.applyTurnRoutes()
	applyExcludeRoutes(gw, iface)

	if rt.dnsOverride {
		overrideInterfaceDNS(iface)
		_ = hiddenCmd("ipconfig", "/flushdns").Run()
	}

	dnsProxyMu.Lock()
	de := activeDomainExcluder
	dnsProxyMu.Unlock()
	if de != nil {
		de.SetGateway(gw, iface)
	}
	emitEvent(Event{Type: EventEvent, Name: "route_guard_switch", Data: fmt.Sprintf("gw=%s iface=%s", gw, iface)})
}

// applyTurnRoutes добавляет /32 на все известные TURN IP через текущий шлюз.
func (rt *wgRuntime) applyTurnRoutes() {
	rt.mu.Lock()
	gw, iface := rt.gateway, rt.iface
	rt.mu.Unlock()
	if gw == "" || iface == "" {
		return
	}
	added := 0
	for _, ip := range getTurnExcludeIPs() {
		if rt.addTurnRoute(ip, gw, iface) {
			added++
		}
	}
	if added > 0 {
		log.Printf("[ROUTE-GUARD] TURN-исключений через %s (%s): %d", gw, iface, added)
	}
}

func (rt *wgRuntime) addTurnRoute(ip net.IP, gw, iface string) bool {
	key := ip.String()
	rt.mu.Lock()
	if rt.turnRoutes[key] {
		rt.mu.Unlock()
		return false
	}
	rt.mu.Unlock()
	if err := addHostRoute(iface, gw, key); err != nil {
		log.Printf("[WG] Не удалось добавить TURN-исключение %s: %v", key, err)
		return false
	}
	rt.mu.Lock()
	rt.turnRoutes[key] = true
	rt.mu.Unlock()
	return true
}

// removeTurnRoutes удаляет ранее добавленные /32 TURN-маршруты.
func (rt *wgRuntime) removeTurnRoutes(iface string) {
	rt.mu.Lock()
	keys := make([]string, 0, len(rt.turnRoutes))
	for k := range rt.turnRoutes {
		keys = append(keys, k)
	}
	rt.turnRoutes = make(map[string]bool)
	rt.mu.Unlock()
	for _, k := range keys {
		if iface != "" {
			_ = runNetsh("interface", "ipv4", "delete", "route", k+"/32", iface, "store=active")
		}
		runRouteDelete(k)
	}
}

// applyTurnHostRoute вызывается из AddTurnExcludeIP: если туннель уже поднят,
// новый TURN IP (например, после обновления кредов) сразу выводится из-под WG.
func applyTurnHostRoute(ip net.IP) {
	activeWGMu.Lock()
	rt := activeWG
	activeWGMu.Unlock()
	if rt == nil {
		return
	}
	rt.mu.Lock()
	gw, iface := rt.gateway, rt.iface
	rt.mu.Unlock()
	if gw == "" || iface == "" {
		return
	}
	if rt.addTurnRoute(ip, gw, iface) {
		log.Printf("[ROUTE-GUARD] Новый TURN IP %s → /32 через %s (%s)", ip, gw, iface)
	}
}

// overrideInterfaceDNS подменяет DNS интерфейса на 127.0.0.1, запоминая
// оригинал для восстановления в teardown. Повторный вызов для того же
// интерфейса — no-op.
func overrideInterfaceDNS(iface string) {
	if iface == "" {
		return
	}
	dnsProxyMu.Lock()
	defer dnsProxyMu.Unlock()
	if _, done := originalDNSByIf[iface]; done {
		return
	}
	orig := getInterfaceDNS(iface)
	originalDNSByIf[iface] = orig
	if err := setInterfaceDNS(iface, []string{"127.0.0.1"}); err != nil {
		log.Printf("[DNS] Не удалось подменить системный DNS на %s: %v", iface, err)
		return
	}
	log.Printf("[DNS] Системный DNS %s: %v → 127.0.0.1 (через локальный прокси)", iface, orig)
}
