// REST surface for vision.describe_timeout_seconds: the stored
// per-image-describe deadline must round-trip PUT -> GET /api/config,
// the GET map must always report the effective value (5s when unset),
// and invalid values (below 1 or non-numeric) must be refused without
// touching the stored value — same rule as results.schedule_history_limit.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func newVisionTimeoutServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(t.TempDir(), "config.yaml")}
}

func getVisionTimeoutSeconds(t *testing.T, s *Server) int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/config: %d", rr.Code)
	}
	var got struct {
		Vision struct {
			DescribeTimeoutSeconds int `json:"describe_timeout_seconds"`
		} `json:"vision"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Vision.DescribeTimeoutSeconds
}

// An unset value reads back as the default (5), and a valid PUT must be
// applied and read back exactly.
func TestVisionDescribeTimeoutSeconds_RestRoundTrip(t *testing.T) {
	s := newVisionTimeoutServer(t)

	if got := getVisionTimeoutSeconds(t, s); got != 5 {
		t.Fatalf("default effective value: got %d, want 5", got)
	}

	// JSON numbers arrive as float64 (what the Settings card and the CLI
	// send).
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"vision.describe_timeout_seconds": float64(30)})
	if got := getVisionTimeoutSeconds(t, s); got != 30 {
		t.Fatalf("after float PUT: got %d, want 30", got)
	}
	if got := s.cfg.Vision.EffectiveDescribeTimeout().Seconds(); got != 30 {
		t.Fatalf("effective duration: got %vs, want 30s", got)
	}

	// Bare ints must work too.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"vision.describe_timeout_seconds": 2})
	if got := getVisionTimeoutSeconds(t, s); got != 2 {
		t.Fatalf("after int PUT: got %d, want 2", got)
	}
}

// Invalid values are refused: the PUT is accepted (unknown-to-the-value
// but a known key) and applies nothing, so the getter keeps reporting the
// prior effective value — never 0 or a negative timeout.
func TestVisionDescribeTimeoutSeconds_PutRefusesBadValues(t *testing.T) {
	for name, val := range map[string]any{
		"zero":        float64(0),
		"negative":    float64(-3),
		"sub-second":  float64(0.5),
		"non-numeric": "lots",
		"wrong type":  []string{"5"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newVisionTimeoutServer(t)
			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"vision.describe_timeout_seconds": float64(30)})
			if got := getVisionTimeoutSeconds(t, s); got != 30 {
				t.Fatalf("baseline PUT: got %d, want 30", got)
			}

			putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"vision.describe_timeout_seconds": val})
			if got := s.cfg.Vision.DescribeTimeoutSeconds; got != 30 {
				t.Fatalf("refused value must not apply: stored %d, want 30", got)
			}
			if got := getVisionTimeoutSeconds(t, s); got != 30 {
				t.Fatalf("after refused PUT: GET reports %d, want 30", got)
			}
		})
	}

	// Fresh config: an invalid value must not drop the effective timeout
	// below the default either.
	t.Run("from default", func(t *testing.T) {
		s := newVisionTimeoutServer(t)
		putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"vision.describe_timeout_seconds": 0})
		if got := s.cfg.Vision.DescribeTimeoutSeconds; got != 0 {
			t.Fatalf("refused value must not apply: stored %d, want 0 (unset)", got)
		}
		if got := getVisionTimeoutSeconds(t, s); got != 5 {
			t.Fatalf("getter must keep reporting the default: got %d, want 5", got)
		}
	})
}
