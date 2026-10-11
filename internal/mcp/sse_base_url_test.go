// GH#207: the MCP SSE "endpoint" event must advertise mcp.public_url when
// set, so clients behind a TLS-terminating reverse proxy are not told to
// POST to the server's localhost bind address.

package mcp

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/config"
)

func TestSSEBaseURL(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.MCPConfig
		want string
	}{
		{
			name: "empty public_url uses first bind address over http",
			cfg:  config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081},
			want: "http://127.0.0.1:8081",
		},
		{
			name: "empty public_url uses https when TLS enabled",
			cfg:  config.MCPConfig{SSEHost: "0.0.0.0", SSEPort: 9443, TLSEnabled: true},
			want: "https://0.0.0.0:9443",
		},
		{
			name: "empty public_url uses first of comma-separated hosts, trimmed",
			cfg:  config.MCPConfig{SSEHost: " 10.0.0.5 , 127.0.0.1", SSEPort: 8081},
			want: "http://10.0.0.5:8081",
		},
		{
			name: "whitespace-only public_url is treated as empty",
			cfg:  config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081, PublicURL: "   "},
			want: "http://127.0.0.1:8081",
		},
		{
			name: "public_url overrides bind address",
			cfg:  config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081, PublicURL: "https://host.tailnet.ts.net:8081"},
			want: "https://host.tailnet.ts.net:8081",
		},
		{
			name: "public_url trailing slashes and whitespace are trimmed",
			cfg:  config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081, PublicURL: " https://proxy.example.com/mcp// "},
			want: "https://proxy.example.com/mcp",
		},
		{
			name: "public_url wins regardless of TLS setting",
			cfg:  config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081, TLSEnabled: true, PublicURL: "http://proxy.lan:80"},
			want: "http://proxy.lan:80",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			if got := sseBaseURL(&cfg); got != tc.want {
				t.Errorf("sseBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// readEndpointEvent connects to an SSE server's /sse stream and returns the
// data line of the first "endpoint" event.
func readEndpointEvent(t *testing.T, baseURL string) string {
	t.Helper()
	mcpSrv := server.NewMCPServer("test", "0.0.0")
	sseSrv := server.NewSSEServer(mcpSrv, server.WithBaseURL(baseURL))
	ts := httptest.NewServer(sseSrv)
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ts.URL + "/sse")
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	sawEndpoint := false
	for sc.Scan() {
		line := sc.Text()
		if line == "event: endpoint" {
			sawEndpoint = true
			continue
		}
		if sawEndpoint && strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	t.Fatalf("no endpoint event received (scan err: %v)", sc.Err())
	return ""
}

func TestSSEEndpointEvent_AdvertisesPublicURL(t *testing.T) {
	cfg := config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081, PublicURL: "https://host.tailnet.ts.net:8081/"}
	got := readEndpointEvent(t, sseBaseURL(&cfg))
	if !strings.HasPrefix(got, "https://host.tailnet.ts.net:8081/message?sessionId=") {
		t.Errorf("endpoint event = %q, want prefix https://host.tailnet.ts.net:8081/message?sessionId=", got)
	}
}

func TestSSEEndpointEvent_DefaultsToBindAddress(t *testing.T) {
	cfg := config.MCPConfig{SSEHost: "127.0.0.1", SSEPort: 8081}
	got := readEndpointEvent(t, sseBaseURL(&cfg))
	if !strings.HasPrefix(got, "http://127.0.0.1:8081/message?sessionId=") {
		t.Errorf("endpoint event = %q, want prefix http://127.0.0.1:8081/message?sessionId=", got)
	}
}
