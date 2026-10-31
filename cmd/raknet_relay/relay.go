// Package main implements the Minecraft Bedrock broadcast relay.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	goraknet "github.com/sandertv/go-raknet"

	"github.com/jsandas/minecraft-bedrock-broadcast-relay/raknet"
)

type Config struct {
	RemoteAddr   string
	ListenAddr   string
	PingInterval time.Duration
	IdleTimeout  time.Duration
}

type ServerStatus struct {
	sync.RWMutex
	data       []byte
	serverGUID int64
	available  bool
}

type Session struct {
	clientAddr   *net.UDPAddr
	backendConn  *net.UDPConn
	lastActivity time.Time
	mu           sync.Mutex
	closed       bool
}

func (s *Session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		_ = s.backendConn.Close()
	}
}

type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*Session
	listener    *net.UDPConn
	remoteAddr  *net.UDPAddr
	idleTimeout time.Duration
}

func NewSessionManager(listener *net.UDPConn, remoteAddr *net.UDPAddr, idleTimeout time.Duration) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*Session),
		listener:    listener,
		remoteAddr:  remoteAddr,
		idleTimeout: idleTimeout,
	}
}

func (sm *SessionManager) GetOrCreate(clientAddr *net.UDPAddr) (*Session, error) {
	key := clientAddr.String()

	sm.mu.RLock()
	sess, exists := sm.sessions[key]
	sm.mu.RUnlock()

	if exists {
		sess.mu.Lock()
		sess.lastActivity = time.Now()
		sess.mu.Unlock()
		return sess, nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Double check under write lock
	if sess, exists = sm.sessions[key]; exists {
		sess.mu.Lock()
		sess.lastActivity = time.Now()
		sess.mu.Unlock()
		return sess, nil
	}

	backendConn, err := net.DialUDP("udp4", nil, sm.remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial backend %s: %w", sm.remoteAddr, err)
	}

	sess = &Session{
		clientAddr:   clientAddr,
		backendConn:  backendConn,
		lastActivity: time.Now(),
	}
	sm.sessions[key] = sess

	log.Printf("[Session] New client connected: %s (active sessions: %d)", key, len(sm.sessions))

	// Goroutine to forward packets from backend server back to client
	go func() {
		buf := make([]byte, 65535)
		for {
			n, err := sess.backendConn.Read(buf)
			if err != nil {
				return
			}
			sess.mu.Lock()
			sess.lastActivity = time.Now()
			sess.mu.Unlock()

			if _, err := sm.listener.WriteToUDP(buf[:n], sess.clientAddr); err != nil {
				return
			}
		}
	}()

	return sess, nil
}

func (sm *SessionManager) Forward(clientAddr *net.UDPAddr, packet []byte) error {
	sess, err := sm.GetOrCreate(clientAddr)
	if err != nil {
		return err
	}
	_, err = sess.backendConn.Write(packet)
	return err
}

func (sm *SessionManager) StartReaper(stopCh <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			sm.mu.Lock()
			for _, sess := range sm.sessions {
				sess.close()
			}
			sm.sessions = make(map[string]*Session)
			sm.mu.Unlock()
			return
		case <-ticker.C:
			sm.mu.Lock()
			now := time.Now()
			for key, sess := range sm.sessions {
				sess.mu.Lock()
				idle := now.Sub(sess.lastActivity)
				sess.mu.Unlock()

				if idle > sm.idleTimeout {
					sess.close()
					delete(sm.sessions, key)
					log.Printf("[Session] Closed idle session %s (idle for %v, active: %d)", key, idle.Round(time.Second), len(sm.sessions))
				}
			}
			sm.mu.Unlock()
		}
	}
}

func pollBackendStatus(cfg Config, status *ServerStatus, listenerPort int, stopCh <-chan struct{}) {
	ticker := time.NewTicker(cfg.PingInterval)
	defer ticker.Stop()

	query := func() {
		data, err := goraknet.PingTimeout(cfg.RemoteAddr, 3*time.Second)
		if err != nil {
			status.Lock()
			if status.available {
				log.Printf("[Status] Backend server %s became unreachable: %v", cfg.RemoteAddr, err)
				status.available = false
			}
			status.Unlock()
			return
		}

		arr := bytes.Split(data, []byte(";"))
		var serverGUID int64
		if len(arr) >= 7 {
			if id, parseErr := strconv.ParseInt(string(arr[6]), 10, 64); parseErr == nil {
				serverGUID = id
			}
		}
		if serverGUID == 0 {
			serverGUID = rand.Int63() //nolint:gosec // server GUID does not require cryptographic randomness
		}

		// Rewrite advertised port numbers to match local listener port
		portBytes := []byte(strconv.Itoa(listenerPort))
		if len(arr) >= 11 {
			arr[10] = portBytes
		}
		if len(arr) >= 12 {
			arr[11] = portBytes
		}
		modifiedData := bytes.Join(arr, []byte(";"))

		status.Lock()
		wasUnavailable := !status.available
		status.data = modifiedData
		status.serverGUID = serverGUID
		status.available = true
		status.Unlock()

		if wasUnavailable {
			log.Printf("[Status] Backend server %s online: %s", cfg.RemoteAddr, string(arr[1]))
		}
	}

	// Initial immediate check
	query()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			query()
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.RemoteAddr, "remote", getEnv("REMOTE_ADDR", "10.4.10.222:19132"), "Remote Minecraft Bedrock server address (host:port)")
	flag.StringVar(&cfg.ListenAddr, "listen", getEnv("LISTEN_ADDR", "0.0.0.0:19132"), "Local UDP address to listen on")
	flag.DurationVar(&cfg.PingInterval, "ping-interval", 5*time.Second, "Interval to refresh backend server status")
	flag.DurationVar(&cfg.IdleTimeout, "idle-timeout", 60*time.Second, "Timeout before closing idle client sessions")
	flag.Parse()

	log.Printf("[Relay] Starting Minecraft Bedrock Broadcast Relay")
	log.Printf("[Relay] Target backend: %s", cfg.RemoteAddr)
	log.Printf("[Relay] Local listen address: %s", cfg.ListenAddr)

	remoteUDPAddr, err := net.ResolveUDPAddr("udp4", cfg.RemoteAddr)
	if err != nil {
		log.Fatalf("Failed to resolve remote address %s: %v", cfg.RemoteAddr, err)
	}

	listenUDPAddr, err := net.ResolveUDPAddr("udp4", cfg.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to resolve listen address %s: %v", cfg.ListenAddr, err)
	}

	listener, err := net.ListenUDP("udp4", listenUDPAddr)
	if err != nil {
		log.Fatalf("Failed to bind UDP listener on %s: %v", cfg.ListenAddr, err)
	}
	defer listener.Close()

	localUDPAddr, ok := listener.LocalAddr().(*net.UDPAddr)
	if !ok {
		_ = listener.Close()
		log.Fatal("Failed to get local UDP address") //nolint:gocritic // defer is irrelevant; listener is explicitly closed above
	}
	actualPort := localUDPAddr.Port

	stopCh := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	status := &ServerStatus{
		serverGUID: rand.Int63(), //nolint:gosec // server GUID does not require cryptographic randomness
	}

	// Start background status poller
	go pollBackendStatus(cfg, status, actualPort, stopCh)

	// Start session manager
	sessionMgr := NewSessionManager(listener, remoteUDPAddr, cfg.IdleTimeout)
	go sessionMgr.StartReaper(stopCh)

	// Graceful shutdown handler
	go func() {
		<-sigCh
		log.Println("[Relay] Shutting down...")
		close(stopCh)
		_ = listener.Close()
	}()

	log.Printf("[Relay] Listening for LAN broadcast pings and game connections on %s", listener.LocalAddr())

	buf := make([]byte, 65535)
	for {
		n, clientAddr, err := listener.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-stopCh:
				return // clean exit on shutdown
			default:
				log.Printf("[Relay] Read error: %v", err)
				continue
			}
		}

		if n == 0 {
			continue
		}

		packetID := buf[0]

		// Handle RakNet Unconnected Ping (Discovery)
		if packetID == raknet.IDUnconnectedPing || packetID == raknet.IDUnconnectedPingOpen {
			var ping raknet.UnconnectedPing
			if err := ping.UnmarshalBinary(buf[:n]); err != nil {
				continue
			}

			status.RLock()
			isAvail := status.available
			cachedData := status.data
			serverGUID := status.serverGUID
			status.RUnlock()

			if !isAvail || len(cachedData) == 0 {
				continue
			}

			pong := raknet.UnconnectedPong{
				SendTimestamp: ping.SendTimestamp,
				ServerGUID:    serverGUID,
				Data:          cachedData,
			}

			pongBytes := pong.MarshalBinary()
			if _, err := listener.WriteToUDP(pongBytes, clientAddr); err != nil {
				log.Printf("[Relay] Failed to reply to ping from %s: %v", clientAddr, err)
			}
			continue
		}

		// Game connection packets: forward to backend session
		if err := sessionMgr.Forward(clientAddr, buf[:n]); err != nil {
			log.Printf("[Relay] Forward error from %s: %v", clientAddr, err)
		}
	}
}
