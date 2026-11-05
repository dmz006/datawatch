package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/classifier"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

type fakeClassifierStatus struct {
	probes []classifier.ProbeStatus
	recs   []*classifier.Record
	err    error
}

func (f fakeClassifierStatus) Probes() []classifier.ProbeStatus { return f.probes }
func (f fakeClassifierStatus) Calibrations() ([]*classifier.Record, error) {
	return f.recs, f.err
}

// verdictAutonomous serves one PRD from GetPRD; every other method panics on
// the nil embedded interface, which the verdict route never calls.
type verdictAutonomous struct {
	AutonomousAPI
	prd *autonomous.PRD
}

func (v verdictAutonomous) GetPRD(id string) (any, bool) {
	if v.prd != nil && id == "p1" {
		return v.prd, true
	}
	return nil, false
}

func classifierTestServer(t *testing.T) (*Server, *audit.Log) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	return &Server{hostname: "h", cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.yaml"), auditLog: al}, al
}

func classifierDo(h http.HandlerFunc, method, target, body string, caps ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if len(caps) > 0 {
		req = withUsageCaps(req, caps...)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestClassifierStatus(t *testing.T) {
	s, _ := classifierTestServer(t)
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "zz-judge", Kind: inference.KindOllama, Model: "m", VerificationMode: inference.VerificationModeClassify}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&inference.LLM{Name: "aa-gen", Kind: inference.KindOllama, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	s.SetInference(reg, nil)
	s.cfg.Classifier.PublisherAllowlist = []string{"qwen"}

	// Not wired: the status still answers and says so.
	rr := classifierDo(s.handleClassifierStatus, http.MethodGet, "/api/llm/classifier", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Modes  []classifierVerificationMode `json:"verification_modes"`
		Probes []classifier.ProbeStatus     `json:"probes"`
		Cals   []classifierCalibrationView  `json:"calibrations"`
		Allow  []string                     `json:"publisher_allowlist"`
		Wired  bool                         `json:"wired"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Wired || len(got.Modes) != 2 || got.Modes[0].LLM != "aa-gen" || got.Modes[0].Mode != "generate" ||
		got.Modes[1].Mode != "classify" || len(got.Probes) != 0 || len(got.Cals) != 0 || len(got.Allow) != 1 {
		t.Fatalf("unwired body = %s", rr.Body.String())
	}

	fp := classifier.NewFingerprint("m", "q4", "n1", "p", "t")
	s.SetClassifierStatus(fakeClassifierStatus{
		probes: []classifier.ProbeStatus{{Node: "n1", Model: "m", Path: classifier.PathNative, State: classifier.StateSupported}},
		recs:   []*classifier.Record{{Fingerprint: fp, FingerprintHash: fp.Hash(), Threshold: 0.7, CreatedAt: time.Unix(0, 0)}},
	})
	rr = classifierDo(s.handleClassifierStatus, http.MethodGet, "/api/llm/classifier", "")
	got.Probes, got.Cals = nil, nil
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Wired || len(got.Probes) != 1 || got.Probes[0].State != classifier.StateSupported ||
		len(got.Cals) != 1 || got.Cals[0].Threshold != 0.7 || got.Cals[0].Model != "m" || got.Cals[0].Node != "n1" {
		t.Fatalf("wired body = %s", rr.Body.String())
	}

	// A calibration read error is reported, not hidden.
	s.SetClassifierStatus(fakeClassifierStatus{err: errors.New("disk gone")})
	rr = classifierDo(s.handleClassifierStatus, http.MethodGet, "/api/llm/classifier", "")
	if !strings.Contains(rr.Body.String(), "disk gone") {
		t.Fatalf("calibration error missing: %s", rr.Body.String())
	}
}

func TestClassifierStatusNoRegistryNoConfigAndMethod(t *testing.T) {
	s := &Server{}
	rr := classifierDo(s.handleClassifierStatus, http.MethodGet, "/api/llm/classifier", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"publisher_allowlist":[]`) {
		t.Fatalf("empty server = %d %s", rr.Code, rr.Body.String())
	}
	if rr := classifierDo(s.handleClassifierStatus, http.MethodPost, "/api/llm/classifier", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rr.Code)
	}
	if rr := classifierDo(s.handleClassifierStatus, http.MethodGet, "/api/llm/classifier", "", federation.CapAlertsRead); rr.Code != http.StatusForbidden {
		t.Fatalf("missing cap = %d", rr.Code)
	}
}

func TestClassifierPublishers(t *testing.T) {
	s, al := classifierTestServer(t)
	rr := classifierDo(s.handleClassifierPublishers, http.MethodGet, "/api/llm/classifier/publishers", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"publisher_allowlist":[]`) {
		t.Fatalf("GET = %d %s", rr.Code, rr.Body.String())
	}

	// Without confirm nothing changes.
	rr = classifierDo(s.handleClassifierPublishers, http.MethodPost, "/api/llm/classifier/publishers", `{"publisher":"Qwen"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "confirm=true") || len(s.cfg.Classifier.PublisherAllowlist) != 0 {
		t.Fatalf("unconfirmed add = %d %s allow=%v", rr.Code, rr.Body.String(), s.cfg.Classifier.PublisherAllowlist)
	}
	for body, want := range map[string]int{`{"confirm":true}`: 400, `{"publisher":"  ","confirm":true}`: 400, `{`: 400} {
		if rr := classifierDo(s.handleClassifierPublishers, http.MethodPost, "/api/llm/classifier/publishers", body); rr.Code != want {
			t.Errorf("body %q = %d, want %d", body, rr.Code, want)
		}
	}

	rr = classifierDo(s.handleClassifierPublishers, http.MethodPost, "/api/llm/classifier/publishers", `{"publisher":" Qwen ","confirm":true,"actor":"chat"}`)
	if rr.Code != http.StatusOK || len(s.cfg.Classifier.PublisherAllowlist) != 1 || s.cfg.Classifier.PublisherAllowlist[0] != "qwen" {
		t.Fatalf("confirmed add = %d %s allow=%v", rr.Code, rr.Body.String(), s.cfg.Classifier.PublisherAllowlist)
	}
	if !strings.Contains(rr.Body.String(), `"added":true`) {
		t.Fatalf("body = %s", rr.Body.String())
	}
	// Persisted to the config file.
	loaded, err := config.Load(s.cfgPath)
	if err != nil || len(loaded.Classifier.PublisherAllowlist) != 1 {
		t.Fatalf("persisted config = %+v %v", loaded, err)
	}
	// Audited with the actor.
	ents, err := al.Read(audit.QueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ents {
		if e.Action == "classifier_publisher_add" && e.Details["publisher"] == "qwen" && e.Details["actor"] == "chat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit entry: %+v", ents)
	}

	// A duplicate is a no-op: not added, not audited twice.
	rr = classifierDo(s.handleClassifierPublishers, http.MethodPost, "/api/llm/classifier/publishers", `{"publisher":"QWEN","confirm":true}`)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"added":false`) || len(s.cfg.Classifier.PublisherAllowlist) != 1 {
		t.Fatalf("duplicate = %d %s", rr.Code, rr.Body.String())
	}
	ents, _ = al.Read(audit.QueryFilter{})
	n := 0
	for _, e := range ents {
		if e.Action == "classifier_publisher_add" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("audit entries = %d, want 1", n)
	}
}

func TestClassifierPublishersGuards(t *testing.T) {
	s, _ := classifierTestServer(t)
	// A read-only capability can list but not add.
	if rr := classifierDo(s.handleClassifierPublishers, http.MethodPost, "/x", `{"publisher":"a","confirm":true}`, federation.CapLLMsRead); rr.Code != http.StatusForbidden {
		t.Fatalf("read cap add = %d", rr.Code)
	}
	if rr := classifierDo(s.handleClassifierPublishers, http.MethodGet, "/x", "", federation.CapAlertsRead); rr.Code != http.StatusForbidden {
		t.Fatalf("no cap list = %d", rr.Code)
	}
	if rr := classifierDo(s.handleClassifierPublishers, http.MethodDelete, "/x", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d", rr.Code)
	}
	// No config file path: refuse rather than change memory only.
	bare := &Server{cfg: config.DefaultConfig()}
	if rr := classifierDo(bare.handleClassifierPublishers, http.MethodPost, "/x", `{"publisher":"a","confirm":true}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("no cfgPath = %d", rr.Code)
	}
	// A failed save rolls the in-memory list back.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.cfgPath = filepath.Join(blocker, "config.yaml")
	rr := classifierDo(s.handleClassifierPublishers, http.MethodPost, "/x", `{"publisher":"a","confirm":true}`)
	if rr.Code != http.StatusInternalServerError || len(s.cfg.Classifier.PublisherAllowlist) != 0 {
		t.Fatalf("failed save = %d allow=%v", rr.Code, s.cfg.Classifier.PublisherAllowlist)
	}
}

func TestClassifierVerdicts(t *testing.T) {
	s, _ := classifierTestServer(t)
	prd := &autonomous.PRD{Story: []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{
		{ID: "t1", Verification: &autonomous.VerificationResult{Category: autonomous.ClassifyCategoryLowConfidence,
			VerifierMode: autonomous.VerifierModeClassify,
			Scores:       &autonomous.VerdictScores{PassProbability: 0.6, Threshold: 0.8},
			Route:        &autonomous.VerdictRoute{Node: "n1", Model: "m", Path: "native"}}},
		{ID: "t2", Verification: &autonomous.VerificationResult{OK: true}},
	}}}}
	get := func(q string, caps ...string) *httptest.ResponseRecorder {
		return classifierDo(s.handleClassifierVerdicts, http.MethodGet, "/api/llm/classifier/verdicts"+q, "", caps...)
	}
	if rr := get("?automaton=p1"); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("autonomous disabled = %d", rr.Code)
	}
	s.autonomousMgr = verdictAutonomous{prd: prd}
	if rr := get(""); rr.Code != http.StatusBadRequest {
		t.Fatalf("no automaton = %d", rr.Code)
	}
	if rr := get("?automaton=nope"); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown automaton = %d", rr.Code)
	}
	rr := get("?automaton=p1")
	var out struct {
		ID       string                      `json:"automaton_id"`
		Verdicts []autonomous.ClassifyRecord `json:"verdicts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil || rr.Code != 200 {
		t.Fatalf("all = %d %s", rr.Code, rr.Body.String())
	}
	if len(out.Verdicts) != 2 || out.Verdicts[0].Mode != "classify" || out.Verdicts[0].Scores.Threshold != 0.8 ||
		out.Verdicts[0].Route.Node != "n1" || !out.Verdicts[0].LowConfidence {
		t.Fatalf("records = %+v", out.Verdicts)
	}
	out.Verdicts = nil
	rr = get("?automaton=p1&low_confidence=true")
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Verdicts) != 1 || out.Verdicts[0].TaskID != "t1" {
		t.Fatalf("low-confidence = %+v", out.Verdicts)
	}
	rr = get("?automaton=p1&task=t2&low_confidence=true")
	if !strings.Contains(rr.Body.String(), `"verdicts":[]`) {
		t.Fatalf("empty list must be [], got %s", rr.Body.String())
	}
	if rr := get("?automaton=p1", federation.CapLLMsRead); rr.Code != http.StatusForbidden {
		t.Fatalf("wrong cap = %d", rr.Code)
	}
	if rr := classifierDo(s.handleClassifierVerdicts, http.MethodPost, "/x", ""); rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rr.Code)
	}
}
