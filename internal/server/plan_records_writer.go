// Adapter that lets the merge pipeline's registry_row post-ship hook record a
// shipped row as a plan record. The mergepipeline package defines the small
// RecordWriter interface; this file implements it over the plan-record store,
// so mergepipeline never imports the store or the server.

package server

import (
	"context"
	"errors"
	"strings"

	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/planrecords"
)

// planRecordsWriter implements mergepipeline.RecordWriter over a Store.
type planRecordsWriter struct{ store *planrecords.Store }

// newPlanRecordsWriter returns the writer for st, or nil when no store is
// wired, so a caller never holds a non-nil interface over a nil store.
func newPlanRecordsWriter(st *planrecords.Store) mergepipeline.RecordWriter {
	if st == nil {
		return nil
	}
	return planRecordsWriter{store: st}
}

// UpsertShipped marks the matching registry row shipped, or creates one. A
// match is the legacy number first, then a title or alias in the project.
func (w planRecordsWriter) UpsertShipped(ctx context.Context, row mergepipeline.ShippedRow) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(row.Project) == "" {
		return "", errors.New("no project alias to record the row under")
	}
	cur, found := w.find(row)
	shipped := planrecords.StateShipped
	if found {
		p := planrecords.Patch{State: &shipped, Actor: "merge-pipeline"}
		if row.ShippedBy != "" {
			p.ShippedBy = &row.ShippedBy
		}
		if row.ShippedVersion != "" {
			p.ShippedVersion = &row.ShippedVersion
		}
		if _, err := w.store.Update(cur.UID, p); err != nil {
			return "", err
		}
		return "updated the plan record " + cur.Title, nil
	}
	title := strings.TrimSpace(row.CreateTitle)
	if title == "" {
		return "", errors.New("no title to create the record with")
	}
	rec, err := w.store.Create(planrecords.Record{
		Kind: planrecords.KindRegistryRow, Project: row.Project, State: shipped,
		Title: title, LegacyNumber: row.LegacyNumber,
		ShippedBy: row.ShippedBy, ShippedVersion: row.ShippedVersion,
		Actor: "merge-pipeline",
	})
	if err != nil {
		return "", err
	}
	return "created the plan record " + rec.Title, nil
}

// find returns the registry_row record the shipped row refers to.
func (w planRecordsWriter) find(row mergepipeline.ShippedRow) (planrecords.Record, bool) {
	if row.LegacyNumber > 0 {
		recs, _ := w.store.List(planrecords.Filter{
			Kind: planrecords.KindRegistryRow, Project: row.Project,
			LegacyNumber: row.LegacyNumber, Limit: 1,
		})
		if len(recs) > 0 {
			return recs[0], true
		}
	}
	for _, t := range row.MatchTitles {
		// Resolve also accepts an id, a uid and an unambiguous prefix; only
		// an exact title or alias hit counts here.
		rec, err := w.store.Resolve(t, row.Project)
		if err != nil || rec.Kind != planrecords.KindRegistryRow || rec.Project != row.Project {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(rec.Title), strings.TrimSpace(t)) || hasAlias(rec, t) {
			return rec, true
		}
	}
	return planrecords.Record{}, false
}

func hasAlias(r planrecords.Record, t string) bool {
	for _, a := range r.Aliases {
		if planrecords.NormTitle(a) == planrecords.NormTitle(t) {
			return true
		}
	}
	return false
}

// mergeRunnerOption adjusts a freshly built merge Runner.
type mergeRunnerOption func(*mergepipeline.Runner)

// withPlanRecords wires the registry_row hook's write-through path: the store
// and the autonomous.plan_records.write_through setting. With the setting off
// or no store, the Runner is left exactly as built.
func withPlanRecords(st *planrecords.Store, writeThrough bool) mergeRunnerOption {
	return func(r *mergepipeline.Runner) {
		w := newPlanRecordsWriter(st)
		if !writeThrough || w == nil {
			return
		}
		r.PlanRecords = w
		r.PostShip.WriteThrough = true
	}
}

// planRecordsOption is the option for a request-path run.
func (s *Server) planRecordsOption() mergeRunnerOption {
	wt := false
	if s.cfg != nil {
		wt = s.cfg.Autonomous.PlanRecords.EffectiveWriteThrough()
	}
	return withPlanRecords(s.planRecords, wt)
}

// PlanRecordStore returns the wired plan-record store, nil when none is.
func (s *HTTPServer) PlanRecordStore() *planrecords.Store {
	if s == nil || s.api == nil {
		return nil
	}
	return s.api.planRecords
}
