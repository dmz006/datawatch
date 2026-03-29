package dns

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/messaging"
)

// ServerBackend implements messaging.Backend as an authoritative DNS server.
// Incoming TXT queries with the configured domain are decoded as commands;
// the response from the router is encoded as TXT records in the DNS reply.
//
// Security posture:
// - All queries require HMAC-SHA256 authentication via shared secret
// - Nonce replay protection with bounded TTL store
// - Per-IP rate limiting (configurable, default 30 queries/min)
// - Non-datawatch queries receive REFUSED (no information leakage)
// - Failed auth queries are indistinguishable from non-datawatch queries
type ServerBackend struct {
	cfg    config.DNSChannelConfig
	nonces *NonceStore

	mu              sync.Mutex
	pendingResponse chan string // single-slot channel for synchronous DNS query→response

	// Per-IP rate limiting
	rateMu    sync.Mutex
	rateMap   map[string]*rateBucket
	rateLimit int // max queries per IP per minute (0 = unlimited)
}

// rateBucket tracks query count per IP within a sliding window.
type rateBucket struct {
	count    int
	windowStart time.Time
}

// NewServer creates a DNS channel server backend.
func NewServer(cfg config.DNSChannelConfig) *ServerBackend {
	maxResp := cfg.MaxResponseSize
	if maxResp <= 0 {
		maxResp = 512
	}
	rateLimit := cfg.RateLimit
	if rateLimit == 0 {
		rateLimit = 30 // default: 30 queries per IP per minute
	}
	if rateLimit < 0 {
		rateLimit = 0 // explicit disable
	}
	return &ServerBackend{
		cfg:             cfg,
		nonces:          NewNonceStore(10000, 5*time.Minute),
		pendingResponse: make(chan string, 1),
		rateMap:         make(map[string]*rateBucket),
		rateLimit:       rateLimit,
	}
}

func (b *ServerBackend) Name() string    { return "dns" }
func (b *ServerBackend) SelfID() string  { return b.cfg.Domain }
func (b *ServerBackend) Close() error    { return nil }

func (b *ServerBackend) Link(_ string, _ func(string)) error {
	return nil // no linking needed for DNS
}

// Send captures the router's response for the pending DNS query.
func (b *ServerBackend) Send(recipient, message string) error {
	select {
	case b.pendingResponse <- message:
	default:
		// Drop if no pending query (shouldn't happen in normal flow)
	}
	return nil
}

// Subscribe starts the DNS server and dispatches decoded commands to the handler.
// Blocks until ctx is cancelled.
func (b *ServerBackend) Subscribe(ctx context.Context, handler func(messaging.Message)) error {
	domain := strings.TrimSuffix(b.cfg.Domain, ".") + "."
	listen := b.cfg.Listen
	if listen == "" {
		listen = ":53"
	}

	mux := mdns.NewServeMux()
	// Handle queries for our domain
	mux.HandleFunc(domain, func(w mdns.ResponseWriter, r *mdns.Msg) {
		b.handleQuery(w, r, domain, handler)
	})
	// Catch-all: refuse anything not matching our domain
	mux.HandleFunc(".", func(w mdns.ResponseWriter, r *mdns.Msg) {
		msg := new(mdns.Msg)
		msg.SetReply(r)
		msg.Rcode = mdns.RcodeRefused
		msg.RecursionAvailable = false
		w.WriteMsg(msg) //nolint:errcheck
	})

	// Start UDP and TCP servers
	udpServer := &mdns.Server{Addr: listen, Net: "udp", Handler: mux}
	tcpServer := &mdns.Server{Addr: listen, Net: "tcp", Handler: mux}

	errCh := make(chan error, 2)
	go func() { errCh <- udpServer.ListenAndServe() }()
	go func() { errCh <- tcpServer.ListenAndServe() }()

	fmt.Printf("[dns] DNS channel server listening on %s (domain: %s)\n", listen, b.cfg.Domain)

	// Start rate limit cleanup goroutine
	go b.cleanupRateBuckets(ctx)

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		udpServer.Shutdown()  //nolint:errcheck
		tcpServer.Shutdown()  //nolint:errcheck
		return ctx.Err()
	case err := <-errCh:
		udpServer.Shutdown()  //nolint:errcheck
		tcpServer.Shutdown()  //nolint:errcheck
		return fmt.Errorf("dns server: %w", err)
	}
}

func (b *ServerBackend) handleQuery(w mdns.ResponseWriter, r *mdns.Msg, domain string, handler func(messaging.Message)) {
	msg := new(mdns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true
	msg.RecursionAvailable = false

	if len(r.Question) == 0 {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg) //nolint:errcheck
		return
	}

	q := r.Question[0]

	// All non-TXT queries get REFUSED — do not reveal domain existence via NXDOMAIN vs NOERROR
	if q.Qtype != mdns.TypeTXT {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg) //nolint:errcheck
		return
	}

	// Per-IP rate limiting
	clientIP := extractIP(w.RemoteAddr().String())
	if b.rateLimit > 0 && !b.checkRateLimit(clientIP) {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg) //nolint:errcheck
		return
	}

	// Decode and authenticate the query — all failures return REFUSED
	// (indistinguishable from non-datawatch queries to prevent oracle attacks)
	command, err := DecodeQuery(q.Name, b.cfg.Domain, b.cfg.Secret)
	if err != nil {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg) //nolint:errcheck
		return
	}

	// Extract nonce for replay check
	labels := strings.Split(strings.TrimSuffix(q.Name, "."), ".")
	nonce := ""
	if len(labels) > 0 {
		nonce = labels[0]
	}
	if nonce != "" && !b.nonces.Check(nonce) {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg) //nolint:errcheck
		return
	}

	// Drain any stale pending response
	select {
	case <-b.pendingResponse:
	default:
	}

	// Dispatch command to the router via the handler callback
	handler(messaging.Message{
		GroupID: b.cfg.Domain,
		Text:    command,
		Sender:  clientIP,
		Backend: "dns",
	})

	// Wait for response from Send() with timeout
	var response string
	select {
	case response = <-b.pendingResponse:
	case <-time.After(10 * time.Second):
		response = "timeout: no response within 10s"
	}

	// Encode response as TXT records
	maxResp := b.cfg.MaxResponseSize
	if maxResp <= 0 {
		maxResp = 512
	}
	txtRecords := EncodeResponse(response, maxResp)

	ttl := uint32(b.cfg.TTL)
	for _, txt := range txtRecords {
		rr := &mdns.TXT{
			Hdr: mdns.RR_Header{
				Name:   q.Name,
				Rrtype: mdns.TypeTXT,
				Class:  mdns.ClassINET,
				Ttl:    ttl,
			},
			Txt: []string{txt},
		}
		msg.Answer = append(msg.Answer, rr)
	}

	w.WriteMsg(msg) //nolint:errcheck
}

// checkRateLimit returns true if the IP is within the rate limit, false if exceeded.
func (b *ServerBackend) checkRateLimit(ip string) bool {
	b.rateMu.Lock()
	defer b.rateMu.Unlock()

	now := time.Now()
	bucket, ok := b.rateMap[ip]
	if !ok {
		b.rateMap[ip] = &rateBucket{count: 1, windowStart: now}
		return true
	}

	// Reset window if expired (1-minute sliding window)
	if now.Sub(bucket.windowStart) > time.Minute {
		bucket.count = 1
		bucket.windowStart = now
		return true
	}

	bucket.count++
	return bucket.count <= b.rateLimit
}

// cleanupRateBuckets periodically removes expired rate limit entries.
func (b *ServerBackend) cleanupRateBuckets(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.rateMu.Lock()
			now := time.Now()
			for ip, bucket := range b.rateMap {
				if now.Sub(bucket.windowStart) > 2*time.Minute {
					delete(b.rateMap, ip)
				}
			}
			b.rateMu.Unlock()
		}
	}
}

// extractIP strips the port from a "host:port" remote address.
func extractIP(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx >= 0 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}
