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

// freeUDPPort finds an available UDP port on localhost.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	conn.Close()
	return port
}

func TestServerIntegration(t *testing.T) {
	port := freeUDPPort(t)
	listen := fmt.Sprintf("127.0.0.1:%d", port)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- backend.Subscribe(ctx, func(msg messaging.Message) {
			// Echo handler — respond with the command received
			response := fmt.Sprintf("echo: %s", msg.Text)
			backend.Send(msg.GroupID, response)
		})
	}()

	// Wait for server to start and check for startup errors
	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-serverErr:
		t.Fatalf("server failed to start: %v", err)
	default:
	}

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

func TestClientExecute(t *testing.T) {
	port := freeUDPPort(t)
	listen := fmt.Sprintf("127.0.0.1:%d", port)

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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go backend.Subscribe(ctx, func(msg messaging.Message) {
		backend.Send(msg.GroupID, "response: "+msg.Text)
	})
	time.Sleep(500 * time.Millisecond)

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
