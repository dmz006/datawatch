// The standing check of closed work: a daemon read that flags every Automaton
// that closes or archives without an outcome class, finished work with no
// shipped record, shipped work with no deploy record on a host that deploys
// after ship, and a deployed version older than the shipped one. Once a day it
// also re-runs the intent proof on work closed as delivered without a shipped
// twin: every named file and symbol of the Automaton's spec must be on the base
// branch and every task complete. The mode is autonomous.closed_outcome_check:
// shadow (the default) records findings for the read, on also raises one alert
// per finding, off does nothing. The check only reads; it never changes a record.
package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding kinds of the standing check.
const (
	FindingClosedNoOutcome    = "closed_no_outcome"
	FindingCompletedNoShipped = "completed_no_shipped"
	FindingShippedNoDeploy    = "shipped_no_deploy"
	FindingDeployedOlder      = "deployed_older_than_shipped"
	FindingDaemonOlder        = "daemon_older_than_shipped"
	FindingIntentUnproven     = "intent_unproven"
)

const (
	// outcomeCheckInterval is how often the scheduled run happens.
	outcomeCheckInterval = 24 * time.Hour
	// outcomeDeployGrace is how long after a merge a missing deploy record is
	// not yet a finding: the deploy step runs on the supervisor loop's schedule.
	outcomeDeployGrace = 2 * time.Hour
	// outcomeProofLimit bounds the records one run re-proves.
	outcomeProofLimit = 500
	outcomeCheckFile  = "closed_outcome_check.json"
	proofGitTimeout   = 20 * time.Second
)

// OutcomeFinding is one thing the check flags.
type OutcomeFinding struct {
	// Automaton is the record id; empty for a finding about the daemon.
	Automaton string    `json:"automaton"`
	Title     string    `json:"title,omitempty"`
	Status    PRDStatus `json:"status,omitempty"`
	Kind      string    `json:"kind"`
	Detail    string    `json:"detail"`
}

// ProofSummary counts the last intent proof run.
type ProofSummary struct {
	Checked  int `json:"checked"`
	Proven   int `json:"proven"`
	Partial  int `json:"partial"`
	Unproven int `json:"unproven"`
}

// OutcomeCheckReport is the check's result.
type OutcomeCheckReport struct {
	Mode    string         `json:"mode"`
	RunAt   time.Time      `json:"run_at"`
	Scanned int            `json:"scanned"`
	Counts  map[string]int `json:"counts"`
	Proof   ProofSummary   `json:"proof"`
	// Findings is never null.
	Findings         []OutcomeFinding `json:"findings"`
	DeploysAfterShip bool             `json:"deploys_after_ship"`
	RunningVersion   string           `json:"running_version,omitempty"`
}

// OutcomeCheckEnv is what the daemon wiring tells the check about its host.
type OutcomeCheckEnv struct {
	// DeploysAfterShip says this host deploys after ship; nil means it does not.
	DeploysAfterShip func() bool
	// RunningVersion is the version of the running daemon; nil means unknown.
	RunningVersion func() string
}

// SetOutcomeCheckEnv installs the host facts the check reads.
func (m *Manager) SetOutcomeCheckEnv(env OutcomeCheckEnv) {
	m.mu.Lock()
	m.outcomeEnv = env
	m.mu.Unlock()
}

// versionParts parses v1.2.3 (or 1.2.3, with a -pre or +build tail) into its
// numbers.
func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, false
	}
	var out []int
	for _, s := range strings.Split(v, ".") {
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// versionLess reports a < b; ok is false when either does not parse.
func versionLess(a, b string) (less, ok bool) {
	pa, oka := versionParts(a)
	pb, okb := versionParts(b)
	if !oka || !okb {
		return false, false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y, true
		}
	}
	return false, true
}

func sameVersion(a, b string) bool {
	l1, ok1 := versionLess(a, b)
	l2, ok2 := versionLess(b, a)
	if ok1 && ok2 {
		return !l1 && !l2
	}
	return strings.TrimPrefix(strings.TrimSpace(a), "v") == strings.TrimPrefix(strings.TrimSpace(b), "v")
}

// RecordReleaseDeployed writes the deploy record onto every Automaton that
// shipped in rec.Release and returns the ids it changed. A repeat of the same
// deploy changes nothing. It never touches a status.
func (m *Manager) RecordReleaseDeployed(rec DeployRecord) ([]string, error) {
	rec.Release, rec.Version = strings.TrimSpace(rec.Release), strings.TrimSpace(rec.Version)
	if rec.Release == "" {
		return nil, fmt.Errorf("deploy record: release is required")
	}
	if rec.Version == "" {
		return nil, fmt.Errorf("deploy record: version is required")
	}
	if rec.Health == "" {
		rec.Health = DeployHealthUnchecked
	}
	if !ValidDeployHealth(rec.Health) {
		return nil, fmt.Errorf("deploy record: health must be passed, failed or unchecked, got %q", rec.Health)
	}
	if strings.TrimSpace(rec.Host) == "" {
		h, _ := os.Hostname()
		rec.Host = h
	}
	if rec.At.IsZero() {
		rec.At = m.holdNow()
	}
	rec.At = rec.At.UTC()
	var ids []string
	for _, p := range m.store.ListPRDs() {
		var match bool
		m.readLocked(func() { match = !p.IsTemplate && hasShippedRecord(p) && sameVersion(p.MergedInRelease, rec.Release) })
		if !match {
			continue
		}
		changed := false
		m.store.MutatePRD(p.ID, func(q *PRD) bool {
			if d := q.Deploy; d != nil && d.Version == rec.Version && d.Host == rec.Host && d.Health == rec.Health && d.At.Equal(rec.At) {
				return false
			}
			cp := rec
			q.Deploy = &cp
			changed = true
			return true
		})
		if changed {
			ids = append(ids, p.ID)
			m.EmitPRDUpdate(p.ID)
		}
	}
	return ids, nil
}

func (m *Manager) outcomeEnvSnapshot() (deploys bool, running string) {
	m.mu.Lock()
	env := m.outcomeEnv
	m.mu.Unlock()
	if env.DeploysAfterShip != nil {
		deploys = env.DeploysAfterShip()
	}
	if env.RunningVersion != nil {
		running = env.RunningVersion()
	}
	return deploys, running
}

// closedLabel is the outcome the record carries or its own facts prove.
func closedLabel(p *PRD) string {
	if p.ClosedOutcome != "" {
		return p.ClosedOutcome
	}
	if hasShippedRecord(p) {
		return OutcomeDelivered
	}
	if p.Status != PRDArchived {
		o, _ := deriveClosedOutcome(p)
		return o
	}
	return ""
}

// cheapFindings runs the checks that read only the records.
func (m *Manager) cheapFindings(now time.Time) (OutcomeCheckReport, []*PRD) {
	deploys, running := m.outcomeEnvSnapshot()
	rep := OutcomeCheckReport{
		Mode: m.Config().EffectiveClosedOutcomeCheck(), RunAt: now.UTC(),
		Counts: map[string]int{}, Findings: []OutcomeFinding{},
		DeploysAfterShip: deploys, RunningVersion: running,
	}
	prds := m.store.ListPRDs()
	var newest string
	add := func(p *PRD, kind, detail string) {
		f := OutcomeFinding{Kind: kind, Detail: detail}
		if p != nil {
			f.Automaton, f.Title, f.Status = p.ID, p.Title, p.Status
		}
		rep.Findings = append(rep.Findings, f)
		rep.Counts[kind]++
	}
	m.readLocked(func() {
		for _, p := range prds {
			if p == nil || p.IsTemplate {
				continue
			}
			rep.Scanned++
			shipped := hasShippedRecord(p)
			if shipped && (newest == "" || func() bool { l, ok := versionLess(newest, p.MergedInRelease); return ok && l }()) {
				newest = strings.TrimSpace(p.MergedInRelease)
			}
			if closedWorkStatus(p.Status) && closedLabel(p) == "" {
				if NormalizePRDStatus(p.Status) == PRDCompleted {
					add(p, FindingCompletedNoShipped, "completed with no shipped record and no outcome: record where its work went (delivered, superseded, dropped or held)")
				} else {
					add(p, FindingClosedNoOutcome, fmt.Sprintf("%s with no outcome: record delivered, superseded, dropped or held", NormalizePRDStatus(p.Status)))
				}
			}
			if !shipped {
				continue
			}
			if deploys && (p.MergedAt == nil || now.Sub(*p.MergedAt) >= outcomeDeployGrace) {
				switch {
				case p.Deploy == nil:
					add(p, FindingShippedNoDeploy, fmt.Sprintf("shipped in %s and never deployed on this host", strings.TrimSpace(p.MergedInRelease)))
				case p.Deploy.Health == DeployHealthFailed:
					add(p, FindingShippedNoDeploy, fmt.Sprintf("shipped in %s; the last deploy failed its health check (%s)", strings.TrimSpace(p.MergedInRelease), p.Deploy.Detail))
				}
			}
			if p.Deploy != nil {
				if less, ok := versionLess(p.Deploy.Version, p.MergedInRelease); ok && less {
					add(p, FindingDeployedOlder, fmt.Sprintf("deployed %s on %s, older than the shipped %s", p.Deploy.Version, p.Deploy.Host, strings.TrimSpace(p.MergedInRelease)))
				}
			}
		}
	})
	if deploys && running != "" && newest != "" {
		if less, ok := versionLess(running, newest); ok && less {
			add(nil, FindingDaemonOlder, fmt.Sprintf("this host runs %s, older than the newest shipped release %s", running, newest))
		}
	}
	sortFindings(rep.Findings)
	return rep, prds
}

func sortFindings(fs []OutcomeFinding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Kind != fs[j].Kind {
			return fs[i].Kind < fs[j].Kind
		}
		return fs[i].Automaton < fs[j].Automaton
	})
}

// OutcomeCheck is the live read: the record checks now, plus the intent proof
// findings of the last run that still hold. It changes nothing.
func (m *Manager) OutcomeCheck() OutcomeCheckReport {
	mode := m.Config().EffectiveClosedOutcomeCheck()
	if mode == ClosedOutcomeOff {
		return OutcomeCheckReport{Mode: mode, Counts: map[string]int{}, Findings: []OutcomeFinding{}}
	}
	rep, _ := m.cheapFindings(m.holdNow())
	last := m.LastOutcomeCheck()
	rep.Proof = last.Proof
	for _, f := range last.Findings {
		if f.Kind != FindingIntentUnproven {
			continue
		}
		if p, ok := m.store.GetPRD(f.Automaton); ok && p.ClosedOutcome == OutcomeDelivered && p.DeliveredVia == "" {
			rep.Findings = append(rep.Findings, f)
			rep.Counts[f.Kind]++
		}
	}
	sortFindings(rep.Findings)
	return rep
}

// LastOutcomeCheck is the report of the last run (zero before any).
func (m *Manager) LastOutcomeCheck() OutcomeCheckReport {
	m.mu.Lock()
	last := m.outcomeLast
	m.mu.Unlock()
	if last != nil {
		return *last
	}
	var rep OutcomeCheckReport
	if b, err := os.ReadFile(filepath.Join(m.dataDir, outcomeCheckFile)); err == nil && json.Unmarshal(b, &rep) == nil {
		m.mu.Lock()
		if m.outcomeLast == nil {
			m.outcomeLast = &rep
		}
		m.mu.Unlock()
	}
	if rep.Findings == nil {
		rep.Findings = []OutcomeFinding{}
	}
	if rep.Counts == nil {
		rep.Counts = map[string]int{}
	}
	return rep
}

// OutcomeCheckIfDue runs the check when the interval has passed since the last
// run (the last run is persisted, so a restart does not rerun it). It reports
// whether it ran. The coordinator's housekeeping pass calls it.
func (m *Manager) OutcomeCheckIfDue(ctx context.Context) bool {
	if m == nil || m.Config().EffectiveClosedOutcomeCheck() == ClosedOutcomeOff {
		return false
	}
	last := m.LastOutcomeCheck()
	if !last.RunAt.IsZero() && m.holdNow().Sub(last.RunAt) < outcomeCheckInterval {
		return false
	}
	m.RunOutcomeCheck(ctx)
	return true
}

// RunOutcomeCheck runs the record checks and the intent proof, stores and
// persists the report and, in mode on, raises one alert per new finding.
func (m *Manager) RunOutcomeCheck(ctx context.Context) OutcomeCheckReport {
	mode := m.Config().EffectiveClosedOutcomeCheck()
	if mode == ClosedOutcomeOff {
		return OutcomeCheckReport{Mode: mode, Counts: map[string]int{}, Findings: []OutcomeFinding{}}
	}
	rep, prds := m.cheapFindings(m.holdNow())
	m.runIntentProof(ctx, &rep, prds)
	sortFindings(rep.Findings)
	m.mu.Lock()
	m.outcomeLast = &rep
	m.mu.Unlock()
	if b, err := json.Marshal(rep); err == nil {
		_ = atomicWriteFile(filepath.Join(m.dataDir, outcomeCheckFile), b)
	}
	if mode == ClosedOutcomeOn {
		m.alertOutcomeFindings(rep)
	}
	return rep
}

// alertOutcomeFindings raises one alert per finding that was not alerted
// before, and forgets findings that no longer hold so a return alerts again.
func (m *Manager) alertOutcomeFindings(rep OutcomeCheckReport) {
	sink := m.AlertSink()
	m.mu.Lock()
	if m.outcomeAlerted == nil {
		m.outcomeAlerted = map[string]bool{}
	}
	live := map[string]bool{}
	var fresh []OutcomeFinding
	for _, f := range rep.Findings {
		k := f.Kind + "/" + f.Automaton
		live[k] = true
		if !m.outcomeAlerted[k] {
			m.outcomeAlerted[k] = true
			fresh = append(fresh, f)
		}
	}
	for k := range m.outcomeAlerted {
		if !live[k] {
			delete(m.outcomeAlerted, k)
		}
	}
	m.mu.Unlock()
	if sink == nil {
		return
	}
	for _, f := range fresh {
		subject := "daemon"
		if f.Automaton != "" {
			subject = "Automaton " + f.Automaton
		}
		sink("warn", "Closed work check: "+strings.ReplaceAll(f.Kind, "_", " "), subject+": "+f.Detail)
	}
}

// ---- intent proof ----

var (
	proofPathRe = regexp.MustCompile("(?:^|[^\\w/.-])((?:[\\w.-]+/)+[\\w.-]+\\.\\w{1,5})")
	proofTickRe = regexp.MustCompile("`([A-Za-z_][\\w.]{5,60})`")
	proofTestRe = regexp.MustCompile(`\b(Test\w{6,60})\b`)
)

// IntentEvidence returns the file paths and symbols a spec names.
func IntentEvidence(spec string) (paths, syms []string) {
	seenP, seenS := map[string]bool{}, map[string]bool{}
	for _, mm := range proofPathRe.FindAllStringSubmatch(spec, -1) {
		t := strings.TrimRight(mm[1], ".,;:)")
		if strings.HasPrefix(t, "http") || strings.HasPrefix(t, "/home") || strings.HasPrefix(t, "/tmp") || strings.Contains(t, "*") || seenP[t] {
			continue
		}
		seenP[t] = true
		paths = append(paths, t)
	}
	add := func(s string) {
		if seenS[s] || strings.Contains(s, "/") {
			return
		}
		upper, lower := strings.ToUpper(s) == s, strings.ToLower(s) == s
		if (!upper && !lower) || strings.HasPrefix(s, "Test") {
			seenS[s] = true
			syms = append(syms, s)
		}
	}
	for _, mm := range proofTickRe.FindAllStringSubmatch(spec, -1) {
		add(mm[1])
	}
	for _, mm := range proofTestRe.FindAllStringSubmatch(spec, -1) {
		add(mm[1])
	}
	sort.Strings(paths)
	sort.Strings(syms)
	return paths, syms
}

// IntentProof is the proof level of one record: proven (every named item on the
// base branch and every task done), partial or unproven (nothing named).
type IntentProof struct {
	Level   string
	Detail  string
	Missing []string
}

type proofRepo struct {
	files   []string
	fileSet map[string]bool
	pathOK  map[string]bool
	symOK   map[string]bool
}

func (m *Manager) loadProofRepo(ctx context.Context, git MergeGitRunner, repo, base string) (*proofRepo, error) {
	cctx, cancel := context.WithTimeout(ctx, proofGitTimeout)
	defer cancel()
	out, err := git.Run(cctx, repo, "ls-tree", "-r", "--name-only", base)
	if err != nil {
		return nil, err
	}
	pr := &proofRepo{fileSet: map[string]bool{}, pathOK: map[string]bool{}, symOK: map[string]bool{}}
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			pr.files = append(pr.files, f)
			pr.fileSet[f] = true
		}
	}
	return pr, nil
}

func (pr *proofRepo) hasPath(p string) bool {
	if v, ok := pr.pathOK[p]; ok {
		return v
	}
	ok := pr.fileSet[p]
	if !ok {
		for _, f := range pr.files {
			if strings.HasSuffix(f, "/"+p) {
				ok = true
				break
			}
		}
	}
	pr.pathOK[p] = ok
	return ok
}

// hasSym searches the base branch for the symbol; an error that is not the
// plain "no match" exit is returned so the record is skipped, not condemned.
func (pr *proofRepo) hasSym(ctx context.Context, git MergeGitRunner, repo, base, sym string) (bool, error) {
	if v, ok := pr.symOK[sym]; ok {
		return v, nil
	}
	cctx, cancel := context.WithTimeout(ctx, proofGitTimeout)
	defer cancel()
	out, err := git.Run(cctx, repo, "grep", "-l", "-F", "-e", sym, base, "--", "*.go", "*.js", "*.md", "*.yaml", "*.sh", "*.json")
	if err != nil && strings.TrimSpace(out) == "" && strings.Contains(err.Error(), "fatal") {
		return false, err
	}
	found := strings.TrimSpace(out) != ""
	pr.symOK[sym] = found
	return found, nil
}

func (m *Manager) proveIntent(ctx context.Context, git MergeGitRunner, repo, base string, pr *proofRepo, p *PRD) (IntentProof, error) {
	paths, syms := IntentEvidence(p.Spec)
	named := len(paths) + len(syms)
	var missing []string
	found := 0
	for _, x := range paths {
		if pr.hasPath(x) {
			found++
		} else {
			missing = append(missing, x)
		}
	}
	for _, s := range syms {
		ok, err := pr.hasSym(ctx, git, repo, base, s)
		if err != nil {
			return IntentProof{}, err
		}
		if ok {
			found++
		} else {
			missing = append(missing, s)
		}
	}
	tasks, done := 0, 0
	for _, t := range progressTasks(p) {
		tasks++
		if t.Status == TaskCompleted {
			done++
		}
	}
	switch {
	case named == 0:
		return IntentProof{Level: "unproven", Detail: "unproven: the spec names no file or symbol to look for on the base branch"}, nil
	case tasks > 0 && done < tasks:
		return IntentProof{Level: "partial", Missing: missing, Detail: fmt.Sprintf("partial: %d of %d tasks done; %d of %d named items on the base branch%s", done, tasks, found, named, missingText(missing))}, nil
	case found == named:
		return IntentProof{Level: "proven"}, nil
	}
	return IntentProof{Level: "partial", Missing: missing, Detail: fmt.Sprintf("partial: %d of %d named items on the base branch%s", found, named, missingText(missing))}, nil
}

func missingText(missing []string) string {
	if len(missing) == 0 {
		return ""
	}
	if len(missing) > 5 {
		missing = append(missing[:5:5], "...")
	}
	return " (missing: " + strings.Join(missing, ", ") + ")"
}

// runIntentProof re-proves closed work recorded as delivered with no shipped
// twin and no merge-step shipped record, and adds a finding for each that is
// not proven. Nothing is recorded on the Automata themselves.
func (m *Manager) runIntentProof(ctx context.Context, rep *OutcomeCheckReport, prds []*PRD) {
	repo := m.archivedBaseRepo()
	if repo == "" {
		return
	}
	var cands []*PRD
	m.readLocked(func() {
		for _, p := range prds {
			if p == nil || p.IsTemplate || !closedWorkStatus(p.Status) {
				continue
			}
			if p.ClosedOutcome == OutcomeDelivered && p.DeliveredVia == "" && !(hasShippedRecord(p) && p.ShippedSource == ShippedSourceMergeStep) {
				cands = append(cands, p)
			}
		}
	})
	if len(cands) == 0 {
		return
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].ID < cands[j].ID })
	if len(cands) > outcomeProofLimit {
		cands = cands[:outcomeProofLimit]
	}
	git := m.archivedUnmergedGitRunner()
	bctx, cancel := context.WithTimeout(ctx, proofGitTimeout)
	base, err := LaneFillBaseBranch(bctx, git, repo)
	cancel()
	if err != nil {
		return
	}
	pr, err := m.loadProofRepo(ctx, git, repo, base)
	if err != nil {
		return
	}
	for _, p := range cands {
		if ctx.Err() != nil {
			return
		}
		var snap *PRD
		m.readLocked(func() { cp := *p; snap = &cp })
		res, perr := m.proveIntent(ctx, git, repo, base, pr, snap)
		if perr != nil {
			continue
		}
		rep.Proof.Checked++
		switch res.Level {
		case "proven":
			rep.Proof.Proven++
			continue
		case "partial":
			rep.Proof.Partial++
		default:
			rep.Proof.Unproven++
		}
		rep.Findings = append(rep.Findings, OutcomeFinding{Automaton: p.ID, Title: p.Title, Status: p.Status, Kind: FindingIntentUnproven, Detail: "closed as delivered, but " + res.Detail})
		rep.Counts[FindingIntentUnproven]++
	}
}
