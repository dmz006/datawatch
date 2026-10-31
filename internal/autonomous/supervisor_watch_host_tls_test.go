package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// tlsProbeNodes is a mutable fake node registry for the probe's live
// lookup: tests edit a node between probes to prove the edit is honoured.
type tlsProbeNodes struct {
	mu    sync.Mutex
	nodes map[string]*compute.Node
}

func (f *tlsProbeNodes) lookup(name string) (*compute.Node, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[name]
	if !ok {
		return nil, false
	}
	cp := *n
	return &cp, true
}

func (f *tlsProbeNodes) edit(name string, fn func(n *compute.Node)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.nodes[name])
}

// newTLSProbeServer serves {"models":[{"name":"m1"}]} on /api/ps over
// TLS with httptest's self-signed certificate, and returns the server,
// its certificate as PEM and its SHA-256 fingerprint.
func newTLSProbeServer(t *testing.T) (*httptest.Server, []byte, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ps" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"m1"}]}`))
	}))
	t.Cleanup(srv.Close)
	cert := srv.Certificate()
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	sum := sha256.Sum256(cert.Raw)
	return srv, pemBytes, hex.EncodeToString(sum[:])
}

func newTLSProbe(t *testing.T, srvURL string) (*ShellSupervisorWatchHostProbe, *tlsProbeNodes) {
	t.Helper()
	reg := &tlsProbeNodes{nodes: map[string]*compute.Node{
		"tls-node": {Name: "tls-node", Address: srvURL},
	}}
	p := NewShellSupervisorWatchHostProbe(nil, "local")
	p.SetNodeLookup(reg.lookup)
	return p, reg
}

func TestSupervisorWatchProbeTLSSelfSignedFailsByDefault(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p, _ := newTLSProbe(t, srv.URL)

	models, err := p.ResidentModels("tls-node")
	if err == nil {
		t.Fatalf("a self-signed server must fail by default, got models %v", models)
	}
	if len(models) != 0 {
		t.Errorf("a failed probe must return no models, got %v", models)
	}
	for _, want := range []string{"tls-node", "certificate", "tls_ca_file", "tls_cert_fingerprint", "tls_insecure_skip_verify"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must mention %q so the operator knows how to fix it", err, want)
		}
	}
	if p.UnverifiedNode("tls-node") {
		t.Error("a verifying probe must not report the node as unverified")
	}
}

func TestSupervisorWatchProbeTLSFailsWithoutNodeRecord(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p := NewShellSupervisorWatchHostProbe(map[string]string{"tls-node": srv.URL}, "local")

	if _, err := p.ResidentModels("tls-node"); err == nil || !strings.Contains(err.Error(), "tls_ca_file") {
		t.Fatalf("an https address from the fallback map must verify against system roots, got %v", err)
	}
}

func TestSupervisorWatchProbeTLSPassesWithCAFile(t *testing.T) {
	srv, pemBytes, _ := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	reg.edit("tls-node", func(n *compute.Node) { n.TLSCAFile = caPath })

	models, err := p.ResidentModels("tls-node")
	if err != nil || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("ResidentModels with the server cert as tls_ca_file = %v, %v", models, err)
	}
	if p.UnverifiedNode("tls-node") {
		t.Error("a node verified against its CA file is not unverified")
	}
}

func TestSupervisorWatchProbeTLSUnreadableCAFileIsConfigError(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)
	missing := filepath.Join(t.TempDir(), "missing.pem")
	reg.edit("tls-node", func(n *compute.Node) { n.TLSCAFile = missing })

	models, err := p.ResidentModels("tls-node")
	if err == nil {
		t.Fatalf("an unreadable CA file must be an error, got %v", models)
	}
	for _, want := range []string{"tls-node", "tls_ca_file", missing} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must name %q", err, want)
		}
	}
	if p.UnverifiedNode("tls-node") {
		t.Error("a config error must not mark the node unverified")
	}

	// The check stays silent: unknown, no finding.
	probeHost := NewSupervisorWatchHost(p, SupervisorWatchConfig{WorkingModel: "wanted"}, fakeSupervisorWatchNodes{names: []string{"tls-node"}})
	if got := probeHost.CheckModelNotResident(); len(got) != 0 {
		t.Errorf("a TLS config error must raise no finding, got %+v", got)
	}
}

func TestSupervisorWatchProbeTLSFingerprintPin(t *testing.T) {
	srv, _, fp := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)

	reg.edit("tls-node", func(n *compute.Node) { n.TLSCertFingerprint = fp })
	models, err := p.ResidentModels("tls-node")
	if err != nil || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("ResidentModels with the matching fingerprint = %v, %v", models, err)
	}
	if p.UnverifiedNode("tls-node") {
		t.Error("a pinned node is verified, not unverified")
	}

	wrong := strings.Repeat("ab", 32)
	reg.edit("tls-node", func(n *compute.Node) { n.TLSCertFingerprint = wrong })
	models, err = p.ResidentModels("tls-node")
	if err == nil {
		t.Fatalf("a wrong fingerprint must fail, got %v", models)
	}
	if !strings.Contains(err.Error(), "tls_cert_fingerprint") {
		t.Errorf("error %q must tell the operator about tls_cert_fingerprint", err)
	}
}

func TestSupervisorWatchProbeTLSOptOutPassesAndIsReported(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)
	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = true })

	models, err := p.ResidentModels("tls-node")
	if err != nil || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("ResidentModels with the opt-out = %v, %v", models, err)
	}
	if !p.UnverifiedNode("tls-node") {
		t.Error("UnverifiedNode must report true for a node probed in verification_off mode")
	}
	if p.UnverifiedNode("other-node") {
		t.Error("UnverifiedNode must be false for a node never probed")
	}
}

func TestSupervisorWatchProbeTLSFindingNotesUnverifiedNode(t *testing.T) {
	srv, pemBytes, _ := newTLSProbeServer(t)
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	reg := &tlsProbeNodes{nodes: map[string]*compute.Node{
		"off-node":      {Name: "off-node", Address: srv.URL, TLSInsecureSkipVerify: true},
		"verified-node": {Name: "verified-node", Address: srv.URL, TLSCAFile: caPath},
	}}
	p := NewShellSupervisorWatchHostProbe(nil, "local")
	p.SetNodeLookup(reg.lookup)
	h := NewSupervisorWatchHost(p, SupervisorWatchConfig{WorkingModel: "not-loaded"}, fakeSupervisorWatchNodes{names: []string{"off-node", "verified-node"}})

	findings := h.CheckModelNotResident()
	if len(findings) != 2 {
		t.Fatalf("both nodes lack the working model, got %d findings: %+v", len(findings), findings)
	}
	byNode := map[string]SupervisorWatchFinding{}
	for _, f := range findings {
		byNode[f.Subject] = f
	}
	note := SupervisorWatchUnverifiedNote("x")
	note = strings.TrimPrefix(note, "x; ")
	if !strings.Contains(byNode["off-node"].Text, note) {
		t.Errorf("unverified node finding %q must contain %q", byNode["off-node"].Text, note)
	}
	if strings.Contains(byNode["verified-node"].Text, note) {
		t.Errorf("verified node finding %q must not contain the unverified note", byNode["verified-node"].Text)
	}
	for _, f := range findings {
		if strings.ContainsAny(f.Text, "\r\n") {
			t.Errorf("finding text must stay one line: %q", f.Text)
		}
		if supervisorWatchPRDRe.MatchString(f.Text) || supervisorWatchTrackerRe.MatchString(f.Text) {
			t.Errorf("finding text must carry no PRD word or tracker id: %q", f.Text)
		}
	}
}

func TestSupervisorWatchProbeTLSSettingsEditTakesEffect(t *testing.T) {
	srv, _, fp := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)

	if _, err := p.ResidentModels("tls-node"); err == nil {
		t.Fatal("first probe with default settings must fail")
	}
	reg.edit("tls-node", func(n *compute.Node) { n.TLSCertFingerprint = fp })
	if _, err := p.ResidentModels("tls-node"); err != nil {
		t.Fatalf("after pinning the fingerprint the probe must pass, got %v", err)
	}
	reg.edit("tls-node", func(n *compute.Node) { n.TLSCertFingerprint = ""; n.TLSInsecureSkipVerify = true })
	if _, err := p.ResidentModels("tls-node"); err != nil {
		t.Fatalf("after the opt-out the probe must pass, got %v", err)
	}
	if !p.UnverifiedNode("tls-node") {
		t.Error("the node must read as unverified after the opt-out edit")
	}
	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = false })
	if _, err := p.ResidentModels("tls-node"); err == nil {
		t.Fatal("removing the opt-out must make the probe fail again")
	}
	if p.UnverifiedNode("tls-node") {
		t.Error("the unverified mark must clear when the opt-out is removed")
	}
}

func TestSupervisorWatchProbeTLSNodeAddedAfterStartupIsProbed(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)

	if _, err := p.ResidentModels("late-node"); err == nil || !strings.Contains(err.Error(), "no model-server address") {
		t.Fatalf("an unknown node must have no address, got %v", err)
	}
	reg.mu.Lock()
	reg.nodes["late-node"] = &compute.Node{Name: "late-node", Address: srv.URL, TLSInsecureSkipVerify: true}
	reg.mu.Unlock()
	if models, err := p.ResidentModels("late-node"); err != nil || len(models) != 1 {
		t.Fatalf("a node added after startup must be probed, got %v, %v", models, err)
	}
}

func TestSupervisorWatchProbeTLSNoticeFiresOncePerSettingChange(t *testing.T) {
	srv, _, fp := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)
	var got []string
	p.SetUnverifiedProbeNotice(func(node string) { got = append(got, node) })

	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = true })
	for i := 0; i < 3; i++ {
		if _, err := p.ResidentModels("tls-node"); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || got[0] != "tls-node" {
		t.Fatalf("notice must fire once for three probes of the same settings, got %v", got)
	}

	// Moving to a pin is a setting change but not an unverified mode.
	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = false; n.TLSCertFingerprint = fp })
	if _, err := p.ResidentModels("tls-node"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("a verified mode must not fire the notice, got %v", got)
	}

	// Back to the opt-out: a new setting change fires again, once.
	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = true; n.TLSCertFingerprint = "" })
	for i := 0; i < 2; i++ {
		if _, err := p.ResidentModels("tls-node"); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("notice must fire again after the setting changed, got %v", got)
	}
}

func TestSupervisorWatchProbeTLSNilNoticeIsSafe(t *testing.T) {
	srv, _, _ := newTLSProbeServer(t)
	p, reg := newTLSProbe(t, srv.URL)
	reg.edit("tls-node", func(n *compute.Node) { n.TLSInsecureSkipVerify = true })
	p.SetUnverifiedProbeNotice(nil)
	if _, err := p.ResidentModels("tls-node"); err != nil {
		t.Fatalf("a nil notice hook must be safe, got %v", err)
	}
}

func TestSupervisorWatchProbeTLSPlainHTTPNodeUnchanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m1"}]}`))
	}))
	defer srv.Close()
	p := NewShellSupervisorWatchHostProbe(nil, "local")
	p.SetNodeLookup(func(name string) (*compute.Node, bool) {
		return &compute.Node{Name: name, Address: srv.URL}, true
	})
	models, err := p.ResidentModels("plain")
	if err != nil || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("a plain http node must work as before, got %v, %v", models, err)
	}
	if p.UnverifiedNode("plain") {
		t.Error("a plain http node is not an unverified https node")
	}
}

func TestSupervisorWatchProbeTLSHostWithoutReporterAddsNoNote(t *testing.T) {
	probe := &fakeSupervisorWatchHostProbe{residentModelsByNode: map[string][]string{"n": {"other"}}}
	h := NewSupervisorWatchHost(probe, SupervisorWatchConfig{WorkingModel: "wanted"}, fakeSupervisorWatchNodes{names: []string{"n"}})
	got := h.CheckModelNotResident()
	if len(got) != 1 || strings.Contains(got[0].Text, "certificate") {
		t.Fatalf("a probe without the reporter interface must add no note, got %+v", got)
	}
}

func TestSupervisorWatchProbeTLSSourceHasNoVerifyBypass(t *testing.T) {
	src, err := os.ReadFile("supervisor_watch_host.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "InsecureSkipVerify") {
		t.Error("supervisor_watch_host.go must not contain InsecureSkipVerify; TLS settings come from compute.NodeTLSConfig")
	}
	if strings.Contains(string(src), `"crypto/tls"`) {
		t.Error("supervisor_watch_host.go must not import crypto/tls")
	}
}
