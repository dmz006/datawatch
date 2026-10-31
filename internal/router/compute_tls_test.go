package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

const tlsKVFingerprint = "ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89:ab:cd:ef:01:23:45:67:89"

// computeTLSFakeDaemon stands in for the loopback REST API. Writes decode the
// body into a compute.Node and run Node.Validate like the real handler, so an
// invalid setting comes back as a 400 with Validate's own text. A GET answers
// with getBody.
type computeTLSFakeDaemon struct {
	mu      sync.Mutex
	method  string
	path    string
	body    map[string]any
	getBody string
}

func newComputeTLSRouter(t *testing.T, d *computeTLSFakeDaemon) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		d.mu.Lock()
		d.method, d.path = r.Method, r.URL.Path
		d.body = map[string]any{}
		_ = json.Unmarshal(raw, &d.body)
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(d.getBody))
			return
		}
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
		out, _ := json.Marshal(n)
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	port, err := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err != nil {
		t.Fatalf("port from %q: %v", srv.URL, err)
	}
	r := &Router{
		hostname: "h",
		backend:  &captureBackend{name: "test", capture: func(string) {}},
	}
	r.SetWebPort(port)
	return r
}

// lastReply returns the final message of a HandleTestMessage run.
func lastReply(t *testing.T, replies []string) string {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("no reply from the router")
	}
	return replies[len(replies)-1]
}

// TestComputeNodeTLSParseKeyValuePairs covers the three keys for add and
// update: strings are trimmed and kept as typed, the boolean is a real bool.
func TestComputeNodeTLSParseKeyValuePairs(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantCA   any
		wantFP   any
		wantSkip any
	}{
		{name: "ca file only", in: "kind=ollama tls_ca_file=/etc/datawatch/ca.pem", wantCA: "/etc/datawatch/ca.pem"},
		{name: "fingerprint with colons", in: "kind=ollama tls_cert_fingerprint=" + tlsKVFingerprint, wantFP: tlsKVFingerprint},
		{name: "ca file and fingerprint", in: "tls_ca_file=/ca.pem tls_cert_fingerprint=abcd", wantCA: "/ca.pem", wantFP: "abcd"},
		{name: "skip true", in: "tls_insecure_skip_verify=true", wantSkip: true},
		{name: "skip yes", in: "tls_insecure_skip_verify=yes", wantSkip: true},
		{name: "skip 1", in: "tls_insecure_skip_verify=1", wantSkip: true},
		{name: "skip false", in: "tls_insecure_skip_verify=false", wantSkip: false},
		{name: "keys are case-insensitive", in: "TLS_INSECURE_SKIP_VERIFY=true TLS_CA_FILE=/x.pem", wantSkip: true, wantCA: "/x.pem"},
		{name: "absent keys stay absent", in: "kind=ollama address=https://gpu-1:11434"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseKVPairs(tc.in)
			for key, want := range map[string]any{
				"tls_ca_file":              tc.wantCA,
				"tls_cert_fingerprint":     tc.wantFP,
				"tls_insecure_skip_verify": tc.wantSkip,
			} {
				v, present := got[key]
				if want == nil {
					if present {
						t.Errorf("%s = %#v, want absent", key, v)
					}
					continue
				}
				if v != want {
					t.Errorf("%s = %#v, want %#v", key, v, want)
				}
			}
		})
	}
}

// TestComputeNodeTLSAddAndUpdateSendKeys runs the full chat commands and
// checks the daemon sees the settings as a Node would decode them.
func TestComputeNodeTLSAddAndUpdateSendKeys(t *testing.T) {
	for _, verb := range []string{"add", "update"} {
		t.Run(verb, func(t *testing.T) {
			d := &computeTLSFakeDaemon{}
			r := newComputeTLSRouter(t, d)
			replies := r.HandleTestMessage("compute node " + verb + " gpu-1 kind=ollama tls_ca_file=/etc/datawatch/ca.pem tls_cert_fingerprint=" + tlsKVFingerprint)

			wantMethod, wantPath := http.MethodPost, "/api/compute/nodes"
			if verb == "update" {
				wantMethod, wantPath = http.MethodPut, "/api/compute/nodes/gpu-1"
			}
			if d.method != wantMethod || d.path != wantPath {
				t.Fatalf("request = %s %s, want %s %s", d.method, d.path, wantMethod, wantPath)
			}
			if d.body["tls_ca_file"] != "/etc/datawatch/ca.pem" || d.body["tls_cert_fingerprint"] != tlsKVFingerprint {
				t.Errorf("body = %v", d.body)
			}
			if got := lastReply(t, replies); !strings.Contains(got, "tls: ca file + pinned") {
				t.Errorf("reply lacks the tls summary line: %s", got)
			}
		})
	}
}

// TestComputeNodeTLSSkipVerifyIsBoolInBody makes sure the skip flag reaches
// the daemon as a JSON bool; a string would fail to decode into a Node.
func TestComputeNodeTLSSkipVerifyIsBoolInBody(t *testing.T) {
	d := &computeTLSFakeDaemon{}
	r := newComputeTLSRouter(t, d)
	replies := r.HandleTestMessage("compute node add gpu-1 kind=ollama tls_insecure_skip_verify=true")
	if d.body["tls_insecure_skip_verify"] != true {
		t.Fatalf("tls_insecure_skip_verify = %#v, want bool true", d.body["tls_insecure_skip_verify"])
	}
	if got := lastReply(t, replies); !strings.Contains(got, "tls: verification OFF") {
		t.Errorf("reply lacks the verification OFF line: %s", got)
	}
}

// TestComputeNodeTLSValidationErrorsSurfaceUnchanged checks the server's
// error text is what the operator reads for a bad fingerprint and for skip
// combined with a CA file.
func TestComputeNodeTLSValidationErrorsSurfaceUnchanged(t *testing.T) {
	cases := []struct{ name, kv, want string }{
		{"invalid fingerprint", "tls_cert_fingerprint=abc123", "compute node: tls_cert_fingerprint must be 64 hex chars (SHA-256), got 6"},
		{"skip plus ca file", "tls_ca_file=/ca.pem tls_insecure_skip_verify=true", "compute node: tls_insecure_skip_verify cannot be combined with tls_ca_file or tls_cert_fingerprint (choose one: verify, pin or skip)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &computeTLSFakeDaemon{}
			r := newComputeTLSRouter(t, d)
			replies := r.HandleTestMessage("compute node add gpu-1 kind=ollama " + tc.kv)
			if got := lastReply(t, replies); !strings.Contains(got, tc.want) {
				t.Errorf("reply = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

// TestComputeNodeTLSGetShowsSummaryLine checks each certificate mode gets
// its own summary line under `compute node get`.
func TestComputeNodeTLSGetShowsSummaryLine(t *testing.T) {
	cases := []struct{ name, node, want string }{
		{"default verifies", `{"name":"gpu-1","kind":"ollama"}`, "tls: verify"},
		{"ca file", `{"name":"gpu-1","tls_ca_file":"/ca.pem"}`, "tls: ca file"},
		{"pinned", `{"name":"gpu-1","tls_cert_fingerprint":"` + strings.Repeat("ab", 32) + `"}`, "tls: pinned"},
		{"ca file and pinned", `{"name":"gpu-1","tls_ca_file":"/ca.pem","tls_cert_fingerprint":"` + strings.Repeat("ab", 32) + `"}`, "tls: ca file + pinned"},
		{"skip verify", `{"name":"gpu-1","tls_insecure_skip_verify":true}`, "tls: verification OFF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &computeTLSFakeDaemon{getBody: tc.node}
			r := newComputeTLSRouter(t, d)
			replies := r.HandleTestMessage("compute node get gpu-1")
			last := lastReply(t, replies)
			if !strings.HasSuffix(strings.TrimSpace(last), tc.want) {
				t.Errorf("reply does not end with %q:\n%s", tc.want, last)
			}
		})
	}
}

// TestComputeNodeTLSSummarySkipsNonNodeBodies keeps lists and errors as they
// were: no summary line is invented for them.
func TestComputeNodeTLSSummarySkipsNonNodeBodies(t *testing.T) {
	for _, body := range []string{`[{"name":"a"},{"name":"b"}]`, `{"error":"not found"}`, `not json`, ``} {
		if got := withTLSSummary(body); strings.Contains(got, "tls:") {
			t.Errorf("withTLSSummary(%q) = %q, want no summary line", body, got)
		}
	}
}

// TestComputeNodeTLSHelpListsKeys checks the usage text names the three keys,
// the audit and PWA note, and that update replaces the whole node.
func TestComputeNodeTLSHelpListsKeys(t *testing.T) {
	for _, want := range []string{
		"tls_ca_file", "tls_cert_fingerprint", "tls_insecure_skip_verify",
		"audited", "PWA", "replaces the whole node",
	} {
		if !strings.Contains(computeUsage, want) {
			t.Errorf("computeUsage lacks %q", want)
		}
	}
	d := &computeTLSFakeDaemon{}
	r := newComputeTLSRouter(t, d)
	replies := r.HandleTestMessage("compute help")
	if got := lastReply(t, replies); !strings.Contains(got, "tls_insecure_skip_verify") {
		t.Errorf("`compute help` reply lacks the tls keys: %s", got)
	}
}
