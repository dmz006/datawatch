// GET /api/git/secret-scan/stats — the daemon's in-memory pre-push
// secret-scan counters plus the effective git.secret_scan mode.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/git"
)

// secretScanStatsTestServer gives the handler a zero-value config so the
// effective mode is the default (shadow) — the handler dereferences s.cfg.
func secretScanStatsTestServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t, nil, nil)
	s.cfg = &config.Config{}
	return s
}

// seedSecretScanCounters drives the package's counters through the same
// exported hooks the push path uses, so the test reads exactly what a live
// scan would have recorded.
func seedSecretScanCounters() {
	git.RecordSecretScanRun()
	git.RecordSecretScanFindings(2)
	git.RecordSecretScanShadowReport()
	git.RecordSecretScanRefusal()
}

func TestGitSecretScanStats_GET200_ExpectedKeys(t *testing.T) {
	seedSecretScanCounters()
	s := secretScanStatsTestServer(t)
	rr := httptest.NewRecorder()
	s.handleGitSecretScanStats(rr, httptest.NewRequest(http.MethodGet, "/api/git/secret-scan/stats", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}
	var body struct {
		Mode  string `json:"mode"`
		Stats struct {
			Runs          int64  `json:"runs"`
			Findings      int64  `json:"findings"`
			ShadowReports int64  `json:"shadow_reports"`
			Refusals      int64  `json:"refusals"`
			LastRunAt     string `json:"last_run_at"`
			LastRefusalAt string `json:"last_refusal_at"`
		} `json:"stats"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rr.Body.String())
	}
	if body.Mode != "shadow" {
		t.Errorf("mode=%q, want the default \"shadow\"", body.Mode)
	}
	// The counters are process-wide and other tests may have run before
	// this one, so assert at-least the seeded values, not equality.
	if body.Stats.Runs < 1 || body.Stats.Findings < 2 || body.Stats.ShadowReports < 1 || body.Stats.Refusals < 1 {
		t.Errorf("stats below seeded values: %+v", body.Stats)
	}
	if body.Stats.LastRunAt == "" || body.Stats.LastRefusalAt == "" {
		t.Errorf("timestamps empty: %+v", body.Stats)
	}
}

func TestGitSecretScanStats_POST405(t *testing.T) {
	s := secretScanStatsTestServer(t)
	rr := httptest.NewRecorder()
	s.handleGitSecretScanStats(rr, httptest.NewRequest(http.MethodPost, "/api/git/secret-scan/stats", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s, want 405", rr.Code, rr.Body.String())
	}
}

func TestGitSecretScanStats_FederationCap(t *testing.T) {
	s := secretScanStatsTestServer(t)

	// A peer without the health read capability (the session-viewer preset
	// grants session/agent reads only) is refused with 403.
	denyRR := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/git/secret-scan/stats", nil)
	s.handleGitSecretScanStats(denyRR, asPeer(req, "session-viewer"))
	if denyRR.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403 for a peer lacking health:read", denyRR.Code, denyRR.Body.String())
	}

	// A monitor peer (holds the health read capability) gets the stats.
	okRR := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/git/secret-scan/stats", nil)
	s.handleGitSecretScanStats(okRR, asPeer(req, "monitor"))
	if okRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s, want 200 for a monitor peer", okRR.Code, okRR.Body.String())
	}
}
