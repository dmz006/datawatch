// The existing records the delivery check reads. Each is a source of its own,
// so one that cannot be read turns the check red instead of looking empty.

package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/deploy"
	pipelinePkg "github.com/dmz006/datawatch/internal/pipeline"
	"github.com/dmz006/datawatch/internal/session"
)

var (
	errAutonomyNotRunning  = errors.New("Automata are enabled but their store is not open")
	errNoQuestionStore     = errors.New("the question store is not open")
	errNoSubscriptionStore = errors.New("the subscription store is not open")
)

func pipelineSource(j *pipelinePkg.Journal, openErr error) delivery.Source {
	return delivery.Source{Name: "pipelines", Collect: func(context.Context, time.Time) ([]delivery.Candidate, error) {
		if openErr != nil {
			return nil, openErr
		}
		return delivery.PipelineCandidates(j.Records()), nil
	}}
}

// deliveryTakesInput reports whether a session in state s can be typed into.
func deliveryTakesInput(s session.State) bool {
	return s == session.StateRunning || s == session.StateWaitingInput || s == session.StateRateLimited
}

// deliveryTargets answers the schedule rules from the session manager.
type deliveryTargets struct{ m *session.Manager }

func (t deliveryTargets) live(id string) bool {
	sess, ok := t.m.GetSession(id)
	return ok && deliveryTakesInput(t.m.SessionSnapshot(sess).State)
}

// Reachable reports whether the schedule has a session to be typed into.
func (t deliveryTargets) Reachable(sc session.ScheduledCommand) bool {
	if !sc.NameTargeted() {
		return t.live(sc.SessionID)
	}
	sess, ok := t.m.ScheduleTarget(&sc)
	return ok && deliveryTakesInput(t.m.SessionSnapshot(sess).State)
}

func deliverySlots(store *session.ScheduleStore) func() map[string]time.Time {
	if store == nil {
		return nil
	}
	return func() map[string]time.Time {
		out := map[string]time.Time{}
		for _, sc := range store.Copies() {
			if sc.State == session.SchedPending && !sc.RunAt.IsZero() {
				out[sc.ID] = sc.RunAt
			}
		}
		return out
	}
}

// automatonCopies returns a copy of every Automaton record.
func automatonCopies(m *autonomouspkg.Manager) []*autonomouspkg.PRD {
	store := m.Store()
	all := store.ListPRDs()
	out := make([]*autonomouspkg.PRD, 0, len(all))
	for _, p := range all {
		if c, ok := store.SnapshotPRD(p.ID); ok {
			out = append(out, c)
		}
	}
	return out
}

// batchBugIDs are the bugs filed into a bug batch.
func batchBugIDs(p *autonomouspkg.PRD) []string {
	meta, ok := parseDefectBatchMeta(p.Spec)
	if !ok {
		return nil
	}
	var ids []string
	for _, d := range storedDefects(p, meta) {
		ids = append(ids, d.ID)
	}
	return ids
}

// deliverySources are the records the check reads on this daemon.
func deliverySources(w *deliveryWiring, d deliveryDeps) []delivery.Source {
	var out []delivery.Source
	add := func(name string, fn func(ctx context.Context, now time.Time) ([]delivery.Candidate, error)) {
		out = append(out, delivery.Source{Name: name, Collect: fn})
	}
	interval := d.cfg.Delivery.Interval()

	if d.sessions != nil {
		targets := deliveryTargets{d.sessions}
		add("sessions", func(context.Context, time.Time) ([]delivery.Candidate, error) {
			all := d.sessions.ListSessions()
			copies := make([]session.Session, 0, len(all))
			for _, s := range all {
				copies = append(copies, d.sessions.SessionSnapshot(s))
			}
			return delivery.SessionCandidates(copies), nil
		})
		if d.schedules != nil {
			add("schedules", func(_ context.Context, now time.Time) ([]delivery.Candidate, error) {
				return delivery.ScheduleCandidates(d.schedules.Copies(), targets, now, interval), nil
			})
		}
	}

	if d.cfg.DataDir != "" {
		store := deploy.FileStore{Path: filepath.Join(expandHome(d.cfg.DataDir), "deploy", "state.json")}
		add("deploy", func(context.Context, time.Time) ([]delivery.Candidate, error) {
			st, found, err := store.Load()
			if err != nil {
				return nil, err
			}
			return delivery.DeployCandidates(st, found), nil
		})
	}

	// Automata switched off: nothing of theirs runs and nothing is read.
	if !d.cfg.Autonomous.Enabled && d.autonomy == nil {
		return out
	}
	autonomy := func(name string, fn func(ctx context.Context, m *autonomouspkg.Manager, now time.Time) ([]delivery.Candidate, error)) {
		add(name, func(ctx context.Context, now time.Time) ([]delivery.Candidate, error) {
			if d.autonomy == nil {
				return nil, errAutonomyNotRunning
			}
			return fn(ctx, d.autonomy, now)
		})
	}
	live := func(id string) bool { return d.sessions != nil && deliveryTargets{d.sessions}.live(id) }

	autonomy("automata", func(_ context.Context, m *autonomouspkg.Manager, now time.Time) ([]delivery.Candidate, error) {
		facts := delivery.AutomatonFacts(automatonCopies(m), live, batchBugIDs)
		return delivery.AutomatonCandidates(facts, now, interval), nil
	})
	autonomy("answers", func(_ context.Context, m *autonomouspkg.Manager, _ time.Time) ([]delivery.Candidate, error) {
		if w.subs == nil {
			return nil, errNoSubscriptionStore
		}
		return delivery.AnswerCandidates(automatonCopies(m), w.subs.List(""), live), nil
	})
	autonomy("questions", func(context.Context, *autonomouspkg.Manager, time.Time) ([]delivery.Candidate, error) {
		if w.questions == nil {
			return nil, errNoQuestionStore
		}
		return delivery.QuestionCandidates(w.questions.List(autonomouspkg.QuestionFilter{State: autonomouspkg.QuestionOpen})), nil
	})
	autonomy("unfinished_branches", func(ctx context.Context, m *autonomouspkg.Manager, _ time.Time) ([]delivery.Candidate, error) {
		ctx, cancel := context.WithTimeout(ctx, deliveryUnlandedTimeout)
		defer cancel()
		branches, err := m.UnlandedWork(ctx)
		if err != nil {
			return nil, err
		}
		return delivery.UnlandedCandidates(branches), nil
	})
	autonomy("pending_items", func(_ context.Context, m *autonomouspkg.Manager, _ time.Time) ([]delivery.Candidate, error) {
		return delivery.PendingItemCandidates(m.ListPendingItems(autonomouspkg.PendingItemFilter{})), nil
	})
	autonomy("closed_work", func(_ context.Context, m *autonomouspkg.Manager, now time.Time) ([]delivery.Candidate, error) {
		return delivery.ClosedWorkCandidates(m.OutcomeCheck().Findings, now), nil
	})
	return out
}
