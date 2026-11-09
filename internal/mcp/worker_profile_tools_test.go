// Tests for the worker profile MCP surface: the read-only worker_profile_status
// tool, the config_set keys for autonomous.worker_profile*, and the Automaton
// set_worker_profile tool. They use the same loopback daemon stand-in pattern
// as the other MCP config tests.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/workerprofile"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// workerProfileStatsDaemon serves GET /api/stats with a worker_startup block.
func workerProfileStatsDaemon(t *testing.T) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/stats" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"worker_startup":{
			"average_startup_tokens_by_profile":{"digest":120.5,"full":900},
			"average_startup_tokens_by_role":{"worker":120.5}}}`))
	}))
	t.Cleanup(srv.Close)
	return &Server{webPort: bl427Port(t, srv)}
}

// TestWorkerProfileStatus_NamesDefaultsAndStartup: the status tool returns
// the built-in profile names, the built-in defaults per role and kind, and
// the latest start-up averages from the stats block.
func TestWorkerProfileStatus_NamesDefaultsAndStartup(t *testing.T) {
	s := workerProfileStatsDaemon(t)
	res, err := s.handleWorkerProfileStatus(context.Background(), subUpdateReq("worker_profile_status", nil))
	if err != nil {
		t.Fatalf("worker_profile_status: %v", err)
	}
	if res.IsError {
		t.Fatalf("worker_profile_status returned an error result: %+v", res.Content)
	}
	var out struct {
		Profiles     []string          `json:"profiles"`
		RoleDefaults map[string]string `json:"role_defaults"`
		KindDefaults map[string]string `json:"kind_defaults"`
		Startup      struct {
			ByProfile map[string]float64 `json:"average_startup_tokens_by_profile"`
			ByRole    map[string]float64 `json:"average_startup_tokens_by_role"`
		} `json:"startup"`
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode status %q: %v", text, err)
	}
	wantNames := []string{"datawatch-tools", "digest", "full", "minimal"}
	if !reflect.DeepEqual(out.Profiles, wantNames) {
		t.Errorf("profiles = %v, want %v", out.Profiles, wantNames)
	}
	if !sort.StringsAreSorted(out.Profiles) {
		t.Errorf("profiles not sorted: %v", out.Profiles)
	}
	for _, c := range []struct{ role, want string }{{"planner", "full"}, {"worker", "digest"}, {"verifier", "digest"}, {"judge", "minimal"}} {
		if got := out.RoleDefaults[c.role]; got != c.want {
			t.Errorf("role default %s = %q, want %q", c.role, got, c.want)
		}
	}
	for _, c := range []struct{ kind, want string }{{"implementation", "digest"}, {"docs", "digest"}, {"note", "minimal"}, {"final_gate", "digest"}} {
		if got := out.KindDefaults[c.kind]; got != c.want {
			t.Errorf("kind default %s = %q, want %q", c.kind, got, c.want)
		}
	}
	if !reflect.DeepEqual(out.RoleDefaults, workerprofile.RoleDefaults()) || !reflect.DeepEqual(out.KindDefaults, workerprofile.KindDefaults()) {
		t.Errorf("defaults do not match workerprofile accessors")
	}
	if out.Startup.ByProfile["digest"] != 120.5 || out.Startup.ByProfile["full"] != 900 || out.Startup.ByRole["worker"] != 120.5 {
		t.Errorf("startup averages = %+v, want digest 120.5, full 900, worker 120.5", out.Startup)
	}
}

// TestWorkerProfileStatus_NoStartupBlock: a stats snapshot without the block
// reads as empty averages, never an error.
func TestWorkerProfileStatus_NoStartupBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"cpu":1}`))
	}))
	t.Cleanup(srv.Close)
	s := &Server{webPort: bl427Port(t, srv)}
	res, _ := s.handleWorkerProfileStatus(context.Background(), subUpdateReq("worker_profile_status", nil))
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res.Content)
	}
	text := res.Content[0].(mcpsdk.TextContent).Text
	if !strings.Contains(text, `"average_startup_tokens_by_profile": {}`) {
		t.Errorf("empty startup block not reported as empty maps: %s", text)
	}
}

// TestWorkerProfileStatus_StatsUnavailable: with no web server the tool
// returns an error result instead of guessing.
func TestWorkerProfileStatus_StatsUnavailable(t *testing.T) {
	s := &Server{}
	res, _ := s.handleWorkerProfileStatus(context.Background(), subUpdateReq("worker_profile_status", nil))
	if !res.IsError {
		t.Fatalf("want an error result when the stats block cannot be read")
	}
}

// workerProfileConfigDaemon is a PUT /api/config stand-in that, like the real
// daemon, refuses an autonomous.worker_profile name that is neither built in
// nor user-defined and a worker_profiles value that is not an object, and
// stores only what it accepted.
type workerProfileConfigDaemon struct {
	mu     sync.Mutex
	config map[string]any
}

func newWorkerProfileConfigServer(t *testing.T) (*workerProfileConfigDaemon, *Server) {
	t.Helper()
	d := &workerProfileConfigDaemon{config: map[string]any{}}
	userProfiles := map[string]bool{"mine": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" || json.Unmarshal(raw, &body) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if v, ok := body["autonomous.worker_profiles"]; ok {
			if _, isObj := v.(map[string]any); !isObj {
				http.Error(w, "autonomous.worker_profiles: must be an object mapping profile names to profile settings", http.StatusBadRequest)
				return
			}
		}
		if v, ok := body["autonomous.worker_profile"]; ok {
			name, isStr := v.(string)
			if !isStr {
				http.Error(w, "autonomous.worker_profile: must be a profile name", http.StatusBadRequest)
				return
			}
			if _, builtin := workerprofile.Builtin(name); !builtin && !userProfiles[name] {
				http.Error(w, "autonomous.worker_profile: "+name+" names no built-in or user-defined profile", http.StatusBadRequest)
				return
			}
		}
		for k, v := range body {
			d.config[k] = v
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return d, &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: true}}
}

// TestWorkerProfileConfigSet_RefusesInvalidWrite: config_set returns an error
// result for an unknown profile name and leaves the stored value unchanged;
// a valid name is stored.
func TestWorkerProfileConfigSet_RefusesInvalidWrite(t *testing.T) {
	d, s := newWorkerProfileConfigServer(t)

	ok, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": "autonomous.worker_profile", "value": "digest"}))
	if err != nil || ok.IsError {
		t.Fatalf("valid write refused: err=%v res=%+v", err, ok.Content)
	}
	if got := d.config["autonomous.worker_profile"]; got != "digest" {
		t.Fatalf("stored value = %v, want digest", got)
	}

	res, err := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": "autonomous.worker_profile", "value": "nope"}))
	if err != nil {
		t.Fatalf("config_set returned a Go error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("invalid write not refused: %+v", res.Content)
	}
	if !strings.Contains(res.Content[0].(mcpsdk.TextContent).Text, "nope") {
		t.Errorf("refusal does not name the bad profile: %+v", res.Content)
	}
	if got := d.config["autonomous.worker_profile"]; got != "digest" {
		t.Errorf("refused write changed the stored value to %v", got)
	}
}

// TestWorkerProfileConfigSet_DefaultsAndProfilesKeys: the defaults and user
// profiles keys are writable through config_set, and a malformed object is
// refused without storing anything.
func TestWorkerProfileConfigSet_DefaultsAndProfilesKeys(t *testing.T) {
	d, s := newWorkerProfileConfigServer(t)

	defaults := `{"role":{"worker":"full"},"kind":{"note":"digest"}}`
	res, _ := s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": "autonomous.worker_profile_defaults", "value": defaults}))
	if res.IsError {
		t.Fatalf("worker_profile_defaults write refused: %+v", res.Content)
	}
	if _, ok := d.config["autonomous.worker_profile_defaults"]; !ok {
		t.Fatalf("worker_profile_defaults not stored")
	}

	profiles := `{"mine":{"name":"mine","model":"x"}}`
	res, _ = s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": "autonomous.worker_profiles", "value": profiles}))
	if res.IsError {
		t.Fatalf("worker_profiles write refused: %+v", res.Content)
	}
	if _, ok := d.config["autonomous.worker_profiles"]; !ok {
		t.Fatalf("worker_profiles not stored")
	}

	res, _ = s.handleConfigSet(context.Background(), subUpdateReq("config_set", map[string]any{"key": "autonomous.worker_profiles", "value": "{not json"}))
	if !res.IsError {
		t.Fatalf("malformed worker_profiles not refused")
	}
	if got, _ := d.config["autonomous.worker_profiles"].(map[string]any); got == nil || got["mine"] == nil {
		t.Errorf("refused worker_profiles write changed the stored value: %v", d.config["autonomous.worker_profiles"])
	}
}

// TestAutonomousPRDSetWorkerProfile_ForwardsAndRefuses: the Automaton setter
// posts the name to the daemon, and an empty id is refused before any call.
func TestAutonomousPRDSetWorkerProfile_ForwardsAndRefuses(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.URL.Path, string(raw)
		if strings.Contains(string(raw), "bogus") {
			http.Error(w, "worker profile \"bogus\" is neither a built-in nor a user-defined profile", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"id":"abc12345","worker_profile":"digest"}`))
	}))
	t.Cleanup(srv.Close)
	s := &Server{webPort: bl427Port(t, srv)}

	res, _ := s.handleAutonomousPRDSetWorkerProfile(context.Background(), subUpdateReq("autonomous_prd_set_worker_profile", map[string]any{"id": "abc12345", "worker_profile": "digest"}))
	if res.IsError {
		t.Fatalf("valid set refused: %+v", res.Content)
	}
	if gotPath != "/api/autonomous/prds/abc12345/set_worker_profile" || !strings.Contains(gotBody, `"worker_profile":"digest"`) {
		t.Errorf("forwarded %s %s, want the set_worker_profile route with the name", gotPath, gotBody)
	}

	res, _ = s.handleAutonomousPRDSetWorkerProfile(context.Background(), subUpdateReq("autonomous_prd_set_worker_profile", map[string]any{"id": "abc12345", "worker_profile": "bogus"}))
	if !res.IsError {
		t.Fatalf("unknown profile not refused: %+v", res.Content)
	}

	res, _ = s.handleAutonomousPRDSetWorkerProfile(context.Background(), subUpdateReq("autonomous_prd_set_worker_profile", map[string]any{"worker_profile": "digest"}))
	if !res.IsError {
		t.Fatalf("missing id not refused")
	}
}
