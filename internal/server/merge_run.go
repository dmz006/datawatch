// Merge run route: POST /api/autonomous/merge/run.
//
// It is the daemon's entry point to the merge pipeline. The route builds a
// mergepipeline.Runner for the configured worktree base repo, with the Manager
// as the recorder, so every step lands in "pipeline_runs" of
// GET /api/autonomous/merge. With no pipeline file and no daemon record the
// built-in default pipeline runs, which reproduces the merge helper script.
// The Runner lives in package mergepipeline because that package imports
// autonomous, so autonomous cannot import it back.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/deploy"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// mergeRunTimeout bounds one whole merge run.
const mergeRunTimeout = 60 * time.Minute

// mergeRunner is the Manager surface the run route needs.
type mergeRunner interface {
	autonomous.MergeRunRecorder
	MergeBaseRepo() string
	MergeQuestionStore() *autonomous.QuestionStore
}

// mergeRunBody is the POST body.
type mergeRunBody struct {
	// Project names the project for per-project settings; empty uses the
	// base name of the worktree base repo.
	Project  string   `json:"project"`
	Title    string   `json:"title"`
	Branches []string `json:"branches"`
}

// mergeRunMu allows one merge run at a time.
var mergeRunMu sync.Mutex

// mergeDataDir is the daemon data directory, as the rest of the server finds it.
func mergeDataDir() string {
	if d := os.Getenv("DATAWATCH_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch")
}

// newMergeRunner builds the Runner for one project from the operator's
// settings. Nothing here names a repository or path: the project directory is
// the configured worktree base repo, and the version files, conflict policy
// and pipeline sources are autonomous.merge_pipeline settings.
func newMergeRunner(mr mergeRunner, cfg config.MergePipelineConfig, deployCfg config.DeployConfig, repo, project string) *mergepipeline.Runner {
	var records *mergepipeline.RecordStore
	if rs, err := mergepipeline.NewRecordStore(mergeDataDir()); err == nil {
		records = rs
	}
	resolver := &mergepipeline.Resolver{
		Records:    records,
		ProjectDir: func(string) (string, bool) { return repo, true },
		Settings: func(p string) mergepipeline.Settings {
			return mergepipeline.Settings{
				FileName:          cfg.EffectiveFileName(p),
				SourceOrder:       cfg.EffectiveSourceOrder(p),
				FallbackOnBadFile: cfg.EffectiveFallbackOnBadFile(p),
			}
		},
	}
	runner := &mergepipeline.Runner{
		Dir: repo, Project: project, Resolver: resolver,
		VersionFiles: cfg.EffectiveVersionFiles(project),
		Ship: mergepipeline.ExecShipRunner{
			DeployConfig: deployCfg,
			NewDeploy:    newDeployRunner,
		},
		Recorder: mr,
		ConflictSettings: func() mergepipeline.ConflictSettings {
			s := mergepipeline.DefaultConflictSettings()
			// The model is not wired here, so the model policy degrades to
			// stop_and_ask exactly as ResolveConflictPolicy describes.
			policy, _, _ := cfg.ResolveConflictPolicy(project, nil)
			s.Policy = mergepipeline.ConflictPolicy(policy)
			return s
		},
	}
	if q := mr.MergeQuestionStore(); q != nil {
		runner.Questions = q
	}
	return runner
}

// handleAutonomousMergeRun serves POST /api/autonomous/merge/run. It runs the
// merge synchronously and answers with the recorded run. A step that fails
// answers 200 with "result":"failed" and the steps that ran, so the caller
// sees the record; only a request that could not start is an error status.
func (s *Server) handleAutonomousMergeRun(w http.ResponseWriter, r *http.Request) {
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
	var body mergeRunBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	repo := mr.MergeBaseRepo()
	if repo == "" {
		http.Error(w, autonomous.ErrMergeNoBaseRepo.Error(), http.StatusServiceUnavailable)
		return
	}
	project := body.Project
	if project == "" {
		project = filepath.Base(repo)
	}
	if !mergeRunMu.TryLock() {
		http.Error(w, "a merge is already running", http.StatusConflict)
		return
	}
	defer mergeRunMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), mergeRunTimeout)
	defer cancel()
	res, err := newMergeRunner(mr, s.mergePipelineConfig(), s.deployConfig(), repo, project).Run(ctx, mergepipeline.RunRequest{
		Title: body.Title, Branches: body.Branches,
	})
	out := map[string]any{"run": res.Run}
	if err != nil {
		out["error"] = err.Error()
	}
	writeJSONOK(w, out)
}

// mergePipelineConfig returns the operator's autonomous.merge_pipeline block.
func (s *Server) mergePipelineConfig() config.MergePipelineConfig {
	if s.cfg == nil {
		return config.MergePipelineConfig{}
	}
	return s.cfg.Autonomous.MergePipeline.Clone()
}

// deployConfig returns the operator's autonomous.deploy block.
func (s *Server) deployConfig() config.DeployConfig {
	if s.cfg == nil {
		return config.DeployConfig{}
	}
	return s.cfg.Autonomous.Deploy.Clone()
}

// noFinalGateDrain reports no running final-gate task: the daemon has no
// final-gate counter to read yet, so the drain wait never blocks.
type noFinalGateDrain struct{}

func (noFinalGateDrain) RunningFinalGate(context.Context) (int, error) { return 0, nil }

// noRestartRecord records nothing about sessions or leases before a restart.
type noRestartRecord struct{}

func (noRestartRecord) RunningSessionIDs(context.Context) ([]string, error) { return nil, nil }
func (noRestartRecord) HeldLeaseCount(context.Context) (int, error)         { return 0, nil }

// newDeployRunner builds the deploy sequence Runner for dir.
func newDeployRunner(dir string, cfg config.DeployConfig) *deploy.Runner {
	data := mergeDataDir()
	return &deploy.Runner{
		Config: cfg, Dir: dir, DataDir: data,
		Cmd:     deploy.ExecCommandRunner{},
		Drain:   noFinalGateDrain{},
		Clock:   deploy.SystemClock{},
		Store:   deploy.FileStore{Path: filepath.Join(data, "deploy", "state.json")},
		Restart: noRestartRecord{},
	}
}
