//go:build windows
// +build windows

package core

import (
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// wintunEmbedded — встроенный wintun.dll (signed by WireGuard LLC, 0.14.1 amd64).
// Извлекается в `%LOCALAPPDATA%\wdtt\wintun.dll` при первом старте, чтобы
// пользователю не нужно было таскать DLL рядом с exe.
// Источник: client/core/assets/wintun.dll (427552 байт, SHA256 в комментарии
// рядом — обновлять при замене файла).
//
//go:embed assets/wintun.dll
var wintunEmbedded []byte

// hideWindow скрывает консольное окно при запуске дочерних процессов.
// CREATE_NO_WINDOW (0x08000000) не создаёт консоль для процесса, HideWindow
// скрывает окно если родитель Wails уже скрыл свою. Двойная защита от
// мелькающих чёрных окон cmd/powershell при старте туннеля.
const createNoWindow = 0x08000000

func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}

type wgConfig struct {
	privateKey              string
	address                 string
	dns                     string
	mtu                     int
	peerPublicKey           string
	peerEndpoint            string
	peerAllowedIPs          []string
	peerPersistentKeepalive int
}

// base64ToHex конвертирует base64-кодированный ключ WireGuard в hex-строку для IPC
func base64ToHex(b64 string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("декодирование base64: %w", err)
	}
	return hex.EncodeToString(decoded), nil
}

// teardownWindowsWireGuard убирает всё, что наставил SetupWindowsWireGuard:
// исключающие маршруты, default route, IP-адрес, локальный DNS-прокси
// и TUN-устройство. Безопасна к повторному вызову (через sync.Once).
var (
	wgTeardownOnce sync.Once
	wgTeardownFn   func()
)

// globalDNSProxy и originalDNSByIf управляют локальным DNS-прокси на
// 127.0.0.1:53 и оригинальным DNS пользователя, который мы временно
// подменяем на 127.0.0.1, чтобы все приложения ходили через наш прокси
// (а не через локальный DNS провайдера, который может перехватывать/резать).
var (
	dnsProxyMu      sync.Mutex
	globalDNSProxy  *dnsProxy
	originalDNSByIf = make(map[string][]string)
)

func SetupWindowsWireGuard(rawConf, ifaceName string, customDNS []string) error {
	cfg, err := parseWireGuardConfig(rawConf)
	if err != nil {
		return err
	}

	if cfg.mtu <= 0 {
		cfg.mtu = 1380
	}

	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	exeDir := filepath.Dir(exePath)
	if err := ensureWintunDLL(exeDir); err != nil {
		return err
	}

	tunDev, err := tun.CreateTUN(ifaceName, cfg.mtu)
	if err != nil {
		return fmt.Errorf("CreateTUN: %w", err)
	}

	actualName, err := tunDev.Name()
	if err != nil {
		_ = tunDev.Close()
		return fmt.Errorf("TUN name: %w", err)
	}
	log.Printf("[WG] TUN интерфейс создан: %s (MTU=%d)", actualName, cfg.mtu)

	if err := setWindowsInterfaceAddress(actualName, cfg.address); err != nil {
		_ = tunDev.Close()
		return err
	}

	if cfg.dns != "" {
		if err := setWindowsInterfaceDNS(actualName, cfg.dns); err != nil {
			log.Printf("[WG] Предупреждение: не удалось задать DNS: %v", err)
		}
	}

	// Запоминаем оригинальный шлюз ДО того, как WG перехватит маршрутизацию
	origGateway, origIface := getDefaultGateway()
	if origGateway == "" && origIface != "" {
		origGateway = getInterfaceGateway(origIface)
	}
	if origGateway != "" && origIface != "" {
		log.Printf("[WG] Оригинальный шлюз: %s (интерфейс: %s)", origGateway, origIface)
	} else {
		log.Printf("[WG] Не удалось определить оригинальный шлюз (gw=%q iface=%q)", origGateway, origIface)
	}

	if err := setWindowsInterfaceMetric(actualName, 1); err != nil {
		log.Printf("[WG] Предупреждение: не удалось задать метрику интерфейса: %v", err)
	}

	if err := addWindowsDefaultRoute(actualName); err != nil {
		log.Printf("[WG] Предупреждение: не удалось добавить маршрут по умолчанию: %v", err)
	}

	// TURN-исключения: добавляем /32 маршруты на TURN-сервера через оригинальный шлюз.
	// Это критично — без них WG-default route 0.0.0.0/0 перехватит трафик к TURN,
	// и туннель упадёт в первую же секунду.
	if origGateway != "" && origIface != "" {
		for _, ip := range getTurnExcludeIPs() {
			if err := addHostRoute(origIface, origGateway, ip.String()); err != nil {
				log.Printf("[WG] Не удалось добавить TURN-исключение %s: %v", ip, err)
			}
		}
	} else {
		log.Printf("[WG] ⚠ TURN-исключения НЕ добавлены — туннель может быть нестабильным")
	}

	// VK CIDR + DNS-исключения: используем route add (более простой, чем netsh).
	// Без этого id.vk.ru, login.vk.com и публичные DNS окажутся внутри туннеля,
	// что ломает авторизацию в VK и замедляет резолв.
	applyExcludeRoutes(origGateway, origIface)

	bind := conn.NewDefaultBind()
	logger := device.NewLogger(device.LogLevelSilent, "WG")
	wgDev := device.NewDevice(tunDev, bind, logger)

	// Конвертируем приватный ключ из base64 в hex для IPC
	hexPrivateKey, err := base64ToHex(cfg.privateKey)
	if err != nil {
		wgDev.Close()
		return fmt.Errorf("конвертирование приватного ключа: %w", err)
	}

	// Конвертируем публичный ключ пира из base64 в hex для IPC
	hexPeerPublicKey, err := base64ToHex(cfg.peerPublicKey)
	if err != nil {
		wgDev.Close()
		return fmt.Errorf("конвертирование публичного ключа пира: %w", err)
	}

	uapiConf := strings.Join([]string{
		fmt.Sprintf("private_key=%s", hexPrivateKey),
		"listen_port=0",
		"replace_peers=true",
		fmt.Sprintf("public_key=%s", hexPeerPublicKey),
		fmt.Sprintf("endpoint=%s", cfg.peerEndpoint),
		fmt.Sprintf("allowed_ip=%s", strings.Join(cfg.peerAllowedIPs, ",")),
		fmt.Sprintf("persistent_keepalive_interval=%d", cfg.peerPersistentKeepalive),
	}, "\n")

	if err := wgDev.IpcSet(uapiConf); err != nil {
		wgDev.Close()
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := wgDev.Up(); err != nil {
		wgDev.Close()
		return fmt.Errorf("Up: %w", err)
	}

	// Регистрируем teardown. Делаем это ПОСЛЕ успешного Up(), чтобы при ошибке
	// выше маршруты не остались без устройства.
	wgTeardownOnce = sync.Once{}
	wgTeardownFn = func() {
		wgTeardownOnce.Do(func() {
			log.Printf("[WG] Teardown интерфейса %s...", actualName)
			// Сначала останавливаем DNS-прокси и возвращаем оригинальный DNS,
			// чтобы приложения не потеряли резолв после удаления туннеля.
			dnsProxyMu.Lock()
			if globalDNSProxy != nil {
				globalDNSProxy.Stop()
				globalDNSProxy = nil
			}
			for iface, orig := range originalDNSByIf {
				if err := restoreInterfaceDNS(iface, orig); err != nil {
					log.Printf("[DNS] Не удалось восстановить DNS на %s: %v", iface, err)
				}
				delete(originalDNSByIf, iface)
			}
			dnsProxyMu.Unlock()
			_ = hiddenCmd("ipconfig", "/flushdns").Run()

			wgDev.Close()
			// Сначала убираем exclude-маршруты, чтобы вернуть трафик в норму,
			// даже если удаление TUN/default route по какой-то причине отвалится.
			removeExcludeRoutes()
			_ = runNetsh("interface", "ipv4", "delete", "route", "0.0.0.0/0", actualName, "0.0.0.0", "store=active")
			_ = runNetsh("interface", "ipv4", "delete", "address", fmt.Sprintf("name=%s", actualName), fmt.Sprintf("address=%s", cfg.address), "store=active")
			log.Printf("[WG] Teardown завершён")
		})
	}

	// Если задан customDNS — поднимаем локальный DNS-прокси на 127.0.0.1:53
	// и подменяем системный DNS на 127.0.0.1. Так приложения резолвят через
	// наш прокси (провайдер не может перехватить localhost), а прокси ходит
	// на upstream-ы через туннель (AllowedIPs 0.0.0.0/0), обходя любые
	// IP-блокировки провайдера.
	if len(customDNS) > 0 {
		// Source IP для DNS-прокси = IP ОРИГИНАЛЬНОГО интерфейса (Ethernet),
		// а не WDTT. Иначе при exclude-маршруте 8.8.8.8 через 192.168.1.1
		// ядро отказывается выпускать пакет: source=10.66.0.32, gw=192.168.1.1
		// → "unreachable host". С Ethernet source всё работает напрямую через ISP.
		srcIP := ""
		if origIface != "" {
			srcIP = getInterfaceIPv4(origIface)
		}
		if srcIP == "" {
			// fallback: cfg.address (WDTT). Может вернуть unreachable, но лучше
			// чем упасть — на некоторых системах origIface пустой.
			if host, _, err := net.ParseCIDR(cfg.address); err == nil {
				srcIP = host.String()
			}
		}
		log.Printf("[DNS] Source IP для upstream=%s (origIface=%q)", srcIP, origIface)
		proxy := newDNSProxy(customDNS, srcIP)
		if err := proxy.Start(); err != nil {
			log.Printf("[DNS] Не удалось запустить локальный прокси: %v (возможно, порт 53 занят)", err)
		} else {
			dnsProxyMu.Lock()
			globalDNSProxy = proxy
			dnsProxyMu.Unlock()
			if origIface != "" {
				orig := getInterfaceDNS(origIface)
				originalDNSByIf[origIface] = orig
				if err := setInterfaceDNS(origIface, []string{"127.0.0.1"}); err != nil {
					log.Printf("[DNS] Не удалось подменить системный DNS на %s: %v", origIface, err)
				} else {
					log.Printf("[DNS] Системный DNS %s: %v → 127.0.0.1 (через локальный прокси)", origIface, orig)
				}
			}
			// Сбрасываем кэш, чтобы новые запросы пошли сразу через прокси.
			_ = hiddenCmd("ipconfig", "/flushdns").Run()
		}
	}

	go func() {
		<-wgDev.Wait()
		log.Printf("[WG] WireGuard устройство %s остановлено", actualName)
	}()

	return nil
}

// TeardownWindowsWireGuard убирает WireGuard-интерфейс и все маршруты.
// Безопасно вызывать многократно и из любого места (включая defer).
func TeardownWindowsWireGuard() {
	if wgTeardownFn != nil {
		wgTeardownFn()
	}
}

func parseWireGuardConfig(raw string) (*wgConfig, error) {
	var cfg wgConfig
	cfg.mtu = 1280
	cfg.peerAllowedIPs = []string{"0.0.0.0/0"}
	cfg.peerPersistentKeepalive = 25

	section := ""
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch line {
		case "[Interface]":
			section = "interface"
			continue
		case "[Peer]":
			section = "peer"
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		if section == "interface" {
			switch key {
			case "privatekey":
				cfg.privateKey = value
			case "address":
				cfg.address = value
			case "dns":
				cfg.dns = value
			case "mtu":
				if mtu, err := strconv.Atoi(value); err == nil {
					cfg.mtu = mtu
				}
			}
		} else if section == "peer" {
			switch key {
			case "publickey":
				cfg.peerPublicKey = value
			case "endpoint":
				cfg.peerEndpoint = value
			case "allowedips":
				cfg.peerAllowedIPs = strings.Split(value, ",")
			case "persistentkeepalive":
				if keepalive, err := strconv.Atoi(value); err == nil {
					cfg.peerPersistentKeepalive = keepalive
				}
			}
		}
	}

	if cfg.privateKey == "" {
		return nil, fmt.Errorf("missing private key in WireGuard config")
	}
	if cfg.address == "" {
		return nil, fmt.Errorf("missing interface address in WireGuard config")
	}
	if cfg.peerPublicKey == "" {
		return nil, fmt.Errorf("missing peer public key in WireGuard config")
	}
	if cfg.peerEndpoint == "" {
		return nil, fmt.Errorf("missing peer endpoint in WireGuard config")
	}

	return &cfg, nil
}

func setWindowsInterfaceAddress(iface, cidr string) error {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("parse CIDR %q: %w", cidr, err)
	}
	mask := net.IP(ipnet.Mask).String()
	return runNetsh("interface", "ipv4", "add", "address", fmt.Sprintf("name=%s", iface), fmt.Sprintf("address=%s", ip.String()), fmt.Sprintf("mask=%s", mask), "store=active")
}

func setWindowsInterfaceDNS(iface, dns string) error {
	return runNetsh("interface", "ipv4", "set", "dnsservers", fmt.Sprintf("name=%s", iface), "source=static", fmt.Sprintf("address=%s", dns), "register=primary", "validate=no")
}

// getInterfaceDNS читает текущий список DNS-серверов интерфейса через netsh.
// Возвращает nil, если DNS назначен через DHCP (пусто) или не удалось распарсить.
func getInterfaceDNS(iface string) []string {
	out, err := hiddenCmd("netsh", "interface", "ipv4", "show", "dnsservers", fmt.Sprintf("name=%s", iface)).Output()
	if err != nil {
		return nil
	}
	var servers []string
	for _, line := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(line)
		// Строки вида "8.8.8.8" или "10.0.0.1" в выводе netsh. Пропускаем заголовки/пустые.
		if s == "" || strings.Contains(s, " ") {
			continue
		}
		if net.ParseIP(s) == nil {
			continue
		}
		servers = append(servers, s)
	}
	return servers
}

// setInterfaceDNS ставит список DNS-серверов на интерфейс. Первый сервер
// ставится через "set dnsservers source=static address=...", остальные
// добавляются через "add dnsservers".
func setInterfaceDNS(iface string, servers []string) error {
	if len(servers) == 0 {
		return nil
	}
	// Ставим первый — "set" всегда перезаписывает текущий список DNS.
	if err := runNetsh("interface", "ipv4", "set", "dnsservers", fmt.Sprintf("name=%s", iface), "source=static", fmt.Sprintf("address=%s", servers[0]), "register=primary", "validate=no"); err != nil {
		return err
	}
	// Добавляем остальные (index=2, 3, ...).
	for i := 1; i < len(servers); i++ {
		if err := runNetsh("interface", "ipv4", "add", "dnsservers", fmt.Sprintf("name=%s", iface), fmt.Sprintf("address=%s", servers[i]), fmt.Sprintf("index=%d", i+1), "validate=no"); err != nil {
			return err
		}
	}
	return nil
}

// restoreInterfaceDNS восстанавливает оригинальные DNS-серверы интерфейса.
// Если orig пуст — переключает на DHCP.
func restoreInterfaceDNS(iface string, orig []string) error {
	if len(orig) == 0 {
		return runNetsh("interface", "ipv4", "set", "dnsservers", fmt.Sprintf("name=%s", iface), "source=dhcp", "validate=no")
	}
	return setInterfaceDNS(iface, orig)
}

func setWindowsInterfaceMetric(iface string, metric int) error {
	return runNetsh("interface", "ipv4", "set", "interface", fmt.Sprintf("name=%s", iface), fmt.Sprintf("metric=%d", metric), "store=active")
}

func addWindowsDefaultRoute(iface string) error {
	_ = runNetsh("interface", "ipv4", "delete", "route", "0.0.0.0/0", iface, "0.0.0.0", "store=active")
	return runNetsh("interface", "ipv4", "add", "route", "0.0.0.0/0", iface, "0.0.0.0", "metric=1", "store=active")
}

// getDefaultGateway возвращает текущий IPv4 шлюз по умолчанию и имя интерфейса.
// Сначала пробует PowerShell, затем route print как fallback.
func getDefaultGateway() (gateway string, ifaceName string) {
	// Попытка 1: PowerShell Get-NetRoute
	if g, i := getDefaultGatewayPS(); g != "" && i != "" {
		return g, i
	}
	// Попытка 2: route.exe print (fallback)
	if g, i := getDefaultGatewayRoutePrint(); g != "" && i != "" {
		return g, i
	}
	return "", ""
}

func getDefaultGatewayPS() (gateway string, ifaceName string) {
	cmd := hiddenCmd("powershell", "-NoProfile", "-Command",
		"$r=Get-NetRoute -DestinationPrefix '0.0.0.0/0'|Select-Object -First 1; if($r){[string]$r.NextHop+'|'+$r.InterfaceAlias}")
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[WG] PowerShell get gateway: %v", err)
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		log.Printf("[WG] PowerShell gateway output: %q → пропускаем", strings.TrimSpace(string(out)))
		return "", ""
	}
	return parts[0], parts[1]
}

func getDefaultGatewayRoutePrint() (gateway string, ifaceName string) {
	out, err := hiddenCmd("route", "print", "0.0.0.0").Output()
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gw := fields[2]
			if gw != "0.0.0.0" && net.ParseIP(gw) != nil {
				// Нужно имя интерфейса — ищем его по IP
				interfaceIP := fields[3]
				if name := findInterfaceNameByIP(interfaceIP); name != "" {
					return gw, name
				}
				return gw, ""
			}
		}
	}
	return "", ""
}

// findInterfaceNameByIP ищет имя Windows-интерфейса по его IP-адресу.
func findInterfaceNameByIP(ipStr string) string {
	out, err := hiddenCmd("powershell", "-NoProfile", "-Command",
		"$ip='"+ipStr+"'; $adapter=Get-NetIPAddress -AddressFamily IPv4|Where-Object{$_.IPAddress -eq $ip}|Select-Object -First 1; if($adapter){$adapter.InterfaceAlias}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// getInterfaceIPv4 возвращает IPv4-адрес указанного интерфейса (Ethernet и т.п.).
// Используется, чтобы bind DNS-прокси на source IP оригинальной сетевой карты,
// а не на WDTT — иначе ядро отказывается выпускать пакет: source из одной подсети,
// а маршрут ведёт в другую ("unreachable host").
func getInterfaceIPv4(ifaceName string) string {
	if ifaceName == "" {
		return ""
	}
	out, err := hiddenCmd("powershell", "-NoProfile", "-Command",
		"$adapter=Get-NetIPAddress -AddressFamily IPv4 -InterfaceAlias '"+ifaceName+"'|Where-Object{$_.PrefixOrigin -ne 'WellKnown'}|Select-Object -First 1; if($adapter){$adapter.IPAddress}").Output()
	if err != nil {
		return ""
	}
	ip := strings.TrimSpace(string(out))
	if net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

// getInterfaceDefaultGateway получает шлюз по умолчанию для указанного интерфейса.
// Используется если getDefaultGateway вернул пустой шлюз но имя интерфейса известно.
func getInterfaceGateway(ifaceName string) string {
	cmd := hiddenCmd("powershell", "-NoProfile", "-Command",
		"Get-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceAlias '"+ifaceName+"'|Select-Object -First 1|ForEach-Object{[string]$_.NextHop}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	gw := strings.TrimSpace(string(out))
	if gw != "" && net.ParseIP(gw) != nil {
		return gw
	}
	return ""
}

// addHostRoute добавляет маршрут /32 для targetIP через указанный интерфейс и шлюз.
func addHostRoute(iface, gateway, targetIP string) error {
	_ = runNetsh("interface", "ipv4", "delete", "route", targetIP+"/32", iface, "store=active")
	return runNetsh("interface", "ipv4", "add", "route", targetIP+"/32", iface, gateway, "metric=0", "store=active")
}

func runNetsh(args ...string) error {
	cmd := hiddenCmd("netsh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %v failed: %w; output=%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// runRouteAdd добавляет статический маршрут через указанный шлюз.
// Использует route.exe вместо netsh, потому что:
//   - не требует повышения прав при наличии admin manifest
//   - работает одинаково и на старых, и на новых Windows
//   - не падает на длинных route-таблицах
// Возвращает true если маршрут успешно добавлен.
func runRouteAdd(cidr, gateway string) bool {
	cmd := hiddenCmd("route", "add", cidr, gateway, "metric", "1")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[EXCL] route add %s via %s: %v — %s", cidr, gateway, err, strings.TrimSpace(string(out)))
		return false
	}
	return true
}

// runRouteDelete удаляет ранее добавленный маршрут. Ошибки игнорируются
// (маршрут мог быть удалён вручную или не существовать).
func runRouteDelete(cidr string) {
	cmd := hiddenCmd("route", "delete", cidr)
	_ = cmd.Run()
}

func ensureWintunDLL(appDir string) error {
	target := filepath.Join(appDir, "wintun.dll")
	if fileExists(target) {
		return nil
	}

	// 1) Извлечь встроенный wintun.dll в %LOCALAPPDATA%\wdtt\ и скопировать в appDir.
	//    Сначала пробуем писать рядом с exe (портативный сценарий с флешки).
	if len(wintunEmbedded) > 0 {
		if err := os.WriteFile(target, wintunEmbedded, 0644); err == nil {
			log.Printf("[WG] Извлечён встроенный wintun.dll (%d KB) → %s", len(wintunEmbedded)/1024, target)
			return nil
		}
		log.Printf("[WG] Не удалось записать wintun.dll рядом с exe (%v), пробую LOCALAPPDATA", filepath.Dir(target))
	}

	// 2) Fallback: %LOCALAPPDATA%\wdtt\wintun.dll (если appDir недоступен для записи,
	//    например C:\Program Files\WDTT).
	if len(wintunEmbedded) > 0 {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			wdttDir := filepath.Join(localAppData, "wdtt")
			_ = os.MkdirAll(wdttDir, 0755)
			localTarget := filepath.Join(wdttDir, "wintun.dll")
			if err := os.WriteFile(localTarget, wintunEmbedded, 0644); err == nil {
				log.Printf("[WG] Извлечён встроенный wintun.dll → %s", localTarget)
				if err := copyFile(localTarget, target); err == nil {
					return nil
				}
			}
		}
	}

	// 3) Искать установленный WireGuard/Happ или System32
	candidates := findWintunDLLs()
	if len(candidates) == 0 {
		return fmt.Errorf("wintun.dll не найден. Переустановите wdtt или установите WireGuard for Windows")
	}

	for _, src := range candidates {
		if err := copyFile(src, target); err == nil {
			log.Printf("[WG] Скопирован wintun.dll из %s", src)
			return nil
		}
	}

	return fmt.Errorf("не удалось скопировать wintun.dll из известных путей")
}

func findWintunDLLs() []string {
	var paths []string
	progFiles := os.Getenv("ProgramFiles")
	progFilesX86 := os.Getenv("ProgramFiles(x86)")
	if progFiles != "" {
		paths = append(paths,
			filepath.Join(progFiles, "WireGuard", "wintun.dll"),
			filepath.Join(progFiles, "WireGuard", "bin", "wintun.dll"),
			filepath.Join(progFiles, "WireGuard", "wintun.dll"),
			filepath.Join(progFiles, "Happ", "core", "wintun.dll"),
			filepath.Join(progFiles, "Happ", "tun2", "wintun.dll"),
		)
	}
	if progFilesX86 != "" {
		paths = append(paths,
			filepath.Join(progFilesX86, "WireGuard", "wintun.dll"),
			filepath.Join(progFilesX86, "WireGuard", "bin", "wintun.dll"),
			filepath.Join(progFilesX86, "Happ", "core", "wintun.dll"),
			filepath.Join(progFilesX86, "Happ", "tun2", "wintun.dll"),
		)
	}
	paths = append(paths,
		`C:\Program Files\WireGuard\wintun.dll`,
		`C:\Program Files\WireGuard\bin\wintun.dll`,
		`C:\Program Files\FlyFrogLLC\Happ\core\wintun.dll`,
		`C:\Program Files\FlyFrogLLC\Happ\tun2\wintun.dll`,
		`C:\Program Files (x86)\WireGuard\wintun.dll`,
		`C:\Windows\System32\wintun.dll`,
		`C:\Windows\SysWOW64\wintun.dll`,
	)
	var result []string
	for _, p := range paths {
		if fileExists(p) {
			result = append(result, p)
		}
	}
	return result
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return nil
}

