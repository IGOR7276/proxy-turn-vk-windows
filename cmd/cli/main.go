// wdtt-pc CLI — бинарь для запуска клиента из терминала / скрипта.
// Использует тот же client/core, что и Wails-обёртка, без UI.
//
// Использование:
//   wdtt-pc.exe -peer VPS_IP:PORT -vk HASH -password PASSWORD [опции]
//
// Опции (соответствуют старому wdtt-pc.exe):
//   -turn HOST               Переопределить TURN host
//   -port PORT               Переопределить TURN port
//   -listen ADDR             Локальный listen (default 127.0.0.1:9000)
//   -peer ADDR               VPS endpoint (обязательно)
//   -vk HASH[,HASH,...]      Хеши VK звонков (обязательно)
//   -n N                     Количество воркеров (default 24)
//   -device-id ID            Уникальный ID устройства
//   -password PWD            Пароль подключения (для WRAP)
//   -captcha-mode MODE       auto/wv/rjs
//   -fingerprint FP          chrome/safari/ios/android/firefox
//   -client-ids IDS          ID клиентов VK через запятую
//   -wg-interface NAME       Имя WireGuard интерфейса (default WDTT)
//   -windows-wg              Поднять Windows WireGuard
//   -dns "IP,IP"             Upstream DNS для локального прокси
//   -no-dns-proxy            Не поднимать локальный DNS-прокси
//   -mtu N                   MTU для WireGuard (default 1280)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"wg-turn-client/core"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(os.Stdout)

	host := flag.String("turn", "", "переопределить IP TURN")
	port := flag.String("port", "", "переопределить порт TURN")
	listen := flag.String("listen", "127.0.0.1:9000", "локальный адрес")
	vkHash := flag.String("vk", "", "хеши VK-звонков (через запятую)")
	peerAddr := flag.String("peer", "", "адрес:порт VPS сервера")
	numW := flag.Int("n", 24, "количество воркеров (кратно 9)")

	deviceID := flag.String("device-id", "unknown", "уникальный ID устройства")
	connPassword := flag.String("password", "", "пароль подключения")
	captchaMode := flag.String("captcha-mode", "auto", "режим обхода капчи (auto/wv/rjs)")
	fingerprint := flag.String("fingerprint", "chrome", "браузерный фингерпринт (chrome, safari, ios, android, firefox)")
	clientIdsFlag := flag.String("client-ids", "", "ID клиентов VK через запятую")
	wgInterface := flag.String("wg-interface", "WDTT", "имя WireGuard интерфейса на Windows")
	autoWG := flag.Bool("windows-wg", false, "поднять WireGuard интерфейс на Windows")
	dnsList := flag.String("dns", "", "upstream DNS для локального прокси. Дефолт '8.8.8.8,1.1.1.1'")
	noDNSProxy := flag.Bool("no-dns-proxy", false, "не поднимать локальный DNS-прокси")
	mtu := flag.Int("mtu", 1280, "MTU для WireGuard")

	flag.Parse()

	if *peerAddr == "" || *vkHash == "" {
		log.Fatal("[CLI] Нужны -peer и -vk")
	}

	hashes := core.ParseHashes(*vkHash)
	if len(hashes) == 0 {
		log.Fatal("[CLI] Нет хешей VK")
	}
	if *connPassword == "" {
		log.Fatal("[CLI] Нужен -password")
	}

	var customDNS []string
	if !*noDNSProxy {
		if *dnsList != "" {
			customDNS = parseCustomDNS(*dnsList)
		}
		if len(customDNS) == 0 {
			customDNS = []string{"8.8.8.8", "1.1.1.1"}
		}
	}

	cfg := core.Config{
		PeerAddr:    *peerAddr,
		Password:    *connPassword,
		Hashes:      hashes,
		Listen:      *listen,
		TurnHost:    *host,
		TurnPort:    *port,
		DeviceID:    *deviceID,
		Workers:     *numW,
		CaptchaMode: *captchaMode,
		Fingerprint: *fingerprint,
		ClientIDs:   *clientIdsFlag,
		WGInterface: *wgInterface,
		AutoWG:      *autoWG,
		DNSUpstream: customDNS,
		NoDNSProxy:  *noDNSProxy,
		WGConfigMTU: *mtu,
	}

	// Контекст с отменой по SIGTERM/SIGINT
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		s := <-sig
		log.Printf("[CLI] Сигнал %v, завершаю...", s)
		cancel()
	}()

	// STDIN для PAUSE/RESUME/STOP и CAPTCHA_RESULT
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			log.Printf("[STDIN] %s", line)
			switch {
			case line == "PAUSE":
				// Pause управляется через атомарный флаг в Core
				// (вызывающий код хранит ссылку)
			case line == "RESUME":
				// см. выше
			case line == "STOP":
				cancel()
				return
			case strings.HasPrefix(line, "CAPTCHA_RESULT|"):
				result := strings.TrimPrefix(line, "CAPTCHA_RESULT|")
				log.Printf("[КАПЧА] Результат от внешнего решателя")
				core.CaptchaResultChan <- result
			}
		}
	}()

	// Watchdog: если родитель умер — выходим (для Parent Watcher)
	ppid := os.Getppid()
	go func() {
		for {
			time.Sleep(2 * time.Second)
			if os.Getppid() != ppid {
				os.Exit(0)
			}
		}
	}()

	coreInstance := core.New(cfg)
	events, err := coreInstance.Start(ctx)
	if err != nil {
		log.Fatalf("[CLI] Не удалось запустить core: %v", err)
	}

	log.Printf("[CLI] Core запущен, listening for events...")
	for ev := range events {
		switch ev.Type {
		case core.EventState:
			log.Printf("[STATE] %s", ev.Status)
			// В не-Wails режиме шлём события родителю через stdout
			fmt.Printf("STATE|%s\n", ev.Status)
		case core.EventLog:
			log.Printf("[%s] %s", strings.ToUpper(ev.Level), ev.Msg)
		case core.EventEvent:
			log.Printf("[EVENT] %s: %s", ev.Name, ev.Data)
			// captcha_required пробрасывается во внешний решатель
			if ev.Name == "captcha_required" {
				parts := strings.SplitN(ev.Data, "|", 3)
				if len(parts) == 3 {
					fmt.Printf("CAPTCHA_SOLVE|%s|%s|%s\n", parts[0], parts[1], parts[2])
				}
			}
		case core.EventError:
			log.Printf("[ERROR] %s", ev.Msg)
		}
	}

	log.Println("[CLI] Core завершил работу")
	_ = atomic.LoadInt32 // keep atomic imported
}

// parseCustomDNS разбирает список DNS-IP из флага -dns.
func parseCustomDNS(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		ip := strings.TrimSpace(p)
		if ip == "" {
			continue
		}
		if net.ParseIP(ip) == nil {
			log.Printf("[CLI] Некорректный DNS IP %q, пропускаю", ip)
			continue
		}
		out = append(out, ip)
	}
	return out
}
