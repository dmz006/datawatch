// Deploy API: GET /api/autonomous/deploy and POST /api/autonomous/deploy/run.
//
// GET reports the effective autonomous.deploy settings, the phase and outcome
// of the last deploy record, why and when a deploy was deferred, and the
// snapshot's time and SHA-256. It never returns the snapshot contents. POST
// starts the deploy sequence for the current build now, the same step the
// merge pipeline runs after a ship.

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

const deployKeyPrefix = "autonomous.deploy."

// deployRunTimeout bounds one deploy run started over REST.
const deployRunTimeout = 60 * time.Minute

// deployConfigSurface is the autonomous.deploy block as GET /api/config shows
// it: the commands as text, and the two timeouts with their defaults filled in.
func deployConfigSurface(c config.DeployConfig) map[string]any {
	drain, health := c.DrainTimeout, c.HealthTimeout
	if strings.TrimSpace(drain) == "" {
		drain = c.EffectiveDrainTimeout().String()
	}
	if strings.TrimSpace(health) == "" {
		health = c.EffectiveHealthTimeout().String()
	}
	return map[string]any{
		"build_command":   c.BuildCommand,
		"install_command": c.InstallCommand,
		"binary_path":     c.BinaryPath,
		"restart_command": c.RestartCommand,
		"health_check":    c.HealthCheck,
		"drain_timeout":   drain,
		"health_timeout":  health,
	}
}

// applyDeployKey stores one key's value in c. known reports whether key is in
// the block; when err is not nil, c is unchanged.
func applyDeployKey(c *config.DeployConfig, key string, v any) (known bool, err error) {
	if !strings.HasPrefix(key, deployKeyPrefix) {
		return false, nil
	}
	var dst *string
	switch strings.TrimPrefix(key, deployKeyPrefix) {
	case "build_command":
		dst = &c.BuildCommand
	case "install_command":
		dst = &c.InstallCommand
	case "binary_path":
		dst = &c.BinaryPath
	case "restart_command":
		dst = &c.RestartCommand
	case "health_check":
		dst = &c.HealthCheck
	case "drain_timeout":
		dst = &c.DrainTimeout
	case "health_timeout":
		dst = &c.HealthTimeout
	default:
		return true, fmt.Errorf("%s: unknown key; valid keys are build_command, install_command, binary_path, restart_command, health_check, drain_timeout, health_timeout", key)
	}
	if v == nil {
		*dst = ""
		return true, nil
	}
	str, ok := v.(string)
	if !ok {
		return true, fmt.Errorf("%s: must be a string", key)
	}
	*dst = strings.TrimSpace(str)
	return true, nil
}

// validateDeployPatch checks the autonomous.deploy keys of a PUT /api/config
// body against the block as it would stand after the patch.
func validateDeployPatch(cur config.DeployConfig, patch map[string]any) error {
	next := cur.Clone()
	touched := false
	for k, v := range patch {
		known, err := applyDeployKey(&next, k, v)
		if err != nil {
			return err
		}
		touched = touched || known
	}
	if !touched {
		return nil
	}
	return next.Validate()
}

// applyDeployPatch writes one deploy key into the daemon config.
func applyDeployPatch(cfg *config.Config, key string, v any) bool {
	known, _ := applyDeployKey(&cfg.Autonomous.Deploy, key, v)
	return known
}

// deployStatusBody is the GET answer.
type deployStatusBody struct {
	Settings map[string]any `json:"settings"`
	Phase    string         `json:"phase,omitempty"`
	Outcome  string         `json:"outcome,omitempty"`
	Deferred *deployDefer   `json:"deferred,omitempty"`
	Snapshot *deploySnap    `json:"snapshot,omitempty"`
	Running  bool           `json:"running"`
}

type deployDefer struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// deploySnap is the snapshot's identity only, never its contents.
type deploySnap struct {
	Time   time.Time `json:"time"`
	SHA256 string    `json:"sha256"`
}

// lastDeployOutcome reads the last line of the outcome file.
func lastDeployOutcome(path string) string {
	f, err := os.Open(path) // #nosec G304 -- daemon data dir
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var last deploy.OutcomeRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var r deploy.OutcomeRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			last = r
		}
	}
	return last.Outcome
}

// handleAutonomousDeploy serves GET /api/autonomous/deploy.
func (s *Server) handleAutonomousDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	data := mergeDataDir()
	body := deployStatusBody{Settings: deployConfigSurface(s.deployConfig())}
	if mergeRunMu.TryLock() {
		mergeRunMu.Unlock()
	} else {
		body.Running = true
	}
	st, ok, err := deploy.FileStore{Path: filepath.Join(data, "deploy", "state.json")}.Load()
	if err == nil && ok {
		body.Phase = string(st.Phase)
		if st.Phase == deploy.PhaseDeferred {
			body.Deferred = &deployDefer{Reason: st.Reason, At: st.UpdatedAt}
		}
		if st.SnapshotHash != "" {
			body.Snapshot = &deploySnap{Time: st.SnapshotTime, SHA256: st.SnapshotHash}
		}
	}
	body.Outcome = lastDeployOutcome(filepath.Join(data, "deploy", "outcomes.jsonl"))
	writeJSONOK(w, body)
}

// handleAutonomousDeployRun serves POST /api/autonomous/deploy/run. It shares
// the merge run lock, so a merge or a deploy already running answers 409.
func (s *Server) handleAutonomousDeployRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousWrite) {
		return
	}
	mr, ok := s.autonomousMgr.(mergeRunner)
	if !ok {
		http.Error(w, "Automaton merge unavailable", http.StatusServiceUnavailable)
		return
	}
	repo := mr.MergeBaseRepo()
	if repo == "" {
		http.Error(w, autonomous.ErrMergeNoBaseRepo.Error(), http.StatusServiceUnavailable)
		return
	}
	if !mergeRunMu.TryLock() {
		http.Error(w, "a merge or deploy is already running", http.StatusConflict)
		return
	}
	defer mergeRunMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), deployRunTimeout)
	defer cancel()
	cfg, restart := s.deployConfig(), s.restartSource()
	res, err := mergepipeline.ExecShipRunner{
		DeployConfig: cfg,
		NewDeploy: func(dir string, c config.DeployConfig) *deploy.Runner {
			return newDeployRunner(dir, c, restart)
		},
	}.Deploy(ctx, repo)
	out := map[string]any{"status": string(res.Status), "note": res.Note}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSONOK(w, out)
}
