//go:build windows
// +build windows

package main

import (
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

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

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

func SetupWindowsWireGuard(rawConf, ifaceName string) error {
	cfg, err := parseWireGuardConfig(rawConf)
	if err != nil {
		return err
	}

	if cfg.mtu <= 0 {
		cfg.mtu = 1280
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

	// Добавляем маршруты исключения для TURN-серверов (поверх WG default route)
	if origGateway != "" && origIface != "" {
		for _, ip := range getTurnExcludeIPs() {
			if err := addHostRoute(origIface, origGateway, ip.String()); err != nil {
				log.Printf("[WG] Не удалось добавить маршрут-исключение %s: %v", ip, err)
			} else {
				log.Printf("[WG] Маршрут исключения: %s → %s (%s)", ip, origIface, origGateway)
			}
		}
	} else {
		log.Printf("[WG] ⚠ Маршруты исключения НЕ добавлены — TURN-сервера могут быть недоступны после поднятия WG")
	}

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

	go func() {
		<-wgDev.Wait()
		log.Printf("[WG] WireGuard устройство %s остановлено", actualName)
	}()

	return nil
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
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
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
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
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
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"$ip='"+ipStr+"'; $adapter=Get-NetIPAddress -AddressFamily IPv4|Where-Object{$_.IPAddress -eq $ip}|Select-Object -First 1; if($adapter){$adapter.InterfaceAlias}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// getInterfaceDefaultGateway получает шлюз по умолчанию для указанного интерфейса.
// Используется если getDefaultGateway вернул пустой шлюз но имя интерфейса известно.
func getInterfaceGateway(ifaceName string) string {
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
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
	cmd := exec.Command("netsh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh %v failed: %w; output=%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func ensureWintunDLL(appDir string) error {
	target := filepath.Join(appDir, "wintun.dll")
	if fileExists(target) {
		return nil
	}

	candidates := findWintunDLLs()
	if len(candidates) == 0 {
		return fmt.Errorf("wintun.dll не найден. Установите WireGuard for Windows или скопируйте wintun.dll в папку с wdtt-client.exe")
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
