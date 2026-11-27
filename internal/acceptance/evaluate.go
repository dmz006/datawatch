package acceptance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// ProjectCheck is a saved, named command the operator registers.
type ProjectCheck struct {
	Command    string   `json:"command" yaml:"command"`
	Inputs     []string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	Expect     []Expect `json:"expect,omitempty" yaml:"expect,omitempty"`
	TimeoutSec int      `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// ProbeRequest asks a source about one item.
type ProbeRequest struct {
	RequestID  string
	Item       Item
	Subject    Subject
	Occurrence string
}

// Reading is what a source answered.
type Reading struct {
	Answered bool
	Value    string
	SourceAt time.Time // when the source observed it
	// HolderWritable means whoever does the work can write what was read.
	HolderWritable bool
	Restarted      bool
	// Occurrence and ResultAt tie a result to one scheduled run.
	Occurrence string
	ResultAt   time.Time
	// By is the observed identity that fetched or confirmed a delivery.
	By string
	// LogComplete means the source holds every event from LogFrom to LogTo;
	// Violations are the times in it at which the condition did not hold.
	LogComplete    bool
	LogFrom, LogTo time.Time
	Violations     []time.Time
}

// Probe reads one kind of fact, for "state:<name>" or "receipt".
type Probe interface {
	Read(ctx context.Context, req ProbeRequest) (Reading, error)
}

// ReviewRequest asks an independent reviewer about one item.
type ReviewRequest struct {
	RequestID string
	Item      Item
	Ask       string
	Subject   Subject
}

// ReviewReply is a reviewer's answer on one item.
type ReviewReply struct {
	ItemID   string
	Value    ResultValue
	Reason   string
	Evidence []Evidence
}

// Reviewer starts an independent reviewer run. datawatch only ever reads runs
// it started; there is no way to submit a verdict. No reviewer is wired until
// the reviewer build lands, so judged items read "cannot tell".
type Reviewer interface {
	Settings() string
	Held() (reason string, until time.Time, held bool)
	Review(ctx context.Context, req ReviewRequest) (ReviewReply, error)
}

// Verdict is the one stored judgement for an item text, a subject version and
// reviewer settings.
type Verdict struct {
	TextHash string      `json:"text_hash"`
	Subject  string      `json:"subject"`
	Settings string      `json:"settings"`
	Value    ResultValue `json:"value"`
	Reason   string      `json:"reason,omitempty"`
	Evidence []Evidence  `json:"evidence,omitempty"`
	At       time.Time   `json:"at"`
}

// Settings are the runtime values of the acceptance configuration.
type Settings struct {
	ReworkRounds  int
	InfraRetries  int
	CheckTimeout  time.Duration
	Freshness     time.Duration
	WarnBefore    time.Duration
	ProjectChecks map[string]ProjectCheck
	DefaultsFile  string
}

func (s Settings) withDefaults() Settings {
	if s.ReworkRounds <= 0 {
		s.ReworkRounds = 2
	}
	if s.InfraRetries <= 0 {
		s.InfraRetries = 2
	}
	if s.CheckTimeout <= 0 {
		s.CheckTimeout = 10 * time.Minute
	}
	if s.Freshness <= 0 {
		s.Freshness = 5 * time.Minute
	}
	if s.WarnBefore <= 0 {
		s.WarnBefore = 24 * time.Hour
	}
	return s
}

// evaluator works on a private copy of one list.
type evaluator struct {
	set      Settings
	now      func() time.Time
	runner   Runner
	repo     Repo
	resolver SubjectResolver
	probes   map[string]Probe
	reviewer Reviewer
	verdicts map[string]Verdict
}

func cannot(reason string, a ...any) Result {
	return Result{Value: ResultCannotTell, Reason: fmt.Sprintf(reason, a...)}
}

func infra(reason string, a ...any) Result {
	r := cannot(reason, a...)
	r.Infra = true
	return r
}

func waiting(reason string, due time.Time) Result {
	return Result{Value: ResultUnchecked, Reason: "waiting: " + reason, Waiting: &Waiting{Reason: reason, Due: due}}
}

// subjectFor returns the version an item is checked on. A command or state
// item runs on the effect's version; a judged item binds to the content of the
// work, so unrelated changes elsewhere do not undo it.
func (e *evaluator) subjectFor(ctx context.Context, l *List, it Item) (Subject, error) {
	if e.resolver == nil {
		return Subject{}, fmt.Errorf("nothing supplies a version of the subject yet")
	}
	effect := ""
	if base, _ := it.methodBase(); base != MethodReview && strings.HasPrefix(it.Effect, "ref:") {
		effect = it.Effect
	}
	sub, err := e.resolver.Resolve(ctx, l.RequestID, effect)
	if err == nil && sub.Commit == "" {
		err = fmt.Errorf("no version for %q", it.Effect)
	}
	return sub, err
}

// readsThePlace reports whether reading the work's own files shows the item's
// effect. Anywhere else, a file the doer can write is only the doer's claim.
func readsThePlace(effect string) bool { return effect == "work" || strings.HasPrefix(effect, "ref:") }

// saveCheck copies a command check into the item: the project check's command
// text and the union of the inputs and expected lines. A later settings change
// does not touch the copy.
func saveCheck(it *Item, checks map[string]ProjectCheck) {
	base, name := it.methodBase()
	if base != MethodCommand {
		return
	}
	if it.Check == nil {
		it.Check = &CheckSpec{}
	}
	c := *it.Check
	if c.ProjectCheck == "" {
		c.ProjectCheck = name
	}
	if pc, ok := checks[c.ProjectCheck]; ok && c.Command == "" && c.Script == "" {
		c.Command = pc.Command
		c.Inputs = union(pc.Inputs, c.Inputs)
		c.Expect = union(pc.Expect, c.Expect)
		if c.TimeoutSec == 0 {
			c.TimeoutSec = pc.TimeoutSec
		}
	}
	it.Check = &c
}

func union[T comparable](a, b []T) []T {
	var out []T
	seen := map[T]bool{}
	for _, v := range append(append([]T(nil), a...), b...) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// freezeInputs records the hashes of a check's listed inputs at a version.
func (e *evaluator) freezeInputs(ctx context.Context, it Item, st *ItemState, sub Subject) {
	st.InputHashes, st.InputsFrozen = nil, false
	if it.Check == nil || sub.Commit == "" {
		return
	}
	hashes, err := e.inputHashes(ctx, it.Check.Inputs, sub)
	if err != nil {
		return
	}
	st.InputHashes, st.InputsFrozen = hashes, true
}

func (e *evaluator) inputHashes(ctx context.Context, inputs []string, sub Subject) (map[string]string, error) {
	out := map[string]string{}
	for _, in := range inputs {
		id, ok, err := e.repo.ObjectID(ctx, sub, in)
		if err != nil {
			return nil, err
		}
		if !ok {
			id = "absent"
		}
		out[in] = id
	}
	return out, nil
}

// changedInputs lists the check inputs, including the saved script, that
// differ at a version.
func (e *evaluator) changedInputs(ctx context.Context, it Item, st *ItemState, sub Subject) ([]string, error) {
	now, err := e.inputHashes(ctx, it.Check.Inputs, sub)
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, in := range it.Check.Inputs {
		if now[in] != st.InputHashes[in] {
			changed = append(changed, in)
		}
	}
	if it.Check.ScriptPath != "" && it.Check.Script != "" {
		data, ok, err := e.repo.ReadFile(ctx, sub, it.Check.ScriptPath)
		if err != nil {
			return nil, err
		}
		if ok && !bytes.Equal(data, []byte(it.Check.Script)) {
			changed = append(changed, it.Check.ScriptPath)
		}
	}
	return changed, nil
}

func (e *evaluator) timeout(c *CheckSpec) time.Duration {
	if c.TimeoutSec > 0 && time.Duration(c.TimeoutSec)*time.Second < e.set.CheckTimeout {
		return time.Duration(c.TimeoutSec) * time.Second
	}
	return e.set.CheckTimeout
}

// baseline runs a command check once on the starting version.
func (e *evaluator) baseline(ctx context.Context, it Item, sub Subject) *Baseline {
	out := e.runner.Run(ctx, *it.Check, sub, e.timeout(it.Check))
	b := &Baseline{Finished: out.Finished && !out.Cut, ExitCode: out.ExitCode, Reason: out.Reason, Subject: sub.Commit, At: e.now()}
	if !b.Finished {
		return b
	}
	passed := out.ExitCode == 0 && len(it.Check.Expect) > 0
	for _, x := range it.Check.Expect {
		if lineSeen(out.Output, x.Fail) {
			b.FailSeen = append(b.FailSeen, x.Fail)
			passed = false
		}
		if !lineSeen(out.Output, x.Pass) {
			passed = false
		}
	}
	b.Passed = passed
	return b
}

// provesWork says why a passing run of a change item does not show that work
// was done, or "" when it does.
func provesWork(it Item, b *Baseline) string {
	switch {
	case it.Before != BeforeFailFirst:
		return "a change item needs a check that failed before the work (before: fail_first)"
	case b == nil:
		return "no baseline: the check was not run on the starting version"
	case b.Passed:
		return "already passed before the work, so it proves nothing"
	case !b.Finished:
		return "no baseline: the start run did not finish (" + b.Reason + ")"
	case b.ExitCode == 0:
		return "no baseline: the start run did not fail"
	}
	named := 0
	for _, x := range it.Check.Expect {
		if x.Fail == "" {
			continue
		}
		named++
		seen := false
		for _, f := range b.FailSeen {
			seen = seen || f == x.Fail
		}
		if !seen {
			return fmt.Sprintf("no baseline: the start run did not show %q", x.Fail)
		}
	}
	if named == 0 {
		return "no baseline: the item names no test or output line that must fail at the start"
	}
	return ""
}

// judgeRun turns a finished run into a result. Exit 0 alone proves nothing:
// every named line must be there.
func judgeRun(it Item, st *ItemState, out RunOutcome) Result {
	switch {
	case !out.Finished:
		return infra("%s", out.Reason)
	case out.Cut:
		return cannot("the check's output was cut, so it was not read whole")
	case out.ExitCode != 0:
		return Result{Value: ResultNotMet, Reason: fmt.Sprintf("the check exited %d", out.ExitCode)}
	}
	for _, x := range it.Check.Expect {
		if lineSeen(out.Output, x.Fail) {
			return Result{Value: ResultNotMet, Reason: fmt.Sprintf("exit 0, but the output shows %q", x.Fail)}
		}
	}
	if len(it.Check.Expect) == 0 {
		return cannot("the item names nothing that must have run; exit 0 alone proves nothing")
	}
	for _, x := range it.Check.Expect {
		if x.Pass == "" || !lineSeen(out.Output, x.Pass) {
			return cannot("expected line %q not seen: nothing ran, or it was skipped", x.Pass)
		}
	}
	if it.Change || it.Before == BeforeFailFirst {
		if why := provesWork(it, st.Baseline); why != "" {
			r := cannot("%s", why)
			r.AlreadyTrue = st.Baseline != nil && st.Baseline.Passed
			return r
		}
	}
	return Result{Value: ResultMet, Reason: "the saved check passed and showed every named line", AlreadyTrue: st.Baseline != nil && st.Baseline.Passed}
}

// runKey names one run: request, item, revision, check, inputs, subject
// version and occurrence.
func runKey(l *List, it Item, st *ItemState, sub Subject) string {
	return hashOf(l.RequestID, it.ID, it.Revision, itemHash(it), st.InputHashes, sub.Repo, sub.Commit, l.Occurrence)
}

// cached returns a stored result for a key unless it was an infrastructure
// fault with retries left.
func (e *evaluator) cached(st *ItemState, key string) (Result, bool) {
	r, ok := st.Runs[key]
	if !ok || r.Infra && st.InfraTries[key] <= e.set.InfraRetries {
		return Result{}, false
	}
	return r, true
}

func (e *evaluator) store(st *ItemState, key string, r Result) {
	if st.Runs == nil {
		st.Runs = map[string]Result{}
	}
	st.Runs[key] = r
	if r.Infra {
		if st.InfraTries == nil {
			st.InfraTries = map[string]int{}
		}
		st.InfraTries[key]++
	}
}

func (e *evaluator) evalCommand(ctx context.Context, l *List, it Item, st *ItemState) Result {
	if it.Check == nil || it.Check.Command == "" && it.Check.Script == "" {
		return cannot("no saved check: the project check is unknown or was never saved")
	}
	// A script from the work reads only the clean copy. A place elsewhere
	// needs an operator-registered check that reads it.
	if it.Effect != "" && !readsThePlace(it.Effect) && it.Check.ProjectCheck == "" {
		return cannot("%q is outside the work; only a registered project check or a person can read it", it.Effect)
	}
	sub, err := e.subjectFor(ctx, l, it)
	if err != nil {
		return cannot("%v", err)
	}
	st.CurrentSubject = sub.Commit
	if len(it.Check.Inputs) > 0 && !st.InputsFrozen {
		return cannot("the check's inputs were not recorded when it was saved")
	}
	changed, err := e.changedInputs(ctx, it, st, sub)
	if err != nil {
		r := infra("the check inputs could not be read: %v", err)
		r.Subject = sub.Commit
		return r
	}
	if len(changed) > 0 {
		r := cannot("check changed: %s differs from the saved version; revise the item after reviewing that it was not weakened", strings.Join(changed, ", "))
		r.InputChanged, r.Subject = changed, sub.Commit
		return r
	}
	if start, ok := l.StartSubjects[""]; ok && st.Baseline == nil {
		if s, ok := l.StartSubjects[it.Effect]; ok {
			start = s
		}
		st.Baseline = e.baseline(ctx, it, start)
	}
	key := runKey(l, it, st, sub)
	if r, ok := e.cached(st, key); ok {
		return r
	}
	out := e.runner.Run(ctx, *it.Check, sub, e.timeout(it.Check))
	r := judgeRun(it, st, out)
	code := out.ExitCode
	origin := OriginDatawatch
	if it.Check.ProjectCheck != "" {
		origin = OriginProjectCheck
	}
	r.Subject, r.RunKey, r.At = sub.Commit, key, e.now()
	r.Evidence = []Evidence{{Origin: origin, What: "saved check on a clean copy", Output: tail(out.Output, maxEvidence), ExitCode: &code, Hash: hashOf(out.Output), Subject: sub.Commit, At: r.At}}
	e.store(st, key, r)
	return r
}

// readFile reads the state:file fact at a version.
func (e *evaluator) readFile(ctx context.Context, it Item, sub Subject) (value string, holds bool, err error) {
	if it.State == nil || it.State.Path == "" {
		return "", false, fmt.Errorf("the item names no file")
	}
	data, ok, err := e.repo.ReadFile(ctx, sub, it.State.Path)
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "absent", false, nil
	}
	return hashOf(string(data)), strings.Contains(string(data), it.State.Contains), nil
}

// startValue saves a state item's value at the start.
func (e *evaluator) startValue(ctx context.Context, l *List, it Item, sub Subject) *StartValue {
	sv := &StartValue{At: e.now(), Subject: sub.Commit}
	base, name := it.methodBase()
	if base != MethodState {
		return nil
	}
	if name == "file" {
		v, holds, err := e.readFile(ctx, it, sub)
		if err == nil {
			sv.Recorded, sv.Value, sv.True = true, v, holds
		}
		return sv
	}
	if p := e.probes[name]; p != nil {
		r, err := p.Read(ctx, ProbeRequest{RequestID: l.RequestID, Item: it, Subject: sub, Occurrence: l.Occurrence})
		if err == nil && r.Answered {
			sv.Recorded, sv.Value, sv.True = true, r.Value, wantHolds(it, r.Value, sub)
		}
	}
	return sv
}

// wantHolds compares a read value with the wanted one. A deploy compares the
// build's commit, never a version string.
func wantHolds(it Item, value string, sub Subject) bool {
	want := ""
	if it.State != nil {
		want = it.State.Want
	}
	if _, name := it.methodBase(); name == "build_commit" {
		if want == "" {
			want = sub.Commit
		}
		return len(value) >= 12 && len(want) >= 12 && (strings.HasPrefix(want, value) || strings.HasPrefix(value, want))
	}
	return want != "" && value == want
}

// afterStart applies the saved start value to a reading that holds: "already
// true" is shown and never proves work.
func afterStart(it Item, st *ItemState, value string, target Subject, r Result) Result {
	s := st.Start
	startTrue := s != nil && s.Recorded && s.True
	if _, name := it.methodBase(); name == "build_commit" {
		// The wanted commit is the target, which at the start is not yet the
		// version the start value was read beside.
		startTrue = s != nil && s.Recorded && wantHolds(it, s.Value, target)
	}
	if startTrue {
		r.AlreadyTrue = true
	}
	if r.Value != ResultMet || !it.Change && it.Before != BeforeDiffer {
		return r
	}
	var why string
	switch {
	case s == nil || !s.Recorded:
		why = "no start value was saved, so it cannot show that the work did this"
	case startTrue:
		why = "already true at the start, so it does not show work"
	case it.Before == BeforeDiffer && s.Value == value:
		why = "the value is the same as at the start"
	default:
		return r
	}
	r.Value, r.Reason = ResultCannotTell, why
	return r
}

func (e *evaluator) evalFile(ctx context.Context, l *List, it Item, st *ItemState) Result {
	if !readsThePlace(it.Effect) {
		return cannot("a file the doer can write is a claim; it shows no effect at %q (use effect \"work\" or \"ref:<branch>\" only when the file itself is the result)", it.Effect)
	}
	sub, err := e.subjectFor(ctx, l, it)
	if err != nil {
		return cannot("%v", err)
	}
	st.CurrentSubject = sub.Commit
	key := runKey(l, it, st, sub)
	if r, ok := e.cached(st, key); ok {
		return r
	}
	value, holds, err := e.readFile(ctx, it, sub)
	var r Result
	switch {
	case err != nil:
		r = infra("the file could not be read: %v", err)
	case !holds:
		r = Result{Value: ResultNotMet, Reason: "the file does not have the stated content at this version"}
	default:
		r = afterStart(it, st, value, sub, Result{Value: ResultMet, Reason: "the file has the stated content at this version"})
	}
	r.Subject, r.RunKey, r.At = sub.Commit, key, e.now()
	r.Evidence = []Evidence{{Origin: OriginDatawatch, What: "read " + it.State.pathOrEmpty() + " at the version", Hash: value, Subject: sub.Commit, At: r.At}}
	e.store(st, key, r)
	return r
}

func (s *StateSpec) pathOrEmpty() string {
	if s == nil {
		return ""
	}
	return s.Path
}

func (e *evaluator) fresh(r Reading, maxAge time.Duration) bool {
	age := e.now().Sub(r.SourceAt)
	return !r.SourceAt.IsZero() && age <= maxAge && age >= -maxAge
}

// evalProbe reads a live fact. Silence, an error or a stale answer is never
// success.
func (e *evaluator) evalProbe(ctx context.Context, l *List, it Item, st *ItemState, name string) Result {
	p := e.probes[name]
	if p == nil {
		return cannot("datawatch has no way to read %q yet", name)
	}
	if it.Negative && (it.Wait == nil || it.Wait.WindowSec <= 0) {
		return cannot("something not happening needs an observation window; one reading cannot show it")
	}
	var sub Subject
	if e.resolver != nil {
		if s, err := e.subjectFor(ctx, l, it); err == nil {
			sub = s
			// Only a reading compared with a version belongs to it.
			if name == "build_commit" {
				st.CurrentSubject = sub.Commit
			}
		} else if name == "build_commit" {
			return cannot("%v", err)
		}
	}
	if it.Wait != nil {
		if st.WaitAnchor.IsZero() {
			st.WaitAnchor = e.now()
		}
		due := st.WaitAnchor.Add(time.Duration(it.Wait.AfterSec) * time.Second)
		if e.now().Before(due) {
			return waiting("not due yet", due)
		}
		if it.Wait.WindowSec > 0 {
			return e.evalWindow(ctx, l, it, st, p, sub, due)
		}
	}
	r, err := p.Read(ctx, ProbeRequest{RequestID: l.RequestID, Item: it, Subject: sub, Occurrence: l.Occurrence})
	maxAge := e.set.Freshness
	if it.Wait != nil && it.Wait.MaxGapSec > 0 {
		maxAge = time.Duration(it.Wait.MaxGapSec) * time.Second
	}
	switch {
	case err != nil:
		return infra("the source failed: %v", err)
	case !r.Answered:
		return infra("the source did not answer; silence is not success")
	case !e.fresh(r, maxAge):
		return cannot("the source's answer is stale or has no time on it")
	case r.HolderWritable && !readsThePlace(it.Effect):
		return cannot("the doer can write what was read, so it is a claim")
	case l.Occurrence != "" && (r.Occurrence != l.Occurrence || !r.ResultAt.After(l.OccurrenceDue)):
		return cannot("the result is not tied to this run: it must be made after the run was due and carry its id")
	}
	var out Result
	switch {
	case name == "scheduled_run" && r.Value == "spawned":
		out = cannot("only \"spawned\" is known; a start is not a finished run")
	case !wantHolds(it, r.Value, sub) && (it.State == nil || it.State.Want == "") && name != "build_commit":
		out = cannot("the item says no value to compare with")
	case !wantHolds(it, r.Value, sub):
		out = Result{Value: ResultNotMet, Reason: fmt.Sprintf("read %q", tail(r.Value, 200))}
	default:
		out = afterStart(it, st, r.Value, sub, Result{Value: ResultMet, Reason: "the source shows the stated value"})
	}
	out.At = e.now()
	if name == "build_commit" {
		out.Subject = sub.Commit
	}
	out.Evidence = []Evidence{{Origin: OriginDatawatch, What: "read " + name, Output: tail(r.Value, maxEvidence), Subject: out.Subject, At: out.At}}
	return out
}

// evalWindow needs positive, fresh answers over the whole window: gaps no
// longer than the stated bound, or a complete event log.
func (e *evaluator) evalWindow(ctx context.Context, l *List, it Item, st *ItemState, p Probe, sub Subject, due time.Time) Result {
	end := due.Add(time.Duration(it.Wait.WindowSec) * time.Second)
	gap := time.Duration(it.Wait.MaxGapSec) * time.Second
	if gap <= 0 {
		return cannot("the item sets no longest gap between readings")
	}
	key := hashOf("window", runKey(l, it, st, Subject{}), st.WaitAnchor.UnixNano())
	if r, ok := st.Runs[key]; ok {
		return r // a closed or broken window stays as it was found
	}
	r, err := p.Read(ctx, ProbeRequest{RequestID: l.RequestID, Item: it, Subject: sub, Occurrence: l.Occurrence})
	now := e.now()
	s := Sample{At: now, Answered: err == nil && r.Answered, SourceAt: r.SourceAt, Restart: r.Restarted}
	s.Holds = s.Answered && wantHolds(it, r.Value, sub)
	st.Samples = append(st.Samples, s)
	final := func(out Result) Result {
		out.At = now
		if _, name := it.methodBase(); name == "build_commit" {
			out.Subject = sub.Commit
		}
		out.Evidence = []Evidence{{Origin: OriginDatawatch, What: fmt.Sprintf("%d readings from %s to %s", len(st.Samples), due.Format(time.RFC3339), end.Format(time.RFC3339)), At: now}}
		e.store(st, key, out)
		return out
	}
	inWindow := func(t time.Time) bool { return !t.Before(due) && !t.After(end) }
	if s.Answered && e.fresh(r, gap) {
		for _, v := range r.Violations {
			if inWindow(v) {
				return final(Result{Value: ResultNotMet, Reason: "the event log shows it did not hold at " + v.Format(time.RFC3339)})
			}
		}
	}
	for _, x := range st.Samples {
		if x.Answered && !x.Holds && !x.SourceAt.IsZero() && x.At.Sub(x.SourceAt) <= gap && inWindow(x.At) {
			return final(Result{Value: ResultNotMet, Reason: "a reading at " + x.At.Format(time.RFC3339) + " shows it did not hold"})
		}
	}
	if now.Before(end) {
		return waiting("the observation window is open", end)
	}
	if s.Answered && e.fresh(r, gap) && s.Holds && !r.Restarted && r.LogComplete && !r.LogFrom.After(due) && !r.LogTo.Before(end) {
		return final(Result{Value: ResultMet, Reason: "a complete event log covers the window"})
	}
	last := due
	for _, x := range st.Samples {
		switch {
		case x.At.Sub(last) > gap:
			return final(cannot("the window has a gap longer than %s before %s", gap, x.At.Format(time.RFC3339)))
		case !x.Answered:
			return final(cannot("the source did not answer at %s", x.At.Format(time.RFC3339)))
		case x.SourceAt.IsZero() || x.At.Sub(x.SourceAt) > gap:
			return final(cannot("the answer at %s was stale", x.At.Format(time.RFC3339)))
		case x.Restart:
			return final(cannot("a restart broke the window at %s", x.At.Format(time.RFC3339)))
		case !x.Holds:
			return final(cannot("the reading at %s does not show it held", x.At.Format(time.RFC3339)))
		}
		last = x.At
	}
	if last.Before(end) {
		return final(cannot("the readings do not reach the end of the window"))
	}
	return final(Result{Value: ResultMet, Reason: "fresh readings cover the whole window"})
}

// receiptSince is the earliest time a receipt can have happened and still be
// about what the list asks now.
func receiptSince(l *List) time.Time {
	since := l.CreatedAt
	if l.Occurrence != "" {
		// A run's list may be made after its run was due.
		since = l.OccurrenceDue
	}
	if n := len(l.Ask.Additions); n > 0 && l.Ask.Additions[n-1].At.After(since) {
		since = l.Ask.Additions[n-1].At
	}
	if l.ReplacedAt.After(since) {
		since = l.ReplacedAt
	}
	return since
}

// tookItFor says why the observed identity does not show receipt, or "". The
// named recipient counts, and on a delivery-only list so does the current
// holder. On any other list the holder is the doer, so only a named recipient
// who has never done the work counts.
func tookItFor(l *List, it Item, by string) string {
	target := ""
	if it.State != nil {
		target = it.State.Target
	}
	if l.Kind != KindDelivery {
		switch {
		case target == "":
			return "the item names no recipient, and on a list that is not delivery only the holder is the doer"
		case by != target:
			return fmt.Sprintf("taken by %q, who is not the named recipient", by)
		case l.everHeld(by):
			return fmt.Sprintf("taken by %q, who does the work, so it does not show the work reached anyone else", by)
		}
		return ""
	}
	switch {
	case target != "" && by == target, l.isHolder(by):
		return ""
	case target == "" && len(l.Holders) == 0:
		return "no holder is recorded and the item names no recipient, so nobody's fetch shows receipt"
	}
	return fmt.Sprintf("taken by %q, who is neither the named recipient nor the current holder", by)
}

// evalReceipt needs the current holder or the named recipient to have fetched
// or confirmed the delivery, at a time the source states, for the run the list
// is about. A channel taking a message is not receipt.
func (e *evaluator) evalReceipt(ctx context.Context, l *List, it Item) Result {
	switch {
	case l.Scheduled && l.Occurrence == "":
		return cannot("a scheduled request is received run by run; this list is not one run")
	case l.Occurrence != "" && e.now().Before(l.OccurrenceDue):
		return waiting("the run is not due yet", l.OccurrenceDue)
	}
	p := e.probes[MethodReceipt]
	if p == nil {
		return cannot("datawatch has no receipt record for this yet")
	}
	r, err := p.Read(ctx, ProbeRequest{RequestID: l.RequestID, Item: it, Occurrence: l.Occurrence})
	switch {
	case err != nil:
		return infra("the receipt record could not be read: %v", err)
	case !r.Answered:
		return Result{Value: ResultUnchecked, Reason: "no receipt yet"}
	case r.Value != "fetched" && r.Value != "confirmed":
		return cannot("only %q is known; that is not receipt", r.Value)
	case r.By == "":
		return cannot("the receipt does not say who was observed taking it")
	}
	if why := tookItFor(l, it, r.By); why != "" {
		return cannot("%s", why)
	}
	switch {
	case r.SourceAt.IsZero():
		return cannot("the receipt has no time on it")
	case r.SourceAt.After(e.now().Add(e.set.Freshness)):
		return cannot("the receipt's time is in the future")
	case r.SourceAt.Before(receiptSince(l)):
		return cannot("the receipt is older than what is asked now: it was made before the ask, later words, the run's due time or the replaced work")
	case r.Occurrence != l.Occurrence:
		return cannot("the receipt is not for this run: it must carry this list's run id")
	case l.Occurrence != "" && (!r.ResultAt.After(l.OccurrenceDue) || !r.SourceAt.After(l.OccurrenceDue)):
		return cannot("the receipt is not tied to this run: it must be made after the run was due")
	}
	out := Result{Value: ResultMet, Reason: r.Value + " by " + r.By, At: e.now()}
	out.Evidence = []Evidence{{Origin: OriginDatawatch, What: "receipt " + r.Value + " by " + r.By + " at " + r.SourceAt.Format(time.RFC3339), At: out.At}}
	return out
}

func verdictTextHash(it Item) string { return hashOf(it.Statement, it.From) }

func verdictKey(it Item, subject, settings string) string {
	return hashOf(verdictTextHash(it), subject, settings)
}

// evalReview reuses the stored verdict for the same item text, subject version
// and reviewer settings, and starts a run only when there is none.
func (e *evaluator) evalReview(ctx context.Context, l *List, it Item, st *ItemState) Result {
	if e.reviewer == nil {
		return cannot("no reviewer")
	}
	sub, err := e.subjectFor(ctx, l, it)
	if err != nil {
		return cannot("%v", err)
	}
	st.CurrentSubject = sub.Commit
	settings := e.reviewer.Settings()
	key := verdictKey(it, sub.Commit, settings)
	if v, ok := e.verdicts[key]; ok {
		return e.fromVerdict(v, key)
	}
	if reason, until, held := e.reviewer.Held(); held {
		r := waiting("the review is held for usage: "+reason, until)
		r.Subject = sub.Commit
		return r
	}
	if r, ok := e.cached(st, key); ok {
		return r
	}
	reply, err := e.reviewer.Review(ctx, ReviewRequest{RequestID: l.RequestID, Item: it, Ask: askFullText(l.Ask), Subject: sub})
	var fault string
	switch {
	case err != nil:
		fault = "the reviewer run failed: " + err.Error()
	case reply.ItemID != it.ID:
		fault = "the reviewer did not answer this item"
	case reply.Value != ResultMet && reply.Value != ResultNotMet && reply.Value != ResultCannotTell:
		fault = "the reviewer's reply could not be read"
	}
	if fault != "" {
		r := infra("%s", fault)
		r.Subject = sub.Commit
		e.store(st, key, r)
		return r
	}
	if reply.Value == ResultMet && len(reply.Evidence) == 0 {
		reply.Value, reply.Reason = ResultCannotTell, "the reviewer said met without evidence datawatch can open"
	}
	v := Verdict{TextHash: verdictTextHash(it), Subject: sub.Commit, Settings: settings, Value: reply.Value, Reason: reply.Reason, Evidence: reply.Evidence, At: e.now()}
	e.verdicts[key] = v
	return e.fromVerdict(v, key)
}

func (e *evaluator) fromVerdict(v Verdict, key string) Result {
	r := Result{Value: v.Value, Reason: v.Reason, Subject: v.Subject, RunKey: key, At: v.At, Evidence: v.Evidence}
	if v.Value != ResultMet {
		return r
	}
	for _, other := range e.verdicts {
		if other.TextHash == v.TextHash && other.Subject == v.Subject && other.Value == ResultNotMet {
			r.Value, r.Reason = ResultCannotTell, "reviewers disagree: another review of the same version said not met"
		}
	}
	return r
}

// evalItem evaluates one deterministic or judged item.
func (e *evaluator) evalItem(ctx context.Context, l *List, it Item, st *ItemState) Result {
	base, name := it.methodBase()
	if it.Negative && base == MethodCommand && it.Wait != nil {
		return cannot("a command cannot cover an observation window; use a state source")
	}
	switch base {
	case MethodCommand:
		if it.Wait != nil {
			if st.WaitAnchor.IsZero() {
				st.WaitAnchor = e.now()
			}
			if due := st.WaitAnchor.Add(time.Duration(it.Wait.AfterSec) * time.Second); e.now().Before(due) {
				return waiting("not due yet", due)
			}
		}
		return e.evalCommand(ctx, l, it, st)
	case MethodState:
		if name == "file" {
			if it.Negative {
				return cannot("a missing line in a file does not show that something did not happen")
			}
			return e.evalFile(ctx, l, it, st)
		}
		return e.evalProbe(ctx, l, it, st, name)
	case MethodReceipt:
		return e.evalReceipt(ctx, l, it)
	case MethodReview:
		return e.evalReview(ctx, l, it, st)
	}
	return cannot("datawatch does not know the check method %q", it.Method)
}

// recheckVersions reads again the version of every met result that belongs to
// one. A version that moved, or cannot be read, makes the result stale.
func (e *evaluator) recheckVersions(ctx context.Context, l *List) {
	for _, it := range l.Items {
		st := l.State[it.ID]
		if st == nil || !versionBound(it) || st.Result.Value != ResultMet {
			continue
		}
		if sub, err := e.subjectFor(ctx, l, it); err == nil {
			st.CurrentSubject = sub.Commit
		} else {
			st.CurrentSubject = "unknown"
		}
	}
}

// run evaluates a list: deterministic items first, and no reviewer while one
// of them is not met.
func (e *evaluator) run(ctx context.Context, l *List) {
	failed := false
	for _, deterministic := range []bool{true, false} {
		for _, it := range l.Items {
			base, _ := it.methodBase()
			if base == MethodConfirm {
				if st := l.State[it.ID]; deterministic && st != nil && st.Result.Subject != "" {
					if sub, err := e.subjectFor(ctx, l, it); err == nil {
						st.CurrentSubject = sub.Commit
					} else {
						st.CurrentSubject = "unknown"
					}
				}
				continue
			}
			if it.deterministic() != deterministic {
				continue
			}
			st := l.state(it.ID)
			if base == MethodReview && failed {
				st.Result = Result{Value: ResultUnchecked, Reason: "not reviewed yet: a deterministic item is not met"}
				continue
			}
			st.Result = e.evalItem(ctx, l, it, st)
			if st.Result.At.IsZero() {
				st.Result.At = e.now()
			}
			failed = failed || deterministic && st.Result.Value == ResultNotMet
		}
	}
}
