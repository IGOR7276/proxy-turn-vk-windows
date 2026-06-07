package core

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cbeuw/connutil"
	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/selfsign"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

const (
	workerSendBuf      = 128
	sessionReadTimeout = 30 * time.Minute // долгий простой не должен убивать сессию
	readBufSize        = 1600
	socketBufSize      = 625 * 1024
	keepaliveByte      = 0xFF // DTLS-level keepalive marker
	keepaliveInterval  = 15 * time.Second
	dtlsHandshakeTimeout = 20 * time.Second // медленный WG успеет договориться
)

// Handshake semaphore: limit to 3 concurrent DTLS handshakes
var handshakeSem = make(chan struct{}, 3)

// NullLoggerFactory подавляет логи pion
type NullLoggerFactory struct{}

func (n *NullLoggerFactory) NewLogger(_ string) logging.LeveledLogger { return &NullLogger{} }

type NullLogger struct{}

func (n *NullLogger) Trace(_ string)                    {}
func (n *NullLogger) Tracef(_ string, _ ...interface{}) {}
func (n *NullLogger) Debug(_ string)                    {}
func (n *NullLogger) Debugf(_ string, _ ...interface{}) {}
func (n *NullLogger) Info(_ string)                     {}
func (n *NullLogger) Infof(_ string, _ ...interface{})  {}
func (n *NullLogger) Warn(_ string)                     {}
func (n *NullLogger) Warnf(_ string, _ ...interface{})  {}
func (n *NullLogger) Error(_ string)                    {}
func (n *NullLogger) Errorf(_ string, _ ...interface{}) {}

// connectedUDPConn — обёртка для connected UDP socket → PacketConn
type connectedUDPConn struct{ *net.UDPConn }

func (c *connectedUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

func RunSession(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	d *Dispatcher,
	localPort string,
	getConfig bool,
	configCh chan<- string,
	sessionID int,
	creds *Credentials,
	deviceID, password string,
	stats *Stats,
) (bool, error) {
	configDelivered := false

	if len(creds.TurnURLs) == 0 {
		return false, fmt.Errorf("нет TURN URL в учетных данных")
	}

	var (
		turnErr     error
		tc          *turn.Client
		relay       net.PacketConn
		turnConn    net.PacketConn
		turnAddress string
	)

	// Load-balance: начинаем с URL по индексу (sessionID % len), далее по кругу.
	// С 2 TURN-URL и 9 воркерами ~5 идут на [0], 4 на [1] — агрегатный BW ×2.
	// Если первый URL упал — fallback по цепочке до рабочего.
	startIdx := 0
	if len(creds.TurnURLs) > 0 {
		startIdx = sessionID % len(creds.TurnURLs)
	}
	for i := 0; i < len(creds.TurnURLs); i++ {
		selectedURL := creds.TurnURLs[(startIdx+i)%len(creds.TurnURLs)]
		urlhost, urlport, err := net.SplitHostPort(selectedURL)
		if err != nil {
			turnErr = err
			continue
		}
		if tp.Host != "" {
			urlhost = tp.Host
		}
		if tp.Port != "" {
			urlport = tp.Port
		}
		turnAddress = net.JoinHostPort(urlhost, urlport)

		resolved, err := net.ResolveUDPAddr("udp", turnAddress)
		if err != nil {
			turnErr = err
			continue
		}
		c, err := net.DialUDP("udp", nil, resolved)
		if err != nil {
			turnErr = err
			continue
		}
		_ = c.SetReadBuffer(socketBufSize)
		_ = c.SetWriteBuffer(socketBufSize)
		turnConn = &connectedUDPConn{c}

		log.Printf("[СЕССИЯ #%d] TURN UDP (%s)", sessionID, turnAddress)

		var addrFamily turn.RequestedAddressFamily
		if peer.IP.To4() != nil {
			addrFamily = turn.RequestedAddressFamilyIPv4
		} else {
			addrFamily = turn.RequestedAddressFamilyIPv6
		}

		tc, err = turn.NewClient(&turn.ClientConfig{
			STUNServerAddr:         turnAddress,
			TURNServerAddr:         turnAddress,
			Conn:                   turnConn,
			Username:               creds.User,
			Password:               creds.Pass,
			RequestedAddressFamily: addrFamily,
			LoggerFactory:          &NullLoggerFactory{},
		})
		if err != nil {
			_ = c.Close()
			turnErr = err
			continue
		}

		if err = tc.Listen(); err != nil {
			tc.Close()
			_ = c.Close()
			turnErr = err
			continue
		}

		relay, err = tc.Allocate()
		if err != nil {
			tc.Close()
			_ = c.Close()
			if isAuthError(err) {
				handleAuthError(creds.CacheStreamID)
			}
			errStr := err.Error()
			if strings.Contains(errStr, "Quota") || strings.Contains(errStr, "486") {
				return false, fmt.Errorf("TURN квота: %w", err)
			}
			turnErr = err
			continue
		}

		break
	}

	if relay == nil {
		if turnErr == nil {
			return false, fmt.Errorf("TURN Allocate: no TURN urls available")
		}
		return false, fmt.Errorf("TURN connection failed: %w", turnErr)
	}
	defer relay.Close()
	defer tc.Close()

	// Reset error count on successful allocation
	getStreamCache(creds.CacheStreamID).errorCount.Store(0)

	log.Printf("[СЕССИЯ #%d] Relay: %s", sessionID, relay.LocalAddr())

	// Pipe для DTLS ↔ TURN relay
	pipeA, pipeB := connutil.AsyncPacketPipe()

	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	// Keepalive goroutine (TURN binding request)
	var sessionWg sync.WaitGroup
	sessionWg.Add(1)
	go func() {
		defer sessionWg.Done()
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				// Pion SendBindingRequest иногда возвращает spurious errors при
				// кратковременной блокировке UDP. Игнорируем — следующий тик
				// повторит. Не убиваем сессию из-за флапа.
				_, _ = tc.SendBindingRequest()
			}
		}
	}()

	// Relay ↔ Pipe proxy (with RTP obfuscation)
	var relayWg sync.WaitGroup
	relayWg.Add(2)

	useWrap := len(tp.WrapKey) == wrapKeyLen

	// Initialize obfs config per session
	var obfsCfg *ObfsConfig
	var obfsWriteState *ObfsState
	if useWrap {
		obfsCfg = NewObfsConfig()
		obfsWriteState = NewObfsState()
	}

	stopRelay := context.AfterFunc(sessCtx, func() {
		_ = relay.SetDeadline(time.Now())
		_ = pipeA.SetDeadline(time.Now())
	})
	defer stopRelay()

	// relay → pipeA (UNWRAP: strip RTP header + decrypt)
	go func() {
		defer relayWg.Done()
		defer sessCancel()
		// Max incoming: RTP header (12) + AEAD tag (16) + padding.
		readBufLen := readBufSize + 80
		buf := make([]byte, readBufLen)
		plain := make([]byte, readBufSize)
		for {
			n, _, readErr := relay.ReadFrom(buf)
			if readErr != nil {
				if sessCtx.Err() == nil {
					log.Printf("[ВОРКЕР #%d] Ошибка relay.ReadFrom(): %v", sessionID, readErr)
				}
				return
			}
			payload := buf[:n]
			if useWrap {
				if !obfsIsRTPPacket(payload) {
					log.Printf("[СЕССИЯ #%d] OBFS unwrap: unexpected packet (n=%d)", sessionID, n)
					continue
				}
				m, wrapErr := obfsUnwrapPacket(tp.WrapKey, payload, plain)
				if wrapErr != nil {
					log.Printf("[СЕССИЯ #%d] OBFS unwrap: %v (n=%d)", sessionID, wrapErr, n)
					continue
				}
				payload = plain[:m]
			}
			if _, writeErr := pipeA.WriteTo(payload, peer); writeErr != nil {
				return
			}
		}
	}()

	// pipeA → relay (WRAP: add RTP header + encrypt)
	go func() {
		defer relayWg.Done()
		defer sessCancel()
		b := make([]byte, readBufSize)
		for {
			n, _, readErr := pipeA.ReadFrom(b)
			if readErr != nil {
				return
			}
			out := b[:n]
			if useWrap {
				if obfsCfg != nil && obfsWriteState != nil {
					wrapped, wrapErr := obfsWrapPacket(tp.WrapKey, out, obfsCfg, obfsWriteState)
					if wrapErr != nil {
						log.Printf("[СЕССИЯ #%d] OBFS wrap: %v", sessionID, wrapErr)
						return
					}
					out = wrapped
				}
			}
			if _, writeErr := relay.WriteTo(out, peer); writeErr != nil {
				if sessCtx.Err() == nil {
					log.Printf("[ВОРКЕР #%d] Ошибка relay.WriteTo(): %v", sessionID, writeErr)
				}
				return
			}
		}
	}()

	// DTLS с поддержкой Connection ID (без SNI)
	cert, err := selfsign.GenerateSelfSigned()
	if err != nil {
		return false, fmt.Errorf("генерация сертификата: %w", err)
	}

	// Acquire handshake semaphore
	select {
	case handshakeSem <- struct{}{}:
	case <-sessCtx.Done():
		return false, sessCtx.Err()
	}

	dtlsCfg := &dtls.Config{
		Certificates:          []tls.Certificate{cert},
		InsecureSkipVerify:    true,
		ExtendedMasterSecret:  dtls.RequireExtendedMasterSecret,
		CipherSuites:          []dtls.CipherSuiteID{dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256},
		ConnectionIDGenerator: dtls.OnlySendCIDGenerator(),
		// No ServerName (SNI) — less detectable by DPI
	}

	dtlsConn, err := dtls.Client(pipeB, peer, dtlsCfg)
	if err != nil {
		<-handshakeSem
		return false, fmt.Errorf("DTLS клиент: %w", err)
	}
	defer dtlsConn.Close()

	hctx, hcancel := context.WithTimeout(sessCtx, dtlsHandshakeTimeout)
	log.Printf("[ВОРКЕР #%d] [DTLS] Рукопожатие (Handshake)...", sessionID)
	err = dtlsConn.HandshakeContext(hctx)
	hcancel()
	<-handshakeSem // RELEASE SEMAPHORE IMMEDIATELY AFTER HANDSHAKE

	if err != nil {
		if useWrap {
			errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "deadline") || strings.Contains(errStr, "timeout") {
			log.Printf("[ВОРКЕР #%d] [DTLS] Таймаут хендшейка (15s) с WRAP, пароль/WRAP не подтверждён", sessionID)
				return false, fmt.Errorf("WRAP_AUTH_TIMEOUT: DTLS timeout, пароль/WRAP не подтверждён")
			}
		}
		return false, fmt.Errorf("DTLS хендшейк: %w", err)
	}
	log.Printf("[ВОРКЕР #%d] [DTLS] Соединение установлено ✓", sessionID)

	atomic.AddInt32(&stats.ActiveConnections, 1)
	defer atomic.AddInt32(&stats.ActiveConnections, -1)

	// Запрос конфига
	if getConfig && configCh != nil {
		conf, confErr := RequestConfig(dtlsConn, localPort, deviceID, password)
		if confErr != nil {
			errStr := confErr.Error()
			if strings.Contains(errStr, "FATAL_AUTH") {
				return false, confErr
			}
			log.Printf("[ВОРКЕР #%d] Ошибка конфига: %v", sessionID, confErr)
		} else if conf != "" {
			select {
			case configCh <- conf:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг получен", sessionID)
			default:
				configDelivered = true
				log.Printf("[ВОРКЕР #%d] Конфиг уже был доставлен другим воркером", sessionID)
			}
		} else {
			log.Printf("[ВОРКЕР #%d] Сервер ещё не выдал WireGuard-конфиг, повторим позже", sessionID)
		}
	}

	log.Printf("[ВОРКЕР #%d] [READY] Туннель готов к работе ✓", sessionID)

	// Регистрация в диспетчере
	slot := &WorkerSlot{
		ID:     sessionID,
		SendCh: make(chan []byte, workerSendBuf),
	}
	d.Register(slot)
	defer d.Unregister(slot)

	// Proxy DTLS ↔ Dispatcher
	var proxyWg sync.WaitGroup
	proxyWg.Add(3) // +1 for keepalive goroutine

	stopDTLS := context.AfterFunc(sessCtx, func() {
		_ = dtlsConn.SetDeadline(time.Now())
	})
	defer stopDTLS()

	// DTLS Keepalive: prevents TURN allocation timeout and DTLS idle disconnect
	go func() {
		defer proxyWg.Done()
		t := time.NewTicker(keepaliveInterval)
		defer t.Stop()
		ping := []byte{keepaliveByte}
		for {
			select {
			case <-sessCtx.Done():
				return
			case <-t.C:
				_ = dtlsConn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, err := dtlsConn.Write(ping); err != nil {
					log.Printf("[ВОРКЕР #%d] Keepalive write failed: %v, closing session", sessionID, err)
					sessCancel()
					return
				}
			}
		}
	}()

	// Writer: dispatcher → DTLS
	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			select {
			case <-sessCtx.Done():
				return
			case pkt, ok := <-slot.SendCh:
				if !ok {
					return
				}
				_ = dtlsConn.SetWriteDeadline(time.Now().Add(sessionReadTimeout))
				_, writeErr := dtlsConn.Write(pkt)
				putPktBuf(pkt)
				if writeErr != nil {
					log.Printf("[ВОРКЕР #%d] Ошибка Writer: %v", sessionID, writeErr)
					return
				}
			}
		}
	}()

	// Reader: DTLS → dispatcher
	go func() {
		defer proxyWg.Done()
		defer sessCancel()
		for {
			pkt := getPktBuf(2048)
			_ = dtlsConn.SetReadDeadline(time.Now().Add(sessionReadTimeout))
			n, readErr := dtlsConn.Read(pkt)
			if readErr != nil {
				putPktBuf(pkt)
				if sessCtx.Err() != nil {
					return
				}
				if ne, ok := readErr.(net.Error); ok && ne.Timeout() {
					continue
				}
				log.Printf("[ВОРКЕР #%d] Ошибка Reader: %v", sessionID, readErr)
				return
			}

			// Skip keepalive pong from server
			if n == 1 && pkt[0] == keepaliveByte {
				putPktBuf(pkt)
				continue
			}

			pkt = pkt[:n]
			select {
			case d.ReturnCh <- pkt:
			case <-sessCtx.Done():
				putPktBuf(pkt)
				return
			}
		}
	}()

	proxyWg.Wait()
	sessCancel()
	relayWg.Wait()
	sessionWg.Wait()
	_ = pipeA.Close()
	_ = pipeB.Close()
	log.Printf("[СЕССИЯ #%d] Завершена", sessionID)
	return configDelivered, nil
}

