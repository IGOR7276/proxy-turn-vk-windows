package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed ui/index.html
var uiHTML string

var (
	vpnCmd     *exec.Cmd
	vpnRunning bool
	vpnMu      sync.Mutex
	vpnStart   time.Time

	logRing  []string
	logMu    sync.RWMutex
	logMax   = 2000
	logIndex int64

	activeConns int32
	trafficMB   float64
	statsMu     sync.RWMutex
)

func addLogLine(line string) {
	logMu.Lock()
	defer logMu.Unlock()
	logRing = append(logRing, line)
	if len(logRing) > logMax {
		logRing = logRing[len(logRing)-logMax:]
	}
}

func getRecentLogs(since int64) []string {
	logMu.RLock()
	defer logMu.RUnlock()
	start := 0
	if since > 0 && since < int64(len(logRing)) {
		start = int(since)
	}
	result := make([]string, len(logRing)-start)
	copy(result, logRing[start:])
	return result
}

func parseStatLine(line string) {
	if strings.Contains(line, "[СТАТИСТИКА]") {
		parts := strings.Split(line, "|")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if strings.HasPrefix(p, "Активных:") {
				n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(p, "Активных:")))
				statsMu.Lock()
				activeConns = int32(n)
				statsMu.Unlock()
			} else if strings.HasPrefix(p, "Трафик:") {
				v := strings.TrimSpace(strings.TrimPrefix(p, "Трафик:"))
				v = strings.Replace(v, "МБ", "", 1)
				v = strings.Replace(v, " ", "", -1)
				v = strings.Replace(v, ",", ".", 1)
				f, _ := strconv.ParseFloat(v, 64)
				statsMu.Lock()
				trafficMB = f
				statsMu.Unlock()
			}
		}
	}
}

func readOutput(rc io.ReadCloser) {
	defer rc.Close()
	buf := make([]byte, 4096)
	remain := ""
	for {
		n, err := rc.Read(buf)
		if n > 0 {
			text := remain + string(buf[:n])
			lines := strings.Split(text, "\n")
			remain = lines[len(lines)-1]
			lines = lines[:len(lines)-1]
			for _, line := range lines {
				line = strings.TrimRight(line, "\r")
				if line != "" {
					addLogLine(line)
					parseStatLine(line)
				}
			}
		}
		if err != nil {
			if remain != "" {
				addLogLine(remain)
			}
			return
		}
	}
}

func buildArgs(p map[string]interface{}) []string {
	var args []string
	args = append(args, "-peer", getStr(p, "Peer"))
	args = append(args, "-vk", getStr(p, "VkHash"))
	args = append(args, "-password", getStr(p, "Password"))
	args = append(args, "-n", fmt.Sprintf("%d", int(getFloat(p, "NumWorkers"))))
	if ids := getStr(p, "ClientIds"); ids != "" {
		args = append(args, "-client-ids", ids)
	}
	if fp := getStr(p, "Fingerprint"); fp != "" && fp != "chrome" {
		args = append(args, "-fingerprint", fp)
	}
	if cm := getStr(p, "CaptchaMode"); cm != "" && cm != "auto" {
		args = append(args, "-captcha-mode", cm)
	}
	if wg, _ := p["UseWindowsWG"].(bool); wg {
		args = append(args, "-windows-wg")
		if wi := getStr(p, "WGInterface"); wi != "" {
			args = append(args, "-wg-interface", wi)
		}
	}
	if did := getStr(p, "DeviceId"); did != "" && did != "unknown" {
		args = append(args, "-device-id", did)
	}
	args = append(args, "-turn")
	args = append(args, "")
	args = append(args, "-port")
	args = append(args, "")
	return args
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

func startVPN(p map[string]interface{}) error {
	vpnMu.Lock()
	defer vpnMu.Unlock()

	if vpnRunning {
		return fmt.Errorf("VPN уже запущен")
	}

	args := buildArgs(p)
	exe, _ := os.Executable()
	cmd := exec.Command(exe, args...)
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	vpnCmd = cmd
	vpnRunning = true
	vpnStart = time.Now()
	statsMu.Lock()
	activeConns = 0
	trafficMB = 0
	statsMu.Unlock()

	go readOutput(stdout)
	go readOutput(stderr)
	go func() {
		cmd.Wait()
		vpnMu.Lock()
		vpnRunning = false
		vpnCmd = nil
		vpnMu.Unlock()
		addLogLine("[UI] Процесс VPN завершён")
	}()

	addLogLine("[UI] VPN процесс запущен (PID: " + strconv.Itoa(cmd.Process.Pid) + ")")
	return nil
}

func stopVPN() error {
	vpnMu.Lock()
	defer vpnMu.Unlock()

	if !vpnRunning || vpnCmd == nil {
		return nil
	}

	addLogLine("[UI] Останавливаем VPN процесс...")
	pid := vpnCmd.Process.Pid

	// На Windows используем taskkill с деревом процессов
	kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	_ = kill.Run()

	vpnCmd.Process.Kill()
	vpnRunning = false
	vpnCmd = nil
	addLogLine("[UI] VPN процесс остановлен")
	return nil
}

func startUI(listenAddr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(uiHTML))
	})

	mux.HandleFunc("/api/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		var params map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), 400)
			return
		}
		if err := startVPN(params); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		stopVPN()
		w.Write([]byte("ok"))
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		vpnMu.Lock()
		running := vpnRunning
		vpnMu.Unlock()

		statsMu.RLock()
		ac := activeConns
		tm := trafficMB
		statsMu.RUnlock()

		var uptimeSec int
		if running {
			uptimeSec = int(time.Since(vpnStart).Seconds())
		}

		resp := map[string]interface{}{
			"running":           running,
			"activeConnections": ac,
			"trafficMB":         fmt.Sprintf("%.2f", tm),
			"uptimeSec":         uptimeSec,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		sinceStr := r.URL.Query().Get("since")
		var since int64
		if sinceStr != "" {
			since, _ = strconv.ParseInt(sinceStr, 10, 64)
		}
		lines := getRecentLogs(since)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lines)
	})

	mux.HandleFunc("/api/profiles", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			profs, _ := loadProfilesFromFile()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(profs)
		case "POST":
			var p VPNProfile
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if err := saveProfile(&p); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Write([]byte("ok"))
		case "DELETE":
			// single delete by name from path
			http.Error(w, "DELETE by name using /api/profiles/name", 400)
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	mux.HandleFunc("/api/profiles/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
		name = strings.TrimSpace(name)
		if name == "" {
			http.Error(w, "profile name required", 400)
			return
		}
		if r.Method == "DELETE" {
			if err := deleteProfile(name); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Write([]byte("ok"))
		} else {
			http.Error(w, "method not allowed", 405)
		}
	})

	// Определяем свободный порт
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("[UI] Не удалось запустить сервер: %v", err)
	}
	addr := listener.Addr().String()

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║        WDTT VPN Web Interface               ║")
	fmt.Println("╠══════════════════════════════════════════════╣")
	fmt.Printf("║  Открой в браузере: http://%s        ║\n", addr)
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Println()

	log.Printf("[UI] Web интерфейс запущен на http://%s", addr)
	log.Fatal(http.Serve(listener, mux))
}
