// Command nethernet_relay answers NetherNet LAN discovery requests on behalf of a
// backend that is reachable by unicast but cannot broadcast, and forwards the
// WebRTC signaling messages between LAN clients and that backend.
package main

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jsandas/minecraft-bedrock-broadcast-relay/nethernet"
)

// Config holds the relay settings.
type Config struct {
	NNRemoteAddr string        // backend discovery address (host:7551)
	NNListenAddr string        // local discovery listen address
	PingInterval time.Duration // how often the backend is queried
	NNTTL        time.Duration // stop answering if the backend is silent this long
	PublicIP     string        // if set, ICE candidates from the backend are rewritten to this IP
}

const clientTTL = 2 * time.Minute

// nnBackend is the cached discovery state of the NetherNet backend server.
type nnBackend struct {
	sync.RWMutex
	data     []byte       // ResponsePacket application data (ServerData blob), relayed verbatim
	id       uint64       // backend's NetherNet network (sender) ID
	addr     *net.UDPAddr // resolved discovery address of the backend
	lastSeen time.Time    // last time the backend answered a discovery request
	online   bool
}

// client is a LAN client that has sent us a signaling message.
type client struct {
	addr *net.UDPAddr
	last time.Time
}

type relay struct {
	cfg     Config
	conn    *net.UDPConn
	relayID uint64
	backend *nnBackend

	clientsMu sync.Mutex
	clients   map[uint64]*client // client network ID -> address
}

func randomID() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		log.Fatalf("failed to generate network ID: %v", err)
	}
	return binary.LittleEndian.Uint64(b[:])
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.NNRemoteAddr, "remote", getEnv("NN_REMOTE_ADDR", "10.4.10.222:7551"), "NetherNet backend discovery address (host:port)")
	flag.StringVar(&cfg.NNListenAddr, "listen", getEnv("NN_LISTEN_ADDR", "0.0.0.0:7551"), "Local UDP address for NetherNet LAN discovery")
	flag.DurationVar(&cfg.PingInterval, "ping-interval", 5*time.Second, "Interval to refresh backend server status")
	flag.DurationVar(&cfg.NNTTL, "ttl", 15*time.Second, "Stop answering discovery if the backend has been silent this long")
	flag.StringVar(&cfg.PublicIP, "public-ip", getEnv("NN_PUBLIC_IP", ""), "IP clients should use for the backend's WebRTC traffic; rewrites ICE candidates from the backend (empty = leave unchanged)")
	flag.Parse()

	runNetherNet(cfg)
}

// runNetherNet runs the relay until interrupted.
func runNetherNet(cfg Config) {
	log.Printf("[NetherNet] Starting relay (backend %s, listen %s, public IP %q)", cfg.NNRemoteAddr, cfg.NNListenAddr, cfg.PublicIP)

	listenAddr, err := net.ResolveUDPAddr("udp4", cfg.NNListenAddr)
	if err != nil {
		log.Fatalf("Failed to resolve listen address %s: %v", cfg.NNListenAddr, err)
	}
	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		log.Fatalf("Failed to bind UDP listener on %s: %v", cfg.NNListenAddr, err)
	}
	defer conn.Close()

	r := &relay{
		cfg:     cfg,
		conn:    conn,
		relayID: randomID(),
		backend: &nnBackend{},
		clients: make(map[uint64]*client),
	}
	stopCh := make(chan struct{})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("[NetherNet] Shutting down...")
		close(stopCh)
		_ = conn.Close()
	}()

	go r.pollBackend(stopCh)

	buf := make([]byte, 65535)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-stopCh:
				return
			default:
				log.Printf("[NetherNet] Read error: %v", err)
				continue
			}
		}
		pk, err := nethernet.Unmarshal(buf[:n])
		if err != nil {
			log.Printf("[NetherNet] Ignoring undecodable packet from %s: %v", from, err)
			continue
		}
		if pk.SenderID == r.relayID {
			continue // our own broadcast/echo
		}

		switch pk.ID {
		case nethernet.IDRequest:
			r.handleRequest(from, pk)
		case nethernet.IDResponse:
			r.handleResponse(from, pk)
		case nethernet.IDMessage:
			r.handleMessage(from, pk)
		}
	}
}

func (r *relay) isBackend(from *net.UDPAddr) bool {
	r.backend.RLock()
	defer r.backend.RUnlock()
	return r.backend.addr != nil && r.backend.addr.IP.Equal(from.IP) && r.backend.addr.Port == from.Port
}

func (r *relay) send(to *net.UDPAddr, pk *nethernet.Packet) {
	if _, err := r.conn.WriteToUDP(nethernet.Marshal(pk), to); err != nil {
		log.Printf("[NetherNet] Failed to send to %s: %v", to, err)
	}
}

func (r *relay) handleRequest(from *net.UDPAddr, pk *nethernet.Packet) {
	r.backend.RLock()
	data, id, fresh := r.backend.data, r.backend.id, r.backend.online && time.Since(r.backend.lastSeen) <= r.cfg.NNTTL
	r.backend.RUnlock()

	if !fresh || len(data) == 0 {
		return
	}

	// Answer as the backend (same network ID) so clients address later
	// signaling messages to the ID they discovered.
	r.send(from, &nethernet.Packet{ID: nethernet.IDResponse, SenderID: id, ApplicationData: data})
}

func (r *relay) handleResponse(from *net.UDPAddr, pk *nethernet.Packet) {
	if !r.isBackend(from) {
		return // only trust responses from the configured backend
	}

	r.backend.Lock()
	wasOnline := r.backend.online
	r.backend.data = pk.ApplicationData
	r.backend.id = pk.SenderID
	r.backend.lastSeen = time.Now()
	r.backend.online = true
	r.backend.Unlock()

	if !wasOnline {
		log.Printf("[NetherNet] Backend %s online (network ID %d, %d bytes of server data)", from, pk.SenderID, len(pk.ApplicationData))
		if sd, err := nethernet.ParseServerData(pk.ApplicationData); err != nil {
			log.Printf("[NetherNet] Could not decode server data: %v (hex %x)", err, pk.ApplicationData)
		} else {
			log.Printf("[NetherNet] Server data: %+v", *sd)
		}
	}
}

// handleMessage forwards signaling messages between clients and the backend.
// Packets are not signed per sender, so they are forwarded with their original
// sender and recipient IDs; the backend learns client IDs as reachable via the
// relay's address, and replies come back here to be routed by recipient ID.
func (r *relay) handleMessage(from *net.UDPAddr, pk *nethernet.Packet) {
	kind, _, _ := strings.Cut(pk.Data, " ")
	if len(kind) > 20 {
		kind = kind[:20]
	}

	if r.isBackend(from) {
		r.clientsMu.Lock()
		c := r.clients[pk.RecipientID]
		r.clientsMu.Unlock()
		if c == nil {
			log.Printf("[NetherNet] Backend message for unknown client %d (type %q) dropped", pk.RecipientID, kind)
			return
		}

		data := pk.Data
		if r.cfg.PublicIP != "" {
			var n int
			if data, n = nethernet.RewriteCandidates(data, r.cfg.PublicIP); n > 0 {
				log.Printf("[NetherNet] Rewrote %d ICE candidate(s) to %s", n, r.cfg.PublicIP)
			}
		}
		log.Printf("[NetherNet] backend -> client %s: %q (%d bytes)", c.addr, kind, len(data))
		if kind != "" && kind != "Ping" {
			log.Printf("[NetherNet]   %s", strings.ReplaceAll(strings.TrimSpace(data), "\r\n", " | "))
		}
		r.send(c.addr, &nethernet.Packet{ID: nethernet.IDMessage, SenderID: pk.SenderID, RecipientID: pk.RecipientID, Data: data})
		return
	}

	// From a client. Sender 0 messages are keepalives with no usable identity.
	if pk.SenderID == 0 {
		return
	}
	r.clientsMu.Lock()
	r.clients[pk.SenderID] = &client{addr: from, last: time.Now()}
	r.clientsMu.Unlock()

	r.backend.RLock()
	backendAddr, backendID := r.backend.addr, r.backend.id
	r.backend.RUnlock()
	if backendAddr == nil || pk.RecipientID != backendID {
		return
	}

	log.Printf("[NetherNet] client %s (ID %d) -> backend: %q (%d bytes)", from, pk.SenderID, kind, len(pk.Data))
	r.send(backendAddr, pk)
}

// pollBackend periodically sends a unicast discovery request to the backend
// and expires idle clients.
func (r *relay) pollBackend(stopCh <-chan struct{}) {
	ticker := time.NewTicker(r.cfg.PingInterval)
	defer ticker.Stop()

	request := &nethernet.Packet{ID: nethernet.IDRequest, SenderID: r.relayID}

	poll := func() {
		remote, err := net.ResolveUDPAddr("udp4", r.cfg.NNRemoteAddr) // re-resolve; LB/DNS may change
		if err != nil {
			log.Printf("[NetherNet] Failed to resolve backend %s: %v", r.cfg.NNRemoteAddr, err)
		} else {
			r.backend.Lock()
			r.backend.addr = remote
			r.backend.Unlock()
			r.send(remote, request)
		}

		r.backend.Lock()
		if r.backend.online && time.Since(r.backend.lastSeen) > r.cfg.NNTTL {
			log.Printf("[NetherNet] Backend %s stopped responding", r.cfg.NNRemoteAddr)
			r.backend.online = false
		}
		r.backend.Unlock()

		r.clientsMu.Lock()
		for id, c := range r.clients {
			if time.Since(c.last) > clientTTL {
				delete(r.clients, id)
			}
		}
		r.clientsMu.Unlock()
	}

	poll()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			poll()
		}
	}
}
