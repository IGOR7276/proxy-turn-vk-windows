package backend

// Константы WG-имён, исключающих маршрутов и парсинг конфига.

const wgIface = "WDTT"

// vkExcludeCIDRs — подсети которые должны идти напрямую, а не через туннель.
// Используется applyExcludeRoutes (в client/core/exclude_cidrs.go).
// 13 записей (PWDTT имеет 15 — добавим 8.8.8.0/24, 1.1.1.0/24 ниже).
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
	"8.8.8.0/24",       // Google DNS (для нашего прокси — идёт на прямую)
	"1.1.1.0/24",       // Cloudflare DNS (для нашего прокси — идёт на прямую)
}

// wgQuickOnlyFields — поля которые wg setconf не понимает (только wg-quick).
var wgQuickOnlyFields = map[string]bool{
	"address": true, "dns": true, "mtu": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}
