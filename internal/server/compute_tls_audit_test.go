package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
)

const computeTLSOffAction = "compute_node_tls_verification_off"

func newComputeTLSAuditServer(t *testing.T) (*Server, *audit.Log) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	return &Server{computeReg: reg, auditLog: al}, al
}

func sendComputeNode(t *testing.T, srv *Server, method, path, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.handleComputeNodes(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
	}
}

func auditByAction(t *testing.T, al *audit.Log, action string) []audit.Entry {
	t.Helper()
	got, err := al.Read(audit.QueryFilter{Action: action})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return got
}

func detailBool(t *testing.T, e audit.Entry, key string) bool {
	t.Helper()
	v, ok := e.Details[key].(bool)
	if !ok {
		t.Fatalf("%s: details[%q] = %#v, want bool (details %v)", e.Action, key, e.Details[key], e.Details)
	}
	return v
}

func TestComputeTLSAuditAddWithVerificationOff(t *testing.T) {
	srv, al := newComputeTLSAuditServer(t)
	sendComputeNode(t, srv, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-a","kind":"remote","address":"https://gpu-a:11434","tls_insecure_skip_verify":true}`)

	adds := auditByAction(t, al, "compute_node_add")
	if len(adds) != 1 {
		t.Fatalf("compute_node_add entries = %d, want 1", len(adds))
	}
	if adds[0].Details["resource_id"] != "gpu-a" || adds[0].Details["resource_type"] != "compute_node" {
		t.Fatalf("add details = %v", adds[0].Details)
	}
	if !detailBool(t, adds[0], "tls_insecure_skip_verify") || detailBool(t, adds[0], "tls_ca_file_set") || detailBool(t, adds[0], "tls_cert_fingerprint_set") {
		t.Fatalf("add TLS details = %v", adds[0].Details)
	}

	offs := auditByAction(t, al, computeTLSOffAction)
	if len(offs) != 1 {
		t.Fatalf("%s entries = %d, want 1", computeTLSOffAction, len(offs))
	}
	if offs[0].Actor != "operator" || offs[0].Details["resource_id"] != "gpu-a" || offs[0].Details["resource_type"] != "compute_node" {
		t.Fatalf("off entry = %+v", offs[0])
	}
	if !detailBool(t, offs[0], "tls_insecure_skip_verify") {
		t.Fatalf("off entry details = %v", offs[0].Details)
	}
}

func TestComputeTLSAuditAddWithCAFileOnly(t *testing.T) {
	srv, al := newComputeTLSAuditServer(t)
	const caPath = "/etc/ssl/private/node-ca-secret-path.pem"
	sendComputeNode(t, srv, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-b","kind":"remote","address":"https://gpu-b:11434","tls_ca_file":"`+caPath+`"}`)

	adds := auditByAction(t, al, "compute_node_add")
	if len(adds) != 1 {
		t.Fatalf("compute_node_add entries = %d, want 1", len(adds))
	}
	if detailBool(t, adds[0], "tls_insecure_skip_verify") || !detailBool(t, adds[0], "tls_ca_file_set") || detailBool(t, adds[0], "tls_cert_fingerprint_set") {
		t.Fatalf("add TLS details = %v", adds[0].Details)
	}
	for k, v := range adds[0].Details {
		if s, ok := v.(string); ok && strings.Contains(s, caPath) {
			t.Fatalf("details[%q] leaks the CA path: %q", k, s)
		}
	}
	if offs := auditByAction(t, al, computeTLSOffAction); len(offs) != 0 {
		t.Fatalf("%s entries = %d, want 0", computeTLSOffAction, len(offs))
	}
}

func TestComputeTLSAuditAddWithFingerprintOnly(t *testing.T) {
	srv, al := newComputeTLSAuditServer(t)
	fp := strings.Repeat("ab", 32)
	sendComputeNode(t, srv, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-c","kind":"remote","address":"https://gpu-c:11434","tls_cert_fingerprint":"`+fp+`"}`)

	adds := auditByAction(t, al, "compute_node_add")
	if len(adds) != 1 {
		t.Fatalf("compute_node_add entries = %d, want 1", len(adds))
	}
	if !detailBool(t, adds[0], "tls_cert_fingerprint_set") || detailBool(t, adds[0], "tls_ca_file_set") || detailBool(t, adds[0], "tls_insecure_skip_verify") {
		t.Fatalf("add TLS details = %v", adds[0].Details)
	}
	if offs := auditByAction(t, al, computeTLSOffAction); len(offs) != 0 {
		t.Fatalf("%s entries = %d, want 0", computeTLSOffAction, len(offs))
	}
}

func TestComputeTLSAuditUpdateTogglesVerificationOff(t *testing.T) {
	srv, al := newComputeTLSAuditServer(t)
	const base = `"name":"gpu-d","kind":"remote","address":"https://gpu-d:11434"`
	sendComputeNode(t, srv, http.MethodPost, "/api/compute/nodes?probe=skip", `{`+base+`}`)
	if offs := auditByAction(t, al, computeTLSOffAction); len(offs) != 0 {
		t.Fatalf("after plain add: %s entries = %d, want 0", computeTLSOffAction, len(offs))
	}

	sendComputeNode(t, srv, http.MethodPut, "/api/compute/nodes/gpu-d?probe=skip", `{`+base+`,"tls_insecure_skip_verify":true}`)
	offs := auditByAction(t, al, computeTLSOffAction)
	if len(offs) != 1 || offs[0].Details["resource_id"] != "gpu-d" || offs[0].Details["source"] != "compute_node_update" {
		t.Fatalf("after opt-out on: off entries = %+v", offs)
	}
	updates := auditByAction(t, al, "compute_node_update")
	if len(updates) != 1 || !detailBool(t, updates[0], "tls_insecure_skip_verify") {
		t.Fatalf("after opt-out on: update entries = %+v", updates)
	}

	sendComputeNode(t, srv, http.MethodPut, "/api/compute/nodes/gpu-d?probe=skip", `{`+base+`,"tls_insecure_skip_verify":false}`)
	if offs := auditByAction(t, al, computeTLSOffAction); len(offs) != 1 {
		t.Fatalf("after opt-out off: off entries = %d, want still 1", len(offs))
	}
	updates = auditByAction(t, al, "compute_node_update")
	if len(updates) != 2 {
		t.Fatalf("update entries = %d, want 2", len(updates))
	}
	// Read returns newest first.
	if detailBool(t, updates[0], "tls_insecure_skip_verify") || !detailBool(t, updates[1], "tls_insecure_skip_verify") {
		t.Fatalf("update entries (newest first) = %+v", updates)
	}
}

func TestComputeTLSAuditNilLogIsSafe(t *testing.T) {
	srv, _ := newComputeTLSAuditServer(t)
	srv.auditLog = nil
	sendComputeNode(t, srv, http.MethodPost, "/api/compute/nodes?probe=skip",
		`{"name":"gpu-e","kind":"remote","address":"https://gpu-e:11434","tls_insecure_skip_verify":true}`)
}
