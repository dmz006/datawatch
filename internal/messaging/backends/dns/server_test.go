package dns

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/messaging"
)

// serverReadyTimeout bounds how long waitForServer polls before failing a test.
const serverReadyTimeout = 5 * time.Second

// serverPollInterval is the pause between readiness probes.
const serverPollInterval = 10 * time.Millisecond

// listenLoopback binds UDP and TCP sockets on 127.0.0.1 with OS-assigned ports
// and keeps them open. The server under test serves on these exact sockets, so
// no port is ever closed and then rebound by another process or test.
func listenLoopback(t *testing.T) (net.PacketConn, net.Listener) {
	t.Helper()
	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		udpConn.Close() //nolint:errcheck
		t.Fatalf("listen tcp: %v", err)
	}
	t.Cleanup(func() {
		udpConn.Close() //nolint:errcheck
		tcpLn.Close()   //nolint:errcheck
	})
	return udpConn, tcpLn
}

// waitForServer polls addr with a probe query until the server returns any DNS
// reply. The probe uses a wrong secret, so the server answers REFUSED before it
// reaches the handler or consumes a nonce. Any reply proves the server is
// reading its socket. The test fails if Subscribe returns an error first or the
// deadline passes.
func waitForServer(t *testing.T, addr, domain string, serverErr <-chan error) {
	t.Helper()
	deadline := time.Now().Add(serverReadyTimeout)
	for {
		select {
		case err := <-serverErr:
			t.Fatalf("server failed to start: %v", err)
		default:
		}

		qname, err := EncodeQuery("probe", "readiness-probe-secret", domain)
		if err != nil {
			t.Fatalf("EncodeQuery: %v", err)
		}
		msg := new(mdns.Msg)
		msg.SetQuestion(qname, mdns.TypeTXT)
		client := &mdns.Client{Timeout: 100 * time.Millisecond}
		if _, _, err := client.Exchange(msg, addr); err == nil {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("server at %s did not answer a probe within %s", addr, serverReadyTimeout)
		}
		time.Sleep(serverPollInterval)
	}
}

// TestServerIntegration is deterministic because of three patterns, and these
// must be preserved. (1) The server serves on a UDP and TCP listener the test
// bound and kept open, so no port is closed and rebound. (2) Startup is awaited
// with waitForServer, a bounded readiness probe, not a fixed sleep. (3) Subscribe
// errors arrive on serverErr and fail the test at once, instead of showing up
// later as a query timeout.
func TestServerIntegration(t *testing.T) {
	udpConn, tcpLn := listenLoopback(t)
	listen := udpConn.LocalAddr().String()

	cfg := config.DNSChannelConfig{
		Enabled:         true,
		Mode:            "server",
		Domain:          "test.local",
		Listen:          listen,
		Secret:          "integration-test-secret-key!",
		TTL:             0,
		MaxResponseSize: 512,
	}

	backend := NewServer(cfg)
	backend.udpConn = udpConn
	backend.tcpListener = tcpLn
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- backend.Subscribe(ctx, func(msg messaging.Message) {
			// Echo handler — respond with the command received
			response := fmt.Sprintf("echo: %s", msg.Text)
			backend.Send(msg.GroupID, response) //nolint:errcheck
		})
	}()

	// Wait until the server answers before sending any real traffic
	waitForServer(t, listen, cfg.Domain, serverErr)

	// Test 1: Valid query
	t.Run("ValidQuery", func(t *testing.T) {
		qname, err := EncodeQuery("list", cfg.Secret, cfg.Domain)
		if err != nil {
			t.Fatalf("EncodeQuery: %v", err)
		}

		msg := new(mdns.Msg)
		msg.SetQuestion(qname, mdns.TypeTXT)
		client := &mdns.Client{Timeout: 5 * time.Second}
		resp, _, err := client.Exchange(msg, listen)
		if err != nil {
			t.Fatalf("DNS query failed: %v", err)
		}

		if resp.Rcode != mdns.RcodeSuccess {
			t.Fatalf("expected success, got %s", mdns.RcodeToString[resp.Rcode])
		}

		var records []string
		for _, rr := range resp.Answer {
			if txt, ok := rr.(*mdns.TXT); ok {
				records = append(records, txt.Txt...)
			}
		}
		decoded, err := DecodeResponse(records)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		if decoded != "echo: list" {
			t.Errorf("got %q, want %q", decoded, "echo: list")
		}
	})

	// Test 2: Bad HMAC
	t.Run("BadHMAC", func(t *testing.T) {
		qname, _ := EncodeQuery("list", "wrong-secret", cfg.Domain)
		msg := new(mdns.Msg)
		msg.SetQuestion(qname, mdns.TypeTXT)
		client := &mdns.Client{Timeout: 5 * time.Second}
		resp, _, err := client.Exchange(msg, listen)
		if err != nil {
			t.Fatalf("DNS query failed: %v", err)
		}
		if resp.Rcode != mdns.RcodeRefused {
			t.Errorf("expected REFUSED, got %s", mdns.RcodeToString[resp.Rcode])
		}
	})

	// Test 3: Replay detection
	t.Run("ReplayDetection", func(t *testing.T) {
		qname, _ := EncodeQuery("version", cfg.Secret, cfg.Domain)

		// First query should succeed
		msg1 := new(mdns.Msg)
		msg1.SetQuestion(qname, mdns.TypeTXT)
		client := &mdns.Client{Timeout: 5 * time.Second}
		resp1, _, _ := client.Exchange(msg1, listen)
		if resp1.Rcode != mdns.RcodeSuccess {
			t.Fatalf("first query: expected success, got %s", mdns.RcodeToString[resp1.Rcode])
		}

		// Same query (same nonce) should be refused
		msg2 := new(mdns.Msg)
		msg2.SetQuestion(qname, mdns.TypeTXT)
		resp2, _, _ := client.Exchange(msg2, listen)
		if resp2.Rcode != mdns.RcodeRefused {
			t.Errorf("replay: expected REFUSED, got %s", mdns.RcodeToString[resp2.Rcode])
		}
	})

	// Test 4: Multiple commands
	t.Run("MultipleCommands", func(t *testing.T) {
		commands := []string{"status abc1", "alerts 5", "kill def2"}
		for _, cmd := range commands {
			qname, _ := EncodeQuery(cmd, cfg.Secret, cfg.Domain)
			msg := new(mdns.Msg)
			msg.SetQuestion(qname, mdns.TypeTXT)
			client := &mdns.Client{Timeout: 5 * time.Second}
			resp, _, err := client.Exchange(msg, listen)
			if err != nil {
				t.Errorf("query %q failed: %v", cmd, err)
				continue
			}
			if resp.Rcode != mdns.RcodeSuccess {
				t.Errorf("query %q: expected success, got %s", cmd, mdns.RcodeToString[resp.Rcode])
				continue
			}
			var records []string
			for _, rr := range resp.Answer {
				if txt, ok := rr.(*mdns.TXT); ok {
					records = append(records, txt.Txt...)
				}
			}
			decoded, _ := DecodeResponse(records)
			expected := "echo: " + cmd
			if decoded != expected {
				t.Errorf("got %q, want %q", decoded, expected)
			}
		}
	})

	// Test 5: Non-TXT query type — should get REFUSED (not NXDOMAIN, to avoid leaking domain existence)
	t.Run("NonTXTQuery", func(t *testing.T) {
		msg := new(mdns.Msg)
		msg.SetQuestion("test.local.", mdns.TypeA)
		client := &mdns.Client{Timeout: 5 * time.Second}
		resp, _, err := client.Exchange(msg, listen)
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if resp.Rcode != mdns.RcodeRefused {
			t.Errorf("expected REFUSED for non-TXT, got %s", mdns.RcodeToString[resp.Rcode])
		}
	})

	// Test 6: Query for wrong domain — should get REFUSED via catch-all handler
	t.Run("WrongDomain", func(t *testing.T) {
		msg := new(mdns.Msg)
		msg.SetQuestion("google.com.", mdns.TypeTXT)
		client := &mdns.Client{Timeout: 5 * time.Second}
		resp, _, err := client.Exchange(msg, listen)
		if err != nil {
			t.Fatalf("query failed: %v", err)
		}
		if resp.Rcode != mdns.RcodeRefused {
			t.Errorf("expected REFUSED for wrong domain, got %s", mdns.RcodeToString[resp.Rcode])
		}
	})

	cancel()
}

// TestClientExecute is deterministic because of three patterns, and these must
// be preserved. (1) The server serves on a UDP and TCP listener the test bound
// and kept open, so no port is closed and rebound. (2) Startup is awaited with
// waitForServer, a bounded readiness probe, not a fixed sleep. (3) Subscribe
// errors arrive on serverErr and fail the test at once.
func TestClientExecute(t *testing.T) {
	udpConn, tcpLn := listenLoopback(t)
	listen := udpConn.LocalAddr().String()

	cfg := config.DNSChannelConfig{
		Enabled:         true,
		Mode:            "server",
		Domain:          "client-test.local",
		Listen:          listen,
		Secret:          "client-test-secret!",
		TTL:             0,
		MaxResponseSize: 512,
	}

	backend := NewServer(cfg)
	backend.udpConn = udpConn
	backend.tcpListener = tcpLn
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- backend.Subscribe(ctx, func(msg messaging.Message) { //nolint:errcheck
			backend.Send(msg.GroupID, "response: "+msg.Text) //nolint:errcheck
		})
	}()
	waitForServer(t, listen, cfg.Domain, serverErr)

	clientCfg := config.DNSChannelConfig{
		Domain:   cfg.Domain,
		Secret:   cfg.Secret,
		Upstream: listen,
	}
	client := NewClient(clientCfg)

	resp, err := client.Execute("help")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp != "response: help" {
		t.Errorf("got %q, want %q", resp, "response: help")
	}

	cancel()
}
