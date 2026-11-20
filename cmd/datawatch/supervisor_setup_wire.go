package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/planrecords"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
	"github.com/dmz006/datawatch/packs"
)

// supervisorProjectPackFile is where a project keeps its own rules pack,
// relative to the project directory (the layout of the default pack).
const supervisorProjectPackFile = "packs/build-supervisor/rules.md"

// wireSupervisorSetup gives the supervisor set-up, state and corrections
// routes their real dependencies. plans and blockedReason are read lazily
// because the plan records and the autonomous manager start after the HTTP
// server. Without this the routes answer 503.
func wireSupervisorSetup(hs *server.HTTPServer, cfg *config.Config, sched *session.ScheduleStore,
	plans func() *planrecords.Store, blockedReason func(automatonID string) string) {
	if hs == nil || cfg == nil || sched == nil {
		return
	}
	clock := supervisorLoopClock{}
	records := supervisorloop.NewStateRecords(&supervisorPinnedFile{path: filepath.Join(expandHome(cfg.DataDir), "supervisor_state.json")}, clock)
	hs.SetSupervisorSetup(&server.SupervisorSetupDeps{
		Session:     supervisorSessionLayer{cfg: cfg},
		Rules:       supervisorRulesSource{cfg: cfg},
		Plans:       supervisorPlanSource{plans: plans, blockedReason: blockedReason},
		Overrides:   supervisorNoOverrides{},
		Schedules:   sched,
		Records:     records,
		Corrections: supervisorloop.NewCorrections(records, nil, nil, clock),
		Clock:       clock,
	})
}

// supervisorSessionLayer resolves a project name to its registered directory.
type supervisorSessionLayer struct{ cfg *config.Config }

func (l supervisorSessionLayer) ProjectDir(project string) (string, error) {
	e, ok := l.cfg.Projects[project]
	if !ok || strings.TrimSpace(e.Dir) == "" {
		return "", fmt.Errorf("no project named %q is registered", project)
	}
	dir := expandHome(e.Dir)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("project directory %q is not a directory", dir)
	}
	return dir, nil
}

func (supervisorSessionLayer) SupervisorProfile(string) (string, error) {
	return supervisorloop.LaunchRoleSupervisor, nil
}

// supervisorRulesSource serves the embedded default pack, the project's own
// pack and its rules file from the project directory.
type supervisorRulesSource struct{ cfg *config.Config }

func (supervisorRulesSource) DefaultPack() (string, error) {
	b, err := packs.BuildSupervisor.ReadFile("build-supervisor/rules.md")
	return string(b), err
}

func (r supervisorRulesSource) readProjectFile(project, rel string) (string, bool, error) {
	dir, err := supervisorSessionLayer(r).ProjectDir(project)
	if err != nil {
		return "", false, err
	}
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(b), true, nil
}

func (r supervisorRulesSource) ProjectPack(project string) (string, bool, error) {
	text, ok, err := r.readProjectFile(project, supervisorProjectPackFile)
	if err != nil || !ok {
		return "", false, err
	}
	// A project that carries the default pack itself adds nothing.
	if def, derr := (supervisorRulesSource{}).DefaultPack(); derr == nil && text == def {
		return "", false, nil
	}
	return text, true, nil
}

func (r supervisorRulesSource) ProjectRulesFile(project string) (string, bool, error) {
	name := strings.TrimSpace(r.cfg.Autonomous.RulesContext.RulesFile)
	if name == "" {
		name = "AGENT.md"
	}
	return r.readProjectFile(project, name)
}

// supervisorPlanSource reads the project's plans from the plan records.
type supervisorPlanSource struct {
	plans         func() *planrecords.Store
	blockedReason func(string) string
}

func (p supervisorPlanSource) ProjectPlans(project string) ([]planrecords.PlanDigest, error) {
	if p.plans == nil || p.plans() == nil {
		return nil, nil
	}
	return p.plans().ProjectPlans(project, planrecords.Page{}, planrecords.Page{}).Plans, nil
}

func (p supervisorPlanSource) BlockedReason(_, automatonID string) (string, error) {
	if p.blockedReason == nil {
		return "", nil
	}
	return p.blockedReason(automatonID), nil
}

// supervisorNoOverrides: the daemon keeps no per-project supervisor
// overrides yet, so every project reads the defaults (set-up still carries
// the model it was asked for).
type supervisorNoOverrides struct{}

func (supervisorNoOverrides) SupervisorOverrides(string) (supervisorloop.SupervisorOverrides, bool, error) {
	return supervisorloop.SupervisorOverrides{}, false, nil
}

// supervisorPinnedFile keeps the pinned state entries in one JSON file under
// the data directory.
type supervisorPinnedFile struct {
	mu   sync.Mutex
	path string
}

func (f *supervisorPinnedFile) load() (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("decode %s: %w", f.path, err)
		}
	}
	return m, nil
}

func (f *supervisorPinnedFile) LoadPinned(key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", false, err
	}
	v, ok := m[key]
	return v, ok, nil
}

func (f *supervisorPinnedFile) SavePinned(key, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = text
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (*supervisorPinnedFile) Recall(string) ([]string, error) { return nil, nil }
