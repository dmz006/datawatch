// verify_tls — server-level tests: the background pull worker against a
// self-signed fake Ollama node, and the add / update / get round trip of the
// field. Fakes and local test servers only.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

func TestVerifyTLSPullWorkerOutcome(t *testing.T) {
	fake := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pull" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprint(w, `{"status":"pulling manifest","total":100,"completed":10}
{"status":"success"}
`)
	}))
	defer fake.Close()

	cases := []struct {
		name      string
		verifyTLS bool
		wantDone  bool
	}{
		{"verify_tls off ends done", false, true},
		{"verify_tls on ends failed with the certificate error", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: config.DefaultConfig()}
			s.cfg.Autonomous.ModelPullConsent = config.ModelPullConsent{Models: []config.ModelConsentEntry{{Name: "tiny", Publisher: "library", Allowed: true}}}
			node := &compute.Node{Name: "gpu-tls", Kind: "remote", Address: fake.URL, VerifyTLS: tc.verifyTLS}
			id := "verifytls-" + strings.ReplaceAll(tc.name, " ", "-")
			globalPullTasks.add(&PullTask{ID: id, NodeName: node.Name, Model: "tiny:latest", Status: "pending"})

			s.runOllamaPull(id, node, "tiny:latest") // synchronous: no goroutine left behind

			tt, ok := globalPullTasks.get(id)
			if !ok {
				t.Fatalf("task %s missing", id)
			}
			if tc.wantDone {
				if tt.Status != "done" || tt.Error != "" {
					t.Fatalf("task = %q (error %q), want done", tt.Status, tt.Error)
				}
				return
			}
			if tt.Status != "failed" {
				t.Fatalf("task = %q, want failed", tt.Status)
			}
			if !strings.Contains(tt.Error, "gpu-tls") || !strings.Contains(tt.Error, "certificate verification") {
				t.Fatalf("error must name the node and certificate verification: %q", tt.Error)
			}
		})
	}
}

func newVerifyTLSRoundTripServer(t *testing.T) *Server {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return &Server{computeReg: reg}
}

func verifyTLSCall(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleComputeNodes(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
	return w
}

// verifyTLSGet reads the node back and decodes into a map so a missing key is
// distinguishable from false.
func verifyTLSGet(t *testing.T, s *Server, name string) bool {
	t.Helper()
	w := verifyTLSCall(t, s, http.MethodGet, "/api/compute/nodes/"+name, "")
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	v, present := m["verify_tls"]
	if !present {
		t.Fatalf("verify_tls key missing from get: %s", w.Body.String())
	}
	b, isBool := v.(bool)
	if !isBool {
		t.Fatalf("verify_tls = %#v, want bool", v)
	}
	return b
}

func TestVerifyTLSRESTRoundTrip(t *testing.T) {
	s := newVerifyTLSRoundTripServer(t)

	verifyTLSCall(t, s, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-a","kind":"remote","address":"https://gpu-a:11434","verify_tls":false}`)
	if verifyTLSGet(t, s, "gpu-a") {
		t.Fatalf("explicit false read back true")
	}

	verifyTLSCall(t, s, http.MethodPut, "/api/compute/nodes/gpu-a?probe=skip",
		`{"name":"gpu-a","kind":"remote","address":"https://gpu-a:11434","verify_tls":true}`)
	if !verifyTLSGet(t, s, "gpu-a") {
		t.Fatalf("update to true read back false")
	}

	verifyTLSCall(t, s, http.MethodPut, "/api/compute/nodes/gpu-a?probe=skip",
		`{"name":"gpu-a","kind":"remote","address":"https://gpu-a:11434","verify_tls":false}`)
	if verifyTLSGet(t, s, "gpu-a") {
		t.Fatalf("update back to false read back true")
	}

	verifyTLSCall(t, s, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-b","kind":"remote","address":"https://gpu-b:11434"}`)
	if verifyTLSGet(t, s, "gpu-b") {
		t.Fatalf("node added without the field read back true")
	}
}
