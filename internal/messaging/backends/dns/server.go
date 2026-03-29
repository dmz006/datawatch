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
type ServerBackend struct {
	cfg    config.DNSChannelConfig
	nonces *NonceStore

	mu              sync.Mutex
	pendingResponse chan string // single-slot channel for synchronous DNS query→response
}

// NewServer creates a DNS channel server backend.
func NewServer(cfg config.DNSChannelConfig) *ServerBackend {
	maxResp := cfg.MaxResponseSize
	if maxResp <= 0 {
		maxResp = 512
	}
	return &ServerBackend{
		cfg:             cfg,
		nonces:          NewNonceStore(10000, 5*time.Minute),
		pendingResponse: make(chan string, 1),
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
	mux.HandleFunc(domain, func(w mdns.ResponseWriter, r *mdns.Msg) {
		b.handleQuery(w, r, domain, handler)
	})

	// Start UDP and TCP servers
	udpServer := &mdns.Server{Addr: listen, Net: "udp", Handler: mux}
	tcpServer := &mdns.Server{Addr: listen, Net: "tcp", Handler: mux}

	errCh := make(chan error, 2)
	go func() { errCh <- udpServer.ListenAndServe() }()
	go func() { errCh <- tcpServer.ListenAndServe() }()

	fmt.Printf("[dns] DNS channel server listening on %s (domain: %s)\n", listen, b.cfg.Domain)

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		udpServer.Shutdown()
		tcpServer.Shutdown()
		return ctx.Err()
	case err := <-errCh:
		udpServer.Shutdown()
		tcpServer.Shutdown()
		return fmt.Errorf("dns server: %w", err)
	}
}

func (b *ServerBackend) handleQuery(w mdns.ResponseWriter, r *mdns.Msg, domain string, handler func(messaging.Message)) {
	msg := new(mdns.Msg)
	msg.SetReply(r)
	msg.Authoritative = true

	if len(r.Question) == 0 {
		msg.Rcode = mdns.RcodeNameError
		w.WriteMsg(msg)
		return
	}

	q := r.Question[0]
	if q.Qtype != mdns.TypeTXT {
		msg.Rcode = mdns.RcodeNameError
		w.WriteMsg(msg)
		return
	}

	// Decode and authenticate the query
	command, err := DecodeQuery(q.Name, b.cfg.Domain, b.cfg.Secret)
	if err != nil {
		msg.Rcode = mdns.RcodeRefused
		w.WriteMsg(msg)
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
		w.WriteMsg(msg)
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
		Sender:  w.RemoteAddr().String(),
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

	w.WriteMsg(msg)
}
