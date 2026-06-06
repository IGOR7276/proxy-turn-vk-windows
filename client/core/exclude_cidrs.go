package core

import (
	"log"
	"sync"
)

// vkExcludeCIDRs — подсети, которые должны идти напрямую, а не через туннель.
// Взято из PWDTT (backend/wg_common.go). Без этого списка VK API
// (id.vk.ru, login.vk.com, m.vk.com и т.д.) оказывается недоступен
// после поднятия туннеля, и приложение VK не может авторизоваться.
var vkExcludeCIDRs = []string{
	"87.240.128.0/18",  // VK
	"87.240.192.0/19",  // VK
	"90.156.0.0/16",    // VK TURN (90.156.234.x, 90.156.236.x и др.)
	"93.186.224.0/21",  // VK
	"95.142.192.0/21",  // VK
	"95.163.0.0/16",    // VK TURN (95.163.34.x и др.)
	"95.213.0.0/18",    // VK (id.vk.ru, login.vk.com)
	"155.212.192.0/20", // OK/VK (calls.okcdn.ru)
	"185.16.28.0/22",   // VK
	"194.67.64.0/18",   // VK
	"195.82.146.0/23",  // VK
	"213.180.193.0/24", // Яндекс DNS
	"77.88.0.0/18",     // Яндекс
}

// dnsExcludeIPs — публичные DNS-резолверы. Их маршрутизация через туннель
// создаёт лишний latency и точку отказа; резолвим напрямую.
var dnsExcludeIPs = []string{
	"8.8.8.8",
	"8.8.4.4",
	"1.1.1.1",
	"1.0.0.1",
	"77.88.8.8",       // Яндекс DNS
	"77.88.8.1",       // Яндекс DNS (вторичный)
	"9.9.9.9",         // Quad9
	"149.112.112.112", // Quad9 (вторичный)
}

// excludedRoutesMu защищает список уже добавленных маршрутов, чтобы teardown
// удалил ровно то, что добавили, и не трогал ничего лишнего.
var (
	excludedRoutesMu sync.Mutex
	excludedRoutes   []string // CIDR-ы, для которых мы добавили route
)

// applyExcludeRoutes добавляет route-ы для VK CIDR через оригинальный шлюз,
// чтобы трафик к ним не уходил в туннель.
//
// Зачем это нужно: после поднятия туннеля весь трафик уходит в WG (0.0.0.0/0),
// в том числе к id.vk.ru/login.vk.com/TURN-серверам VK. Без exclude-маршрутов
// VK API и TURN-авторизация попадают в туннель → зацикливаются или ломаются.
//
// DNS через туннель (по умолчанию): системный DNS пользователя работает
// через туннель — это нужно в РФ, где провайдер может не уметь YouTube/Google.
//
// Важно: вызывать ПОСЛЕ поднятия WG-интерфейса, но ДО того, как WG-маршрут
// 0.0.0.0/0 перехватит весь трафик. Конкретный порядок: SetupWindowsWireGuard
// сначала создаёт TUN, потом ставит default route, и только в самом конце
// добавляет exclude-маршруты с более низкой метрикой.
func applyExcludeRoutes(gateway, ifaceName string) {
	if gateway == "" {
		log.Printf("[EXCL] Шлюз пустой — exclude-маршруты НЕ добавлены (TURN/VK могут быть недоступны)")
		return
	}

	excludedRoutesMu.Lock()
	defer excludedRoutesMu.Unlock()

	addRoute := func(cidr string) {
		if runRouteAdd(cidr, gateway) {
			excludedRoutes = append(excludedRoutes, cidr)
		}
	}

	for _, cidr := range vkExcludeCIDRs {
		addRoute(cidr)
	}

	log.Printf("[EXCL] Добавлено %d exclude-маршрутов через %s (DNS — через туннель)", len(excludedRoutes), gateway)
}

// removeExcludeRoutes удаляет все ранее добавленные exclude-маршруты.
// Вызывается при teardown WG.
func removeExcludeRoutes() {
	excludedRoutesMu.Lock()
	cidrs := excludedRoutes
	excludedRoutes = nil
	excludedRoutesMu.Unlock()

	for _, cidr := range cidrs {
		runRouteDelete(cidr)
	}
	if len(cidrs) > 0 {
		log.Printf("[EXCL] Удалено %d exclude-маршрутов", len(cidrs))
	}
}
