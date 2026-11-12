package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/compute"
)

const tlsTestFingerprint = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89"

// computeTLSDaemon fakes the REST side of the node endpoints. It decodes the
// body into a compute.Node and runs the same Validate the real handler runs,
// so an invalid setting comes back as a 400 carrying Validate's own text.
type computeTLSDaemon struct {
	mu     sync.Mutex
	method string
	path   string
	raw    []byte
	body   map[string]any
}

func newComputeTLSServer(t *testing.T) (*Server, *computeTLSDaemon) {
	t.Helper()
	d := &computeTLSDaemon{}
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.method, d.path, d.raw = r.Method, r.URL.Path, raw
		d.body = map[string]any{}
		_ = json.Unmarshal(raw, &d.body)
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var n compute.Node
		if err := json.Unmarshal(raw, &n); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		if err := n.Validate(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"` + err.Error() + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"` + n.Name + `"}`))
	}))
	t.Cleanup(rest.Close)
	_, portStr, err := net.SplitHostPort(rest.Listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	return &Server{webPort: port}, d
}

func computeTLSReq(args map[string]any) mcpsdk.CallToolRequest {
	req := mcpsdk.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

// TestComputeNodeTLSToolSchemasListSettings checks both node tools advertise
// the three TLS arguments with the right types and the audit/replace wording.
func TestComputeNodeTLSToolSchemasListSettings(t *testing.T) {
	s := &Server{}
	tools := map[string]mcpsdk.Tool{
		"compute_node_add":    s.toolComputeNodeAdd(),
		"compute_node_update": s.toolComputeNodeUpdate(),
	}
	want := map[string]string{
		"tls_ca_file":              "string",
		"tls_cert_fingerprint":     "string",
		"tls_insecure_skip_verify": "boolean",
	}
	for toolName, tool := range tools {
		for arg, typ := range want {
			raw, ok := tool.InputSchema.Properties[arg]
			if !ok {
				t.Errorf("%s: schema lacks %s", toolName, arg)
				continue
			}
			prop, _ := raw.(map[string]any)
			if prop["type"] != typ {
				t.Errorf("%s.%s type = %v, want %s", toolName, arg, prop["type"], typ)
			}
			if d, _ := prop["description"].(string); d == "" {
				t.Errorf("%s.%s has no description", toolName, arg)
			}
		}
		skip, _ := tool.InputSchema.Properties["tls_insecure_skip_verify"].(map[string]any)
		desc, _ := skip["description"].(string)
		for _, phrase := range []string{"turn certificate checking off", "audited", "PWA"} {
			if !strings.Contains(desc, phrase) {
				t.Errorf("%s.tls_insecure_skip_verify description lacks %q: %s", toolName, phrase, desc)
			}
		}
	}
	upd, _ := tools["compute_node_update"].InputSchema.Properties["tls_insecure_skip_verify"].(map[string]any)
	if d, _ := upd["description"].(string); !strings.Contains(d, "replaces the whole node") || !strings.Contains(d, "omitting") {
		t.Errorf("update description must say update replaces the node and omitting clears: %s", d)
	}
	for _, arg := range []string{"tls_ca_file", "tls_cert_fingerprint"} {
		p, _ := tools["compute_node_update"].InputSchema.Properties[arg].(map[string]any)
		if d, _ := p["description"].(string); !strings.Contains(d, "omitting") {
			t.Errorf("update %s description must say omitting clears it: %s", arg, d)
		}
	}
}

// TestComputeNodeTLSAddForwardsSettings checks a compute_node_add call puts
// the three settings in the POST body that the daemon decodes as a Node.
func TestComputeNodeTLSAddForwardsSettings(t *testing.T) {
	s, d := newComputeTLSServer(t)
	res, err := s.handleComputeNodeAddMCP(context.Background(), computeTLSReq(map[string]any{
		"name":                 "gpu-1",
		"kind":                 "ollama",
		"address":              "https://gpu-1:11434",
		"tls_ca_file":          "/etc/datawatch/ca.pem",
		"tls_cert_fingerprint": tlsTestFingerprint,
	}))
	if err != nil || res == nil {
		t.Fatalf("handler: res=%v err=%v", res, err)
	}
	if d.method != http.MethodPost || d.path != "/api/compute/nodes" {
		t.Errorf("request = %s %s, want POST /api/compute/nodes", d.method, d.path)
	}
	if d.body["tls_ca_file"] != "/etc/datawatch/ca.pem" {
		t.Errorf("tls_ca_file = %v", d.body["tls_ca_file"])
	}
	if d.body["tls_cert_fingerprint"] != tlsTestFingerprint {
		t.Errorf("tls_cert_fingerprint = %v", d.body["tls_cert_fingerprint"])
	}
	if d.body["tls_insecure_skip_verify"] != false {
		t.Errorf("tls_insecure_skip_verify = %v, want bool false when not passed", d.body["tls_insecure_skip_verify"])
	}
}

// TestComputeNodeTLSUpdateForwardsSkipVerify checks the boolean arrives as a
// JSON bool on the PUT, and that unset strings stay out of the body.
func TestComputeNodeTLSUpdateForwardsSkipVerify(t *testing.T) {
	s, d := newComputeTLSServer(t)
	res, err := s.handleComputeNodeUpdateMCP(context.Background(), computeTLSReq(map[string]any{
		"name":                     "gpu-1",
		"kind":                     "ollama",
		"tls_insecure_skip_verify": true,
	}))
	if err != nil || res == nil {
		t.Fatalf("handler: res=%v err=%v", res, err)
	}
	if d.method != http.MethodPut || d.path != "/api/compute/nodes/gpu-1" {
		t.Errorf("request = %s %s, want PUT /api/compute/nodes/gpu-1", d.method, d.path)
	}
	if d.body["tls_insecure_skip_verify"] != true {
		t.Errorf("tls_insecure_skip_verify = %v, want bool true", d.body["tls_insecure_skip_verify"])
	}
	for _, k := range []string{"tls_ca_file", "tls_cert_fingerprint"} {
		if _, present := d.body[k]; present {
			t.Errorf("%s was sent although it was not set: %s", k, d.raw)
		}
	}
}

// TestComputeNodeTLSValidationErrorsSurfaceUnchanged checks the server's
// Validate text reaches the caller as is for a bad fingerprint and for skip
// combined with a CA file.
func TestComputeNodeTLSValidationErrorsSurfaceUnchanged(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "invalid fingerprint",
			args: map[string]any{"name": "gpu-1", "kind": "ollama", "tls_cert_fingerprint": "abc123"},
			want: "compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got 6",
		},
		{
			name: "skip plus ca file",
			args: map[string]any{"name": "gpu-1", "kind": "ollama", "tls_ca_file": "/etc/datawatch/ca.pem", "tls_insecure_skip_verify": true},
			want: "compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)",
		},
	}
	for _, tc := range cases {
		for _, h := range []string{"add", "update"} {
			t.Run(tc.name+"/"+h, func(t *testing.T) {
				s, _ := newComputeTLSServer(t)
				handler := s.handleComputeNodeAddMCP
				if h == "update" {
					handler = s.handleComputeNodeUpdateMCP
				}
				_, err := handler(context.Background(), computeTLSReq(tc.args))
				if err == nil {
					t.Fatal("expected the server's validation error, got nil")
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
				}
			})
		}
	}
}
