// Housekeeping repairs: the daemon repairs, or at least cleanly resolves, the
// basic stuck states that otherwise only the supervisor watch reports.
//
// A Repairer owns five groups (task_chain, advance_judge, lane_fill,
// leftovers, planning). Each group has a mode: off (nothing happens), report (the
// default: record what the group would do and act on nothing) or on (repair).
//
// Hard limits, enforced by what a Repair may do rather than by a check: a
// repair never merges, ships, deletes, kills or signals a session, changes
// a model or backend setting, or touches a security workflow. It only
// resets or retries tasks, runs the documented sync and cleanup steps, and
// advances state. Every repair writes one decision-log line (found, done,
// how to undo), and a case skipped as unsafe raises exactly one alert per
// subject until the subject stops being found.
package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Repair group names.
const (
	RepairGroupTaskChain    = "task_chain"
	RepairGroupAdvanceJudge = "advance_judge"
	RepairGroupLaneFill     = "lane_fill"
	RepairGroupLeftovers    = "leftovers"
	RepairGroupPlanning     = "planning"
)

// Record kinds.
const (
	// RepairWould is what a report-mode group would do.
	RepairWould = "would"
	// RepairDid is what an on-mode group did.
	RepairDid = "did"
	// RepairFailed is an apply that returned an error.
	RepairFailed = "failed"
	// RepairSkippedUnsafe is a finding judged unsafe to repair.
	RepairSkippedUnsafe = "skipped_unsafe"
)

// housekeepingRecordCap bounds the in-memory and persisted record.
const housekeepingRecordCap = 200

// RepairFinding is one stuck state a Repair found.
type RepairFinding struct {
	// Subject names what is stuck (an Automaton, task or lane); the
	// de-duplication key for alerts and the grace clock.
	Subject string
	// Summary says in plain words what is wrong.
	Summary string
	// Unsafe is set when repairing it could lose work or needs a person;
	// the finding is reported with an alert and never repaired.
	Unsafe bool
	// UnsafeReason says why.
	UnsafeReason string
}

// Repair is one group's detector and fixer.
type Repair interface {
	// Group is the group name this repair belongs to.
	Group() string
	// Detect lists the stuck states now. It changes nothing.
	Detect(ctx context.Context) []RepairFinding
	// Apply repairs one finding and returns what it did.
	Apply(ctx context.Context, f RepairFinding) (string, error)
	// UndoHint says how an operator undoes what Apply did to f.
	UndoHint(f RepairFinding) string
}

// RepairRecord is one entry in the bounded record.
type RepairRecord struct {
	At      time.Time `json:"at"`
	Group   string    `json:"group"`
	Subject string    `json:"subject"`
	Kind    string    `json:"kind"`
	Found   string    `json:"found"`
	Done    string    `json:"done,omitempty"`
	Undo    string    `json:"undo,omitempty"`
}

// RepairerHooks are the pieces the wiring supplies; every field may be nil.
type RepairerHooks struct {
	// Settings reads the block live; nil means all groups report.
	Settings func() config.HousekeepingRepairsConfig
	// Log writes one decision-log line.
	Log func(line string)
	// Alert raises one alert.
	Alert func(level, title, body string)
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// StorePath is the file the record persists to; empty keeps it in memory.
	StorePath string
}

// Repairer runs the five groups.
type Repairer struct {
	hooks   RepairerHooks
	repairs []Repair

	mu        sync.Mutex
	records   []RepairRecord
	firstSeen map[string]time.Time // group/subject -> first pass that found it
	alerted   map[string]bool      // group/subject already alerted as unsafe
}

// NewRepairer builds a Repairer over the repairs. A StorePath that holds an
// earlier record is loaded; an unreadable one starts empty.
func NewRepairer(hooks RepairerHooks, repairs ...Repair) *Repairer {
	r := &Repairer{hooks: hooks, repairs: repairs, firstSeen: map[string]time.Time{}, alerted: map[string]bool{}}
	if hooks.StorePath != "" {
		if b, err := os.ReadFile(hooks.StorePath); err == nil {
			var recs []RepairRecord
			if json.Unmarshal(b, &recs) == nil {
				if len(recs) > housekeepingRecordCap {
					recs = recs[len(recs)-housekeepingRecordCap:]
				}
				r.records = recs
			}
		}
	}
	return r
}

func (r *Repairer) now() time.Time {
	if r.hooks.Now != nil {
		return r.hooks.Now()
	}
	return time.Now()
}

func (r *Repairer) settings() config.HousekeepingRepairsConfig {
	if r.hooks.Settings != nil {
		return r.hooks.Settings().Effective()
	}
	return config.HousekeepingRepairsConfig{}.Effective()
}

// Records returns a copy of the bounded record, oldest first.
func (r *Repairer) Records() []RepairRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RepairRecord(nil), r.records...)
}

// Run makes one pass over every group and returns the records it added.
func (r *Repairer) Run(ctx context.Context) []RepairRecord {
	if r == nil {
		return nil
	}
	set := r.settings()
	grace := time.Duration(set.GraceSecondsOrDefault()) * time.Second
	var added []RepairRecord
	for idx, rep := range r.repairs {
		if ctx.Err() != nil {
			break
		}
		group := rep.Group()
		mode := set.ModeFor(group)
		if mode == config.HousekeepingModeOff {
			continue
		}
		added = append(added, r.runGroup(ctx, rep, idx, mode, grace)...)
	}
	if len(added) > 0 {
		r.persist()
	}
	return added
}

func (r *Repairer) runGroup(ctx context.Context, rep Repair, idx int, mode string, grace time.Duration) []RepairRecord {
	group := rep.Group()
	// ns keeps two repairs of one group from clearing each other's clocks.
	ns := fmt.Sprintf("%s#%d/", group, idx)
	now := r.now()
	findings := rep.Detect(ctx)
	live := map[string]bool{}
	var added []RepairRecord
	for _, f := range findings {
		key := ns + f.Subject
		live[key] = true
		r.mu.Lock()
		first, seen := r.firstSeen[key]
		if !seen {
			first = now
			r.firstSeen[key] = now
		}
		r.mu.Unlock()
		if now.Sub(first) < grace {
			continue // not stuck long enough yet
		}
		added = append(added, r.handle(ctx, rep, mode, f, key))
	}
	r.mu.Lock()
	for k := range r.firstSeen {
		if strings.HasPrefix(k, ns) && !live[k] {
			delete(r.firstSeen, k) // gone: a later find is a new one
			delete(r.alerted, k)
		}
	}
	r.mu.Unlock()
	return added
}

func (r *Repairer) handle(ctx context.Context, rep Repair, mode string, f RepairFinding, key string) RepairRecord {
	group := rep.Group()
	rec := RepairRecord{At: r.now(), Group: group, Subject: f.Subject, Found: f.Summary, Undo: rep.UndoHint(f)}
	switch {
	case f.Unsafe:
		rec.Kind = RepairSkippedUnsafe
		rec.Done = "skipped as unsafe: " + f.UnsafeReason
		r.alertOnce(key, group, f)
	case mode != config.HousekeepingModeOn:
		rec.Kind = RepairWould
		rec.Done = "would repair (report mode; nothing changed)"
	default:
		done, err := rep.Apply(ctx, f)
		if err != nil {
			rec.Kind = RepairFailed
			rec.Done = "repair failed: " + err.Error()
		} else {
			rec.Kind = RepairDid
			rec.Done = done
		}
	}
	r.append(rec)
	if r.hooks.Log != nil {
		r.hooks.Log(DecisionLogLine(rec))
	}
	return rec
}

// alertOnce raises the one alert for an unsafe subject.
func (r *Repairer) alertOnce(key, group string, f RepairFinding) {
	r.mu.Lock()
	done := r.alerted[key]
	r.alerted[key] = true
	r.mu.Unlock()
	if done || r.hooks.Alert == nil {
		return
	}
	r.hooks.Alert("warn", "Housekeeping repair skipped: "+f.Subject,
		fmt.Sprintf("The %s repair found a stuck state it will not touch on its own: %s. Reason: %s. A person needs to look at it.", group, f.Summary, f.UnsafeReason))
}

func (r *Repairer) append(rec RepairRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec)
	if len(r.records) > housekeepingRecordCap {
		r.records = append([]RepairRecord(nil), r.records[len(r.records)-housekeepingRecordCap:]...)
	}
}

func (r *Repairer) persist() {
	if r.hooks.StorePath == "" {
		return
	}
	r.mu.Lock()
	b, err := json.Marshal(r.records)
	r.mu.Unlock()
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(r.hooks.StorePath), 0o755) != nil {
		return
	}
	tmp := r.hooks.StorePath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, r.hooks.StorePath)
	}
}

// DecisionLogLine is the one decision-log line a repair writes: what was
// found, what was done and how to undo it.
func DecisionLogLine(rec RepairRecord) string {
	return fmt.Sprintf("housekeeping repair [%s] %s: found: %s; done: %s; undo: %s", rec.Group, rec.Subject, rec.Found, rec.Done, rec.Undo)
}

// ---- Repairs over the supervisor loop steps ----

// NewSupervisorStepRepairs returns the four repairs backed by the mechanical
// supervisor loop steps (start next chain step, retry failed infra, lane
// count, cleanup sweep). Nil steps give none.
func NewSupervisorStepRepairs(s *SupervisorLoopSteps) []Repair {
	if s == nil {
		return nil
	}
	return []Repair{taskChainRepair{s}, advanceJudgeRepair{s}, laneFillRepair{s}, leftoversRepair{s}}
}

type taskChainRepair struct{ s *SupervisorLoopSteps }

func (taskChainRepair) Group() string { return RepairGroupTaskChain }
func (r taskChainRepair) Detect(_ context.Context) []RepairFinding {
	var out []RepairFinding
	for _, a := range r.s.c.m.automations.List() {
		if a.Status != AutomationActive {
			continue
		}
		for _, id := range liveCursorIDs(a) {
			if mem := automationMember(a, id); mem != nil && memberUnstarted(mem) {
				out = append(out, RepairFinding{Subject: "automation " + a.ID, Summary: "the next step of the chain is waiting and nothing started it"})
				break
			}
		}
	}
	return out
}
func (r taskChainRepair) Apply(ctx context.Context, _ RepairFinding) (string, error) {
	return r.s.startNextChainStep(ctx)
}
func (taskChainRepair) UndoHint(RepairFinding) string {
	return "pause the Automation to stop the started step, or cancel that step's Automaton"
}

type advanceJudgeRepair struct{ s *SupervisorLoopSteps }

func (advanceJudgeRepair) Group() string { return RepairGroupAdvanceJudge }
func (r advanceJudgeRepair) Detect(_ context.Context) []RepairFinding {
	var out []RepairFinding
	st := r.s.c.m.Store()
	prds := st.ListPRDs()
	st.WithLock(func() {
		for _, prd := range prds {
			for i := range prd.Story {
				for j := range prd.Story[i].Tasks {
					t := &prd.Story[i].Tasks[j]
					if t.Status == TaskReviewNeeded && t.FailureReason != nil && t.FailureReason.Source == infraRetryHoldSource {
						out = append(out, RepairFinding{Subject: "task " + prd.ID + "/" + t.ID, Summary: "the task is held after infrastructure retries ran out"})
					}
				}
			}
		}
	})
	return out
}
func (r advanceJudgeRepair) Apply(ctx context.Context, _ RepairFinding) (string, error) {
	return r.s.retryFailedInfra(ctx)
}
func (advanceJudgeRepair) UndoHint(RepairFinding) string {
	return "cancel the task, or hold it again by resolving its review with the hold answer"
}

type laneFillRepair struct{ s *SupervisorLoopSteps }

func (laneFillRepair) Group() string { return RepairGroupLaneFill }
func (r laneFillRepair) Detect(_ context.Context) []RepairFinding {
	lc := r.s.hooks.Lanes
	set := r.s.settings()
	if lc == nil || set.LaneMax <= 0 {
		return nil
	}
	target := lc.Wanted()
	if target < set.LaneMin {
		target = set.LaneMin
	}
	if target > set.LaneMax {
		target = set.LaneMax
	}
	if cur := lc.Current(); cur != target {
		return []RepairFinding{{Subject: "lanes", Summary: fmt.Sprintf("running lane count is %d, wanted %d", cur, target)}}
	}
	return nil
}
func (r laneFillRepair) Apply(ctx context.Context, _ RepairFinding) (string, error) {
	return r.s.laneCount(ctx)
}
func (laneFillRepair) UndoHint(RepairFinding) string {
	return "set the lane count back with the lane count control; the bounds are supervisor_loop.lane_count.min and .max"
}

type leftoversRepair struct{ s *SupervisorLoopSteps }

func (leftoversRepair) Group() string { return RepairGroupLeftovers }
func (r leftoversRepair) Detect(_ context.Context) []RepairFinding {
	if r.s.c.m == nil || r.s.c.m.cleanupStatus == nil {
		return nil
	}
	if due, _ := r.s.c.m.CleanupDiskPass(); due {
		return []RepairFinding{{Subject: "disk cleanup", Summary: "leftover files crossed the cleanup alert threshold"}}
	}
	return nil
}
func (r leftoversRepair) Apply(ctx context.Context, _ RepairFinding) (string, error) {
	return r.s.cleanupSweep(ctx)
}
func (leftoversRepair) UndoHint(RepairFinding) string {
	return "cleanup only refreshes status and raises the disk alert; there is nothing to undo"
}

// ErrRepairNotConfigured is returned by a Repair with nothing wired.
var ErrRepairNotConfigured = errors.New("housekeeping repair: nothing is wired for it")
