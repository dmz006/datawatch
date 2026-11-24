// Package pause is the operator's "paused" service: a daemon-level state,
// global or per project, that stops new work from being admitted without
// stopping schedules, the daemon or the autonomous manager. Running tasks
// finish. Only the operator, through a surface (CLI, REST, MCP, chat, web or
// a federation peer), can clear a pause; the autonomous manager only ever
// sees the read side (Gate), and the store itself refuses a resume from any
// actor that names a worker, watcher, supervisor or model.
package pause

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scope says what a pause covers.
type Scope string

const (
	// ScopeGlobal pauses admission of new work everywhere.
	ScopeGlobal Scope = "global"
	// ScopeProject pauses admission for one project directory (and any
	// directory inside it).
	ScopeProject Scope = "project"
)

// Audit action names the store reports through its audit hook.
const (
	ActionPause         = "pause"
	ActionResume        = "resume"
	ActionResumeRefused = "resume_refused"
)

// ActorConfig is the actor recorded for a pause the daemon applied itself
// because pause.start_paused is on.
const ActorConfig = "config"

// Errors returned by the store.
var (
	ErrReasonRequired  = errors.New("a pause needs a reason; say why work is being paused")
	ErrProjectRequired = errors.New("a project pause needs a project")
	ErrBadScope        = errors.New("scope must be global or project")
	ErrNotOperator     = errors.New("only the operator can resume paused work; a worker, watcher, supervisor or model cannot clear a pause")
)

// Entry is one active pause.
type Entry struct {
	Scope   Scope     `json:"scope"`
	Project string    `json:"project,omitempty"`
	Reason  string    `json:"reason"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
}

// Event is one audited change.
type Event struct {
	Action  string
	Scope   Scope
	Project string
	Reason  string
	Actor   string
	At      time.Time
}

// Block is why a start was refused.
type Block struct {
	Scope   Scope
	Project string
	Reason  string
	By      string
	At      time.Time
}

// Message is the refusal text shown to whoever tried to start work.
func (b Block) Message() string {
	where := "all work"
	if b.Scope == ScopeProject {
		where = "project " + b.Project
	}
	return fmt.Sprintf("paused by operator (%s): %s (by %s since %s); the operator resumes it",
		where, b.Reason, b.By, b.At.UTC().Format(time.RFC3339))
}

// Snapshot is the readable state.
type Snapshot struct {
	Paused   bool    `json:"paused"`
	Global   *Entry  `json:"global,omitempty"`
	Projects []Entry `json:"projects,omitempty"`
}

// Gate is the read-only view the autonomous manager, lane fill and the task
// starter use. It has no way to resume.
type Gate interface {
	Blocked(projectDir string) (Block, bool)
}

type fileState struct {
	Global   *Entry           `json:"global,omitempty"`
	Projects map[string]Entry `json:"projects,omitempty"`
}

// Store holds the pause state and persists it.
type Store struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	st    fileState
	audit func(Event)
}

// Open reads the state file at path (creating nothing until the first
// change). A missing file is an unpaused store, or a globally paused one when
// startPaused is set; a file that exists but cannot be read is an error, so a
// damaged file never reads as "not paused".
func Open(path string, startPaused bool, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	s := &Store{path: path, now: now}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.st); err != nil {
			return nil, fmt.Errorf("pause state %s unreadable: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		if startPaused {
			s.st.Global = &Entry{Scope: ScopeGlobal, Reason: "daemon started paused (pause.start_paused)", By: ActorConfig, At: now().UTC()}
			if err := s.saveLocked(); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("pause state %s: %w", path, err)
	}
	return s, nil
}

// OpenFailClosed is Open for the daemon: when the state file cannot be read it
// returns a store that is globally paused (by the config actor, with the read
// error as the reason) together with the error, so a damaged file stops new
// work instead of silently clearing the operator's pause. The first resume
// rewrites the file.
func OpenFailClosed(path string, startPaused bool, now func() time.Time) (*Store, error) {
	s, err := Open(path, startPaused, now)
	if err == nil {
		return s, nil
	}
	if now == nil {
		now = time.Now
	}
	s = &Store{path: path, now: now}
	s.st.Global = &Entry{Scope: ScopeGlobal, Reason: "pause state unreadable, paused until the operator resumes: " + err.Error(), By: ActorConfig, At: now().UTC()}
	return s, err
}

// SetAudit sets the hook that receives every pause, resume and refused resume.
func (s *Store) SetAudit(fn func(Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = fn
}

// NormalizeProject returns the key a project directory is stored under.
func NormalizeProject(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	return filepath.Clean(dir)
}

// inside reports whether dir is project or lies inside it.
func inside(dir, project string) bool {
	if dir == project {
		return true
	}
	return strings.HasPrefix(dir, strings.TrimSuffix(project, string(filepath.Separator))+string(filepath.Separator))
}

// Pause records a pause. Pausing again replaces the reason and author.
func (s *Store) Pause(scope Scope, project, reason, actor string) (Entry, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Entry{}, ErrReasonRequired
	}
	project = NormalizeProject(project)
	switch scope {
	case ScopeGlobal:
		project = ""
	case ScopeProject:
		if project == "" {
			return Entry{}, ErrProjectRequired
		}
	default:
		return Entry{}, ErrBadScope
	}
	if strings.TrimSpace(actor) == "" {
		actor = "operator"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := Entry{Scope: scope, Project: project, Reason: reason, By: actor, At: s.now().UTC()}
	prevG, prevP := s.st.Global, s.st.Projects
	if scope == ScopeGlobal {
		s.st.Global = &e
	} else {
		next := map[string]Entry{}
		for k, v := range s.st.Projects {
			next[k] = v
		}
		next[project] = e
		s.st.Projects = next
	}
	if err := s.saveLocked(); err != nil {
		s.st.Global, s.st.Projects = prevG, prevP
		return Entry{}, err
	}
	s.emitLocked(Event{Action: ActionPause, Scope: scope, Project: project, Reason: reason, Actor: actor, At: e.At})
	return e, nil
}

// operatorActor reports whether actor may clear a pause. The rule is by
// exclusion: anything naming a worker, watcher, supervisor, model, agent,
// session or the autonomous manager is refused, and so is a blank actor.
func operatorActor(actor string) bool {
	a := strings.ToLower(strings.TrimSpace(actor))
	if a == "" {
		return false
	}
	for _, p := range []string{"doer", "fast-doer", "watcher", "supervisor", "llm", "model", "agent", "session", "autonomous", "claude", "worker", "task"} {
		if a == p || strings.HasPrefix(a, p+":") || strings.HasPrefix(a, p+"-") || strings.HasPrefix(a, p+"/") {
			return false
		}
	}
	return true
}

// Resume clears one pause. It reports false when that scope was not paused.
func (s *Store) Resume(scope Scope, project, actor, note string) (bool, error) {
	project = NormalizeProject(project)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !operatorActor(actor) {
		s.emitLocked(Event{Action: ActionResumeRefused, Scope: scope, Project: project, Reason: note, Actor: actor, At: s.now().UTC()})
		return false, ErrNotOperator
	}
	prevG, prevP := s.st.Global, s.st.Projects
	cleared := false
	switch scope {
	case ScopeGlobal:
		if s.st.Global != nil {
			s.st.Global, cleared = nil, true
		}
	case ScopeProject:
		if project == "" {
			return false, ErrProjectRequired
		}
		if _, ok := s.st.Projects[project]; ok {
			next := map[string]Entry{}
			for k, v := range s.st.Projects {
				if k != project {
					next[k] = v
				}
			}
			s.st.Projects, cleared = next, true
		}
	default:
		return false, ErrBadScope
	}
	if !cleared {
		return false, nil
	}
	if err := s.saveLocked(); err != nil {
		s.st.Global, s.st.Projects = prevG, prevP
		return false, err
	}
	s.emitLocked(Event{Action: ActionResume, Scope: scope, Project: project, Reason: note, Actor: actor, At: s.now().UTC()})
	return true, nil
}

// ResumeAll clears the global pause and every project pause in one action
// and returns how many were cleared.
func (s *Store) ResumeAll(actor, note string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !operatorActor(actor) {
		s.emitLocked(Event{Action: ActionResumeRefused, Reason: note, Actor: actor, At: s.now().UTC()})
		return 0, ErrNotOperator
	}
	n := len(s.st.Projects)
	if s.st.Global != nil {
		n++
	}
	if n == 0 {
		return 0, nil
	}
	prev := s.st
	s.st = fileState{}
	if err := s.saveLocked(); err != nil {
		s.st = prev
		return 0, err
	}
	s.emitLocked(Event{Action: ActionResume, Reason: note, Actor: actor, At: s.now().UTC()})
	return n, nil
}

// Blocked reports whether starting new work in projectDir is paused. A global
// pause blocks every directory, including an empty one; a project pause
// blocks that directory and anything inside it. The global pause wins when
// both apply.
func (s *Store) Blocked(projectDir string) (Block, bool) {
	dir := NormalizeProject(projectDir)
	s.mu.Lock()
	defer s.mu.Unlock()
	if g := s.st.Global; g != nil {
		return Block{Scope: ScopeGlobal, Reason: g.Reason, By: g.By, At: g.At}, true
	}
	if dir == "" {
		return Block{}, false
	}
	keys := make([]string, 0, len(s.st.Projects))
	for k := range s.st.Projects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if inside(dir, k) {
			e := s.st.Projects[k]
			return Block{Scope: ScopeProject, Project: k, Reason: e.Reason, By: e.By, At: e.At}, true
		}
	}
	return Block{}, false
}

// Snapshot returns the current state for status surfaces.
func (s *Store) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Snapshot{}
	if s.st.Global != nil {
		g := *s.st.Global
		out.Global = &g
	}
	keys := make([]string, 0, len(s.st.Projects))
	for k := range s.st.Projects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Projects = append(out.Projects, s.st.Projects[k])
	}
	out.Paused = out.Global != nil || len(out.Projects) > 0
	return out
}

func (s *Store) emitLocked(e Event) {
	if s.audit != nil {
		s.audit(e)
	}
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.path, data)
}

// writeFile writes atomically: a temp file in the same directory, then rename.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pause-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
