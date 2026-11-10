// generated_artefacts.go — connects the Manager/executor to the change
// record (internal/changerecord) and the generated artefacts
// (internal/genartifacts).
//
//   - When a task starts the executor remembers the base ref; when the task
//     finishes it builds a ChangeRecord from the git diff since that ref plus
//     the verification commands the task ran, and saves it. A record error
//     is logged and never fails the task.
//   - When the Automaton reaches the completion point (right before the
//     rules review) the task records are merged and every enabled artefact
//     kind is written; one line per kind is recorded on the PRD decisions.
//   - The rules review lists the generated artefacts that exist as present
//     context files and counts them toward the documentation checks.
//
// The whole feature is dormant until SetGeneratedArtefacts wires a settings
// provider (the daemon passes config.Autonomous.EffectiveGeneratedArtefacts).
package autonomous

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// generatedArtefactDecisionKind is the Decision.Kind of the one-line result
// recorded per artefact kind.
const generatedArtefactDecisionKind = "generated_artefact"

// generatedArtefactState is the Manager's generated-artefact wiring. The zero
// value is dormant (no settings provider).
type generatedArtefactState struct {
	mu       sync.Mutex
	settings func() config.EffectiveGeneratedArtefactsSettings
	bases    map[string]string                    // task id -> base ref
	cmds     map[string][]changerecord.CommandRun // task id -> commands run

	// Test seams; nil means the production implementation.
	headRef    func(ctx context.Context, dir string) string
	diffSource func(dir, base string) changerecord.DiffSource
	now        func() time.Time
}

// SetGeneratedArtefacts wires the settings provider. Nil switches the feature
// off. Called at daemon wiring time.
func (m *Manager) SetGeneratedArtefacts(fn func() config.EffectiveGeneratedArtefactsSettings) {
	m.genArt.mu.Lock()
	defer m.genArt.mu.Unlock()
	m.genArt.settings = fn
}

// generatedArtefactSettings returns the effective switches and whether the
// feature is wired at all.
func (m *Manager) generatedArtefactSettings() (config.EffectiveGeneratedArtefactsSettings, bool) {
	m.genArt.mu.Lock()
	fn := m.genArt.settings
	m.genArt.mu.Unlock()
	if fn == nil {
		return config.EffectiveGeneratedArtefactsSettings{}, false
	}
	return fn(), true
}

func (m *Manager) genNow() time.Time {
	m.genArt.mu.Lock()
	fn := m.genArt.now
	m.genArt.mu.Unlock()
	if fn != nil {
		return fn()
	}
	return time.Now().UTC()
}

// noteTaskStart remembers the base ref for a task (first call wins, so a
// retry keeps the original base).
func (m *Manager) noteTaskStart(ctx context.Context, prd *PRD, t *Task) {
	s, ok := m.generatedArtefactSettings()
	if !ok || !s.ChangeRecord {
		return
	}
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" {
		return
	}
	m.genArt.mu.Lock()
	if _, have := m.genArt.bases[t.ID]; have {
		m.genArt.mu.Unlock()
		return
	}
	head := m.genArt.headRef
	m.genArt.mu.Unlock()
	var ref string
	if head != nil {
		ref = head(ctx, dir)
	} else {
		ref = CapturePostVerifySHA(ctx, dir, m.driftGit)
	}
	if ref == "" {
		return
	}
	m.genArt.mu.Lock()
	if m.genArt.bases == nil {
		m.genArt.bases = map[string]string{}
	}
	if _, have := m.genArt.bases[t.ID]; !have {
		m.genArt.bases[t.ID] = ref
	}
	m.genArt.mu.Unlock()
}

// noteTaskCommand records one command a task ran (a verification script run).
// Output is parsed for pass/fail counts; unknown formats give zero counts.
func (m *Manager) noteTaskCommand(taskID, command string, exitCode int, output string) {
	if taskID == "" || command == "" {
		return
	}
	pass, fail := parseTestCounts(output)
	m.genArt.mu.Lock()
	defer m.genArt.mu.Unlock()
	if m.genArt.cmds == nil {
		m.genArt.cmds = map[string][]changerecord.CommandRun{}
	}
	m.genArt.cmds[taskID] = append(m.genArt.cmds[taskID], changerecord.CommandRun{
		Command: command, ExitCode: exitCode, Pass: pass, Fail: fail,
	})
}

// recordTaskChange builds and saves the task's change record. It never fails
// the task: any error is logged and swallowed.
func (m *Manager) recordTaskChange(prd *PRD, t *Task) {
	s, ok := m.generatedArtefactSettings()
	if !ok || !s.ChangeRecord {
		return
	}
	if err := m.buildAndSaveRecord(prd, t); err != nil {
		log.Printf("[autonomous] %s/%s change record: %v", prd.ID, t.ID, err)
	}
}

func (m *Manager) buildAndSaveRecord(prd *PRD, t *Task) error {
	m.genArt.mu.Lock()
	base := m.genArt.bases[t.ID]
	cmds := append([]changerecord.CommandRun{}, m.genArt.cmds[t.ID]...)
	mk := m.genArt.diffSource
	m.genArt.mu.Unlock()
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" || base == "" {
		return fmt.Errorf("no base ref or project dir recorded")
	}
	var src changerecord.DiffSource
	if mk != nil {
		src = mk(dir, base)
	} else {
		src = changerecord.GitDiff{Dir: dir, Base: base}
	}
	rec, err := changerecord.BuildFromDiff(src, cmds)
	if err != nil {
		return err
	}
	rec.TaskID = t.ID
	rec.AutomatonID = prd.ID
	rec.Timestamp = m.genNow()
	if err := changerecord.Save(m.dataDir, t.ID, rec); err != nil {
		return err
	}
	m.genArt.mu.Lock()
	delete(m.genArt.bases, t.ID)
	delete(m.genArt.cmds, t.ID)
	m.genArt.mu.Unlock()
	return nil
}

// generatedArtefactOptions builds the genartifacts options for a PRD.
func (m *Manager) generatedArtefactOptions(prd *PRD, s config.EffectiveGeneratedArtefactsSettings) genartifacts.Options {
	return genartifacts.Options{Root: prd.ProjectDir, Settings: &s}
}

// writeGeneratedArtefacts merges the PRD's task records and writes every
// enabled artefact kind, recording a one-line result per kind on the PRD. It
// never fails the completion: errors are logged and recorded as lines.
func (m *Manager) writeGeneratedArtefacts(prd *PRD) {
	s, ok := m.generatedArtefactSettings()
	if !ok || prd.ProjectDir == "" {
		return
	}
	var recs []*changerecord.ChangeRecord
	if s.ChangeRecord {
		for _, st := range prd.Story {
			for i := range st.Tasks {
				t := &st.Tasks[i]
				if t.Status != TaskCompleted {
					continue
				}
				r, err := changerecord.Load(m.dataDir, t.ID)
				if err != nil {
					continue // no record for this task (not an error: record may be off or failed)
				}
				recs = append(recs, r)
			}
		}
	}
	merged := changerecord.Merge(recs...)
	merged.AutomatonID = prd.ID
	if s.ChangeRecord {
		// Store the merged record under the Automaton id so the REST and MCP
		// reads (and a later regenerate) work from the same record. A save
		// error is logged and never fails the completion.
		if err := changerecord.Save(m.dataDir, prd.ID, merged); err != nil {
			log.Printf("[autonomous] %s save merged change record: %v", prd.ID, err)
		}
	}
	opts := m.generatedArtefactOptions(prd, s)
	var lines []string
	for _, k := range genartifacts.Kinds() {
		if !opts.Enabled(k) {
			continue
		}
		res, err := genartifacts.Generate(k, merged, opts)
		switch {
		case err != nil:
			log.Printf("[autonomous] %s generated artefact %s: %v", prd.ID, k, err)
			lines = append(lines, fmt.Sprintf("%s: error: %v", k, err))
		case res.Status == genartifacts.Written:
			lines = append(lines, fmt.Sprintf("%s: written %s", k, res.Path))
		default:
			lines = append(lines, fmt.Sprintf("%s: %s %s (%s)", k, res.Status, res.Path, res.Reason))
		}
	}
	if len(lines) == 0 {
		return
	}
	now := m.genNow()
	m.store.WithLock(func() {
		for _, l := range lines {
			prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: generatedArtefactDecisionKind, Actor: "autonomous", Note: l})
		}
	})
	if err := m.savePRDOwn(prd); err != nil { // executor-owned: the rollup's expectedRev must see this Rev bump
		log.Printf("[autonomous] %s save generated artefact results: %v", prd.ID, err)
	}
}

// generatedArtefactReviewText returns the rules-review prose about generated
// artefacts: the ones that exist on disk (as present context files) and the
// instruction to count them toward the documentation checks. Empty when the
// feature is not wired or no kind is enabled.
func (m *Manager) generatedArtefactReviewText(prd *PRD) string {
	s, ok := m.generatedArtefactSettings()
	if !ok || prd.ProjectDir == "" {
		return ""
	}
	opts := m.generatedArtefactOptions(prd, s)
	rec := &changerecord.ChangeRecord{AutomatonID: prd.ID}
	var present, missing []string
	for _, k := range genartifacts.Kinds() {
		if !opts.Enabled(k) {
			continue
		}
		p := genartifacts.TrackedPath(k, rec, opts)
		if fileExists(filepath.Join(prd.ProjectDir, filepath.FromSlash(p))) {
			present = append(present, p)
		} else {
			missing = append(missing, p)
		}
	}
	if len(present) == 0 && len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(" Generated artefacts: ")
	if len(present) > 0 {
		b.WriteString("also read these generated files, which exist as context files: " + strings.Join(present, ", ") + ". ")
	} else {
		b.WriteString("none exist yet. ")
	}
	b.WriteString("Generated changelog, parity, tracker and registry artefacts count toward the documentation checks:" +
		" a rule that asks for a changelog entry, a parity issue, tracker rows or a registry row is met when the matching generated artefact covers it.")
	if len(missing) > 0 {
		b.WriteString(" An enabled generated artefact that is missing is a finding; expected but missing: " + strings.Join(missing, ", ") + ".")
	}
	b.WriteString(" A generated kind that is switched off is not a finding.")
	return b.String()
}

var (
	reGoTestLine  = regexp.MustCompile(`(?m)^\s*--- (PASS|FAIL):`)
	reGoPkgOK     = regexp.MustCompile(`(?m)^ok\s+\S+`)
	reGoPkgFail   = regexp.MustCompile(`(?m)^FAIL\s+\S+`)
	reNodePass    = regexp.MustCompile(`(?m)^#\s*pass\s+(\d+)`)
	reNodeFail    = regexp.MustCompile(`(?m)^#\s*fail\s+(\d+)`)
	reJestPassed  = regexp.MustCompile(`(\d+) passed`)
	reJestFailed  = regexp.MustCompile(`(\d+) failed`)
	reJestSummary = regexp.MustCompile(`(?m)^Tests:\s.*$`)
)

// parseTestCounts reads pass and fail counts from go test and node test
// summary output. Unknown formats give zero counts.
func parseTestCounts(out string) (pass, fail int) {
	if m := reNodePass.FindStringSubmatch(out); m != nil {
		pass, _ = strconv.Atoi(m[1])
		if f := reNodeFail.FindStringSubmatch(out); f != nil {
			fail, _ = strconv.Atoi(f[1])
		}
		return pass, fail
	}
	if line := reJestSummary.FindString(out); line != "" {
		if m := reJestPassed.FindStringSubmatch(line); m != nil {
			pass, _ = strconv.Atoi(m[1])
		}
		if m := reJestFailed.FindStringSubmatch(line); m != nil {
			fail, _ = strconv.Atoi(m[1])
		}
		return pass, fail
	}
	if ms := reGoTestLine.FindAllStringSubmatch(out, -1); len(ms) > 0 {
		for _, m := range ms {
			if m[1] == "PASS" {
				pass++
			} else {
				fail++
			}
		}
		return pass, fail
	}
	return len(reGoPkgOK.FindAllString(out, -1)), len(reGoPkgFail.FindAllString(out, -1))
}
