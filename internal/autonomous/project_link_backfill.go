package autonomous

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DatawatchProjectName is the project name the link backfill treats as the
// datawatch project without any directory check.
const DatawatchProjectName = "datawatch"

// projectLinkBackfillDecisionKind is the decision-log kind of one link the
// backfill wrote.
const projectLinkBackfillDecisionKind = "project_link_backfill"

// projectLinkBackfillDoneFile is the durable done marker, kept in the
// autonomous data directory next to the stores.
const projectLinkBackfillDoneFile = "project_link_backfill.done"

// ProjectLinkProject is one configured project as the backfill sees it.
type ProjectLinkProject struct {
	// Dir is the project directory.
	Dir string
	// Repository is the project's repository directory, when it names one.
	Repository string
}

// ProjectLinkBackfillEntry is the decision for one Automaton.
type ProjectLinkBackfillEntry struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// Source is how the target was found: automation, parent or directory.
	// Empty on a skipped Automaton.
	Source string `json:"source,omitempty"`
	// Project is the target project. Empty on a skipped Automaton.
	Project string `json:"project,omitempty"`
	// Applied is true once the link was written and saved.
	Applied bool `json:"applied"`
	// Skipped is true when the Automaton was left alone; Reason says why.
	Skipped bool   `json:"skipped,omitempty"`
	Reason  string `json:"reason,omitempty"`
	// Error is set when writing this Automaton failed.
	Error string `json:"error,omitempty"`
}

// ProjectLinkBackfillCounts are the totals of one report.
type ProjectLinkBackfillCounts struct {
	// Scanned is the number of non-template Automata with an empty link.
	Scanned int `json:"scanned"`
	// Matched is how many of them resolved to the datawatch project.
	Matched int `json:"matched"`
	// Applied is how many links were written.
	Applied int `json:"applied"`
	// Skipped is how many were left alone.
	Skipped int `json:"skipped"`
	// Failed is how many writes failed.
	Failed int `json:"failed"`
}

// ProjectLinkBackfillReport is the outcome of one backfill pass.
type ProjectLinkBackfillReport struct {
	// Apply is true when the pass was allowed to write.
	Apply       bool                       `json:"apply"`
	GeneratedAt time.Time                  `json:"generated_at"`
	Project     string                     `json:"project,omitempty"`
	Entries     []ProjectLinkBackfillEntry `json:"entries"`
	Counts      ProjectLinkBackfillCounts  `json:"counts"`
}

// projectLinkBackfillState is the Manager's backfill state. The zero value is
// ready to use.
type projectLinkBackfillState struct {
	mu       sync.Mutex
	projects func() map[string]ProjectLinkProject
	save     func(*PRD) error // nil = the store's SavePRD
	latest   *ProjectLinkBackfillReport
	run      sync.Mutex // serialises passes
}

// SetProjectLinkProjectsFn installs the reader of the configured projects the
// backfill matches against. Nil means no projects are known.
func (m *Manager) SetProjectLinkProjectsFn(fn func() map[string]ProjectLinkProject) {
	m.projectLink.mu.Lock()
	defer m.projectLink.mu.Unlock()
	m.projectLink.projects = fn
}

// LatestProjectLinkBackfill returns the most recent report (dry-run or apply)
// and whether one exists, so surfaces can read it.
func (m *Manager) LatestProjectLinkBackfill() (ProjectLinkBackfillReport, bool) {
	m.projectLink.mu.Lock()
	defer m.projectLink.mu.Unlock()
	if m.projectLink.latest == nil {
		return ProjectLinkBackfillReport{}, false
	}
	return *m.projectLink.latest, true
}

// datawatchProjectTarget picks the datawatch project among the configured
// projects: the project named datawatch, else the first (by name) whose
// directory or repository is the datawatch repository. It returns the name and
// the directory the resolver matches against.
func datawatchProjectTarget(projects map[string]ProjectLinkProject, baseRepo string) (name, dir string) {
	dirOf := func(p ProjectLinkProject) string {
		if strings.TrimSpace(p.Dir) != "" {
			return p.Dir
		}
		return p.Repository
	}
	if p, ok := projects[DatawatchProjectName]; ok {
		return DatawatchProjectName, dirOf(p)
	}
	base := strings.TrimSpace(baseRepo)
	if base == "" {
		return "", ""
	}
	want := RepositoryDirOf(base)
	names := make([]string, 0, len(projects))
	for n := range projects {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := projects[n]
		for _, d := range []string{p.Dir, p.Repository} {
			if strings.TrimSpace(d) == "" || !filepath.IsAbs(d) {
				continue
			}
			if RepositoryDirOf(d) == want || filepath.Clean(d) == filepath.Clean(base) {
				return n, dirOf(p)
			}
		}
	}
	return "", ""
}

// ProjectLinkBackfill decides, for every non-template Automaton with an empty
// project link, whether it belongs to the datawatch project, using the
// membership resolver's sources in order: the Automation's project, the
// parent's project, then the repository directory (a worktree maps to its
// repository). Automata that already carry a link are never changed;
// Automata that match nothing are reported as skipped and left alone.
//
// The report is computed first and stored as the latest report. With apply
// false nothing is written. With apply true each match gets its link set and
// saved and one decision-log entry; a failure on one Automaton is recorded and
// does not stop the rest. A second apply finds nothing to do and records
// nothing. It only reads and writes Automaton records.
func (m *Manager) ProjectLinkBackfill(apply bool) ProjectLinkBackfillReport {
	m.projectLink.run.Lock()
	defer m.projectLink.run.Unlock()

	report, targets := m.planProjectLinkBackfill()
	report.Apply = apply
	m.storeProjectLinkReport(report)
	if !apply {
		return report
	}
	for i := range report.Entries {
		e := &report.Entries[i]
		if e.Skipped {
			continue
		}
		if err := m.applyProjectLink(targets[e.ID], e); err != nil {
			e.Error = err.Error()
			report.Counts.Failed++
			log.Printf("[autonomous] project link backfill: %s: %v", e.ID, err)
			continue
		}
		e.Applied = true
		report.Counts.Applied++
	}
	m.storeProjectLinkReport(report)
	return report
}

func (m *Manager) storeProjectLinkReport(r ProjectLinkBackfillReport) {
	cp := r
	cp.Entries = append([]ProjectLinkBackfillEntry(nil), r.Entries...)
	m.projectLink.mu.Lock()
	m.projectLink.latest = &cp
	m.projectLink.mu.Unlock()
}

// planProjectLinkBackfill computes the report without writing. targets maps an
// Automaton id to its record for the apply step.
func (m *Manager) planProjectLinkBackfill() (ProjectLinkBackfillReport, map[string]*PRD) {
	report := ProjectLinkBackfillReport{GeneratedAt: time.Now(), Entries: []ProjectLinkBackfillEntry{}}
	targets := map[string]*PRD{}

	m.projectLink.mu.Lock()
	fn := m.projectLink.projects
	m.projectLink.mu.Unlock()
	var projects map[string]ProjectLinkProject
	if fn != nil {
		projects = fn()
	}
	name, dir := datawatchProjectTarget(projects, m.Config().WorktreeBaseRepo)
	report.Project = name

	prds := m.store.ListPRDs()
	var automations []*Automation
	if m.automations != nil {
		automations = m.automations.List()
	}
	// The resolver only knows the datawatch project, so every source is
	// restricted to the case that identifies it.
	restricted := map[string]string{}
	if name != "" {
		restricted[name] = dir
	}
	idx := NewAutomatonProjectIndex(prds, automations, restricted)

	sort.Slice(prds, func(i, j int) bool { return prds[i].ID < prds[j].ID })
	for _, p := range prds {
		if p == nil || p.IsTemplate || strings.TrimSpace(p.Project) != "" {
			continue
		}
		report.Counts.Scanned++
		e := ProjectLinkBackfillEntry{ID: p.ID, Title: p.Title}
		mem := idx.Of(p.ID)
		switch {
		case name == "":
			e.Skipped, e.Reason = true, "no datawatch project is configured"
		case mem.Project == "":
			e.Skipped, e.Reason = true, "matches no datawatch source (Automation, parent or directory)"
		default:
			e.Source, e.Project = mem.Source, mem.Project
			report.Counts.Matched++
			targets[p.ID] = p
		}
		if e.Skipped {
			report.Counts.Skipped++
		}
		report.Entries = append(report.Entries, e)
	}
	return report, targets
}

// applyProjectLink sets one link, saves it and writes the decision entry. On a
// failed save the in-memory record is put back so it stays consistent with the
// store.
func (m *Manager) applyProjectLink(p *PRD, e *ProjectLinkBackfillEntry) error {
	if p == nil {
		return fmt.Errorf("record not found")
	}
	m.projectLink.mu.Lock()
	save := m.projectLink.save
	m.projectLink.mu.Unlock()
	if save == nil {
		save = m.store.SavePRD
	}
	var prevDecisions []Decision
	skip := false
	m.store.WithLock(func() {
		if strings.TrimSpace(p.Project) != "" { // linked since the plan
			skip = true
			return
		}
		prevDecisions = p.Decisions
		p.Project = e.Project
		p.Decisions = trimDecisions(append(append([]Decision(nil), p.Decisions...), Decision{
			At:    time.Now(),
			Kind:  projectLinkBackfillDecisionKind,
			Actor: "autonomous",
			Note:  fmt.Sprintf("linked this Automaton to project %q by %s via backfill", e.Project, e.Source),
		}))
		p.UpdatedAt = time.Now()
	})
	if skip {
		e.Skipped, e.Reason = true, "already linked"
		return nil
	}
	if err := save(p); err != nil {
		m.store.WithLock(func() {
			p.Project = ""
			p.Decisions = prevDecisions
		})
		return err
	}
	return nil
}

// ProjectLinkBackfillDone reports whether the durable done marker exists.
func (m *Manager) ProjectLinkBackfillDone() bool {
	_, err := os.Stat(filepath.Join(m.store.dir, projectLinkBackfillDoneFile))
	return err == nil
}

// StartProjectLinkBackfill serves autonomous.project_link_backfill_apply. It
// runs once at daemon start after the stores have loaded. With apply false only
// the dry-run report is produced. With apply true the pass is applied once and,
// when every write succeeded, a durable done marker is written so later starts
// only produce the dry-run report. It reads and writes Automaton records only.
func (m *Manager) StartProjectLinkBackfill(apply bool) {
	if apply && !m.ProjectLinkBackfillDone() {
		r := m.ProjectLinkBackfill(true)
		b, _ := json.Marshal(r.Counts)
		log.Printf("[autonomous] project_link_backfill_apply: %s", b)
		if r.Counts.Failed == 0 {
			marker, _ := json.Marshal(map[string]any{"done_at": time.Now().UTC(), "counts": r.Counts})
			if err := os.WriteFile(filepath.Join(m.store.dir, projectLinkBackfillDoneFile), marker, 0o600); err != nil {
				log.Printf("[autonomous] project_link_backfill_apply: done marker: %v", err)
			}
		}
		return
	}
	r := m.ProjectLinkBackfill(false)
	b, _ := json.Marshal(r.Counts)
	log.Printf("[autonomous] project link backfill dry-run: %s", b)
}
