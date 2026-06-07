package core

import (
	"log"
	"net"
	"sync"
)

var (
	turnExcludeIPs []net.IP
	turnExcludeMu  sync.Mutex
)

func AddTurnExcludeIP(ipStr string) {
	ip := net.ParseIP(ipStr)
	if ip == nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
		return
	}
	turnExcludeMu.Lock()
	defer turnExcludeMu.Unlock()
	for _, existing := range turnExcludeIPs {
		if existing.Equal(ip) {
			return
		}
	}
	turnExcludeIPs = append(turnExcludeIPs, ip)
	log.Printf("[WG] TURN IP для исключения из WG: %s", ip)
}

func getTurnExcludeIPs() []net.IP {
	turnExcludeMu.Lock()
	defer turnExcludeMu.Unlock()
	result := make([]net.IP, len(turnExcludeIPs))
	copy(result, turnExcludeIPs)
	return result
}

