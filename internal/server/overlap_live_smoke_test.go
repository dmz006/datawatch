package server

// Search-before-create smoke. Two parts, like the triage smoke.
//
// Part 1 (TestOverlapSmoke) always runs. It is in process, with no network
// and no daemon: a real autonomous Manager over a file-backed store, a real
// audit log, the real finder and sources, and the create, config and read
// handlers called directly. It uses no fakes at all, because a create only
// stores a draft and no session starts. One Automaton is created in shadow
// mode, a near-identical one follows, and its shadow audit entry must name
// the first. Then the mode is turned on through PUT /api/config, a third
// Automaton with the first one's spec must carry overlap_warnings that name
// the first, and the read route must return the same warning object.
//
// Part 2 (TestOverlapSmokeLive) is an opt-in check against a SANDBOX daemon.
// It is skipped unless the environment names the daemon:
//
//	DW_LIVE_OVERLAP_SMOKE=https://host:port  base URL of the sandbox daemon
//	                                         (required; no default, so the
//	                                         test can never reach a
//	                                         production daemon by accident)
//	DW_BASE_TOKEN=...                        bearer token, if the daemon has one
//	DW_BASE_INSECURE=0                       verify TLS (default: skip
//	                                         verify, as the other live
//	                                         scripts do)
//
//	Run it against a sandbox daemon the operator started:
//
//		DW_LIVE_OVERLAP_SMOKE=https://127.0.0.1:28443 \
//		  env -u TMUX -u TMUX_TMPDIR go test -count=1 -run 'TestOverlapSmoke(Live)?$' -v ./internal/server/
//
// The live part is read-only. It calls GET /api/config and
// GET /api/autonomous/overlap, and it never creates an Automaton.

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

const overlapSmokeLiveEnv = "DW_LIVE_OVERLAP_SMOKE"

// overlapSmokeSpec1 is distinctive: no other fixture in this package uses
// these words together.
const overlapSmokeSpec1 = "Rotate the sandbox audit archive nightly, compress each dated copy with gzip, and keep only the newest fourteen archives on the lane storage volume."

// overlapSmokeSpec2 is the first spec with one word added, so every term of
// the first is shared and the search finds it.
const overlapSmokeSpec2 = overlapSmokeSpec1 + " Please."

// overlapSmokeReply is the part of a create reply the smoke reads.
type overlapSmokeReply struct {
	ID              string                      `json:"id"`
	Status          string                      `json:"status"`
	OverlapWarnings []autonomous.OverlapWarning `json:"overlap_warnings"`
}

// overlapSmokeAuditLine is the part of one audit.log line the smoke reads.
type overlapSmokeAuditLine struct {
	Action  string `json:"action"`
	Subject string `json:"subject"`
	Details struct {
		Mode     string                      `json:"mode"`
		Warnings []autonomous.OverlapWarning `json:"warnings"`
	} `json:"details"`
}

// TestOverlapSmoke runs the whole search-before-create path in process.
func TestOverlapSmoke(t *testing.T) {
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	auditDir := filepath.Join(dir, "audit")
	al, err := audit.New(auditDir)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	cfg := config.DefaultConfig()
	cfg.Autonomous.Automation.CreateSearchMode = config.CreateSearchModeShadow
	s := &Server{
		autonomousMgr: newTestAPI(mgr),
		cfg:           cfg,
		cfgPath:       filepath.Join(dir, "config.yaml"),
	}
	s.SetAuditLog(al)

	// The project directory sits under the system temp directory, which
	// newTestAPI allows as a project root. It has no registry file, so the
	// registry source reports unavailable, as it does on the sandbox.
	project := filepath.Join(dir, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	create := func(t *testing.T, spec string) (overlapSmokeReply, map[string]json.RawMessage) {
		t.Helper()
		body, err := json.Marshal(map[string]string{"spec": spec, "project_dir": project})
		if err != nil {
			t.Fatalf("marshal create body: %v", err)
		}
		rr := httptest.NewRecorder()
		s.handleAutonomousPRDs(rr, httptest.NewRequest(http.MethodPost, "/api/autonomous/prds", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("create: HTTP %d, body=%s", rr.Code, rr.Body.String())
		}
		var reply overlapSmokeReply
		if err := json.Unmarshal(rr.Body.Bytes(), &reply); err != nil {
			t.Fatalf("create reply does not parse: %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
			t.Fatalf("create reply is not an object: %v", err)
		}
		if reply.ID == "" {
			t.Fatalf("create reply has no id, body=%s", rr.Body.String())
		}
		return reply, raw
	}

	// 1. Shadow: the first Automaton is a draft and the reply has no warnings.
	first, firstRaw := create(t, overlapSmokeSpec1)
	if first.Status != "draft" {
		t.Errorf("first status = %q, want draft", first.Status)
	}
	if _, ok := firstRaw["overlap_warnings"]; ok {
		t.Errorf("shadow create reply carries overlap_warnings: %s", firstRaw["overlap_warnings"])
	}

	// 2. Shadow: the near-identical second has no warnings in its reply, but
	// its audit entry names the first.
	second, secondRaw := create(t, overlapSmokeSpec2)
	if _, ok := secondRaw["overlap_warnings"]; ok {
		t.Errorf("shadow create reply carries overlap_warnings: %s", secondRaw["overlap_warnings"])
	}
	entries := overlapSmokeAuditEntries(t, auditDir)
	var shadowNamesFirst bool
	for _, e := range entries {
		if e.Action != "automaton_create_search" || e.Subject != second.ID {
			continue
		}
		if e.Details.Mode != "shadow" {
			t.Errorf("audit mode = %q, want shadow", e.Details.Mode)
		}
		for _, w := range e.Details.Warnings {
			if w.Kind == "automaton" && w.ID == first.ID {
				shadowNamesFirst = true
			}
		}
	}
	if !shadowNamesFirst {
		t.Fatalf("no automaton_create_search entry for %s names the first %s; entries=%+v", second.ID, first.ID, entries)
	}

	// 3. The mode is turned on through PUT /api/config.
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(`{"autonomous.automation.create_search_mode":"on"}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT create_search_mode on: HTTP %d, body=%s", rr.Code, rr.Body.String())
	}
	if got := s.createSearchSettings().EffectiveCreateSearchMode(); got != config.CreateSearchModeOn {
		t.Fatalf("mode after PUT = %q, want on", got)
	}

	// 4. On: a third Automaton with the first one's spec carries warnings
	// that name the first.
	third, _ := create(t, overlapSmokeSpec1)
	var thirdWarning *autonomous.OverlapWarning
	for i := range third.OverlapWarnings {
		if third.OverlapWarnings[i].Kind == "automaton" && third.OverlapWarnings[i].ID == first.ID {
			thirdWarning = &third.OverlapWarnings[i]
		}
	}
	if thirdWarning == nil {
		t.Fatalf("on-mode reply does not name the first %s: %+v", first.ID, third.OverlapWarnings)
	}

	// 5. The read route with the same text gives the same warning object.
	read := overlapReadGET(s, url.Values{"spec": {overlapSmokeSpec1}})
	if read.Code != http.StatusOK {
		t.Fatalf("overlap read: HTTP %d, body=%s", read.Code, read.Body.String())
	}
	reply := overlapReadDecode(t, read)
	if reply.Mode != config.CreateSearchModeOn {
		t.Errorf("read mode = %q, want on", reply.Mode)
	}
	var readWarning *autonomous.OverlapWarning
	for i := range reply.Warnings {
		if reply.Warnings[i].Kind == "automaton" && reply.Warnings[i].ID == first.ID {
			readWarning = &reply.Warnings[i]
		}
	}
	if readWarning == nil {
		t.Fatalf("overlap read does not name the first %s: %+v", first.ID, reply.Warnings)
	}
	if !reflect.DeepEqual(*thirdWarning, *readWarning) {
		t.Errorf("warning differs between create reply and read:\ncreate: %+v\nread:   %+v", *thirdWarning, *readWarning)
	}
}

// overlapSmokeAuditEntries reads every line of the audit log in dir.
func overlapSmokeAuditEntries(t *testing.T, dir string) []overlapSmokeAuditLine {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatalf("open audit.log: %v", err)
	}
	defer f.Close() //nolint:errcheck
	var out []overlapSmokeAuditLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e overlapSmokeAuditLine
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("audit line does not parse: %v (line %q)", err, sc.Text())
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	return out
}

// TestOverlapSmokeLive is the opt-in check against a sandbox daemon. It is
// read-only: it never creates an Automaton.
func TestOverlapSmokeLive(t *testing.T) {
	base := strings.TrimRight(os.Getenv(overlapSmokeLiveEnv), "/")
	if base == "" {
		t.Skipf("set %s to the sandbox daemon base URL to run the live overlap check", overlapSmokeLiveEnv)
	}
	if !strings.HasPrefix(base, "https://") {
		t.Fatalf("%s must be an https:// sandbox URL, got %q", overlapSmokeLiveEnv, base)
	}
	hc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: os.Getenv("DW_BASE_INSECURE") != "0", // #nosec G402 -- sandbox daemons use self-signed certs
			},
		},
	}
	get := func(path string) []byte {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, base+path, nil)
		if err != nil {
			t.Fatalf("build GET %s: %v", path, err)
		}
		if tok := os.Getenv("DW_BASE_TOKEN"); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close() //nolint:errcheck
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: HTTP %d, body=%s", path, resp.StatusCode, body)
		}
		return body
	}

	var cfg struct {
		Autonomous struct {
			Automation struct {
				CreateSearchMode string `json:"create_search_mode"`
			} `json:"automation"`
		} `json:"autonomous"`
	}
	if err := json.Unmarshal(get("/api/config"), &cfg); err != nil {
		t.Fatalf("config does not parse: %v", err)
	}
	switch mode := cfg.Autonomous.Automation.CreateSearchMode; mode {
	case config.CreateSearchModeOff, config.CreateSearchModeShadow, config.CreateSearchModeOn:
		t.Logf("sandbox create_search_mode = %s", mode)
	default:
		t.Fatalf("create_search_mode = %q, want off, shadow or on", mode)
	}

	var reply overlapReadReply
	q := url.Values{"title": {"Live overlap smoke probe"}, "spec": {overlapSmokeSpec1}}
	if err := json.Unmarshal(get("/api/autonomous/overlap?"+q.Encode()), &reply); err != nil {
		t.Fatalf("overlap read does not parse: %v", err)
	}
	if reply.Threshold <= 0 || reply.Threshold > 1 {
		t.Fatalf("overlap threshold = %v, want in (0, 1]", reply.Threshold)
	}
	if reply.Warnings == nil {
		t.Fatal("overlap read has no warnings list")
	}
	t.Logf("sandbox overlap read: mode %s, threshold %v, %d warnings", reply.Mode, reply.Threshold, len(reply.Warnings))
}
