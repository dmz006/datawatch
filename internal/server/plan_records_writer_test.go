package server

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/mergepipeline"
	"github.com/dmz006/datawatch/internal/planrecords"
)

func openWriterStore(t *testing.T) *planrecords.Store {
	t.Helper()
	st, err := planrecords.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestPlanRecordWriteThroughAdapterCreateThenUpdate(t *testing.T) {
	st := openWriterStore(t)
	w := newPlanRecordsWriter(st)
	row := mergepipeline.ShippedRow{
		Project: "datawatch", MatchTitles: []string{"post-ship actions"}, CreateTitle: "Post-ship actions",
		ShippedBy: "4bec0001", ShippedVersion: "9.9.9",
	}
	if _, err := w.UpsertShipped(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	recs, total := st.List(planrecords.Filter{Project: "datawatch"})
	if total != 1 || recs[0].State != planrecords.StateShipped || recs[0].ShippedBy != "4bec0001" || recs[0].Kind != planrecords.KindRegistryRow {
		t.Fatalf("created %+v", recs)
	}
	// a second ship matches by title and updates instead of creating
	row.ShippedVersion = "9.9.10"
	if _, err := w.UpsertShipped(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	recs, total = st.List(planrecords.Filter{Project: "datawatch"})
	if total != 1 || recs[0].ShippedVersion != "9.9.10" {
		t.Fatalf("after update %+v", recs)
	}
}

func TestPlanRecordWriteThroughAdapterMatchesLegacyNumber(t *testing.T) {
	st := openWriterStore(t)
	seed, err := st.Create(planrecords.Record{Kind: planrecords.KindRegistryRow, Project: "datawatch",
		State: planrecords.StatePlanned, Title: "Old row title", LegacyNumber: 396})
	if err != nil {
		t.Fatal(err)
	}
	w := newPlanRecordsWriter(st)
	if _, err := w.UpsertShipped(context.Background(), mergepipeline.ShippedRow{
		Project: "datawatch", LegacyNumber: 396, CreateTitle: "Different", ShippedBy: "a1", ShippedVersion: "1.0.0",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(seed.UID)
	if err != nil || got.State != planrecords.StateShipped || got.ShippedVersion != "1.0.0" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, total := st.List(planrecords.Filter{Project: "datawatch"}); total != 1 {
		t.Fatalf("total %d, want 1", total)
	}
}

func TestPlanRecordWriteThroughOptionGating(t *testing.T) {
	st := openWriterStore(t)
	for name, tc := range map[string]struct {
		st *planrecords.Store
		wt bool
		on bool
	}{"off": {st, false, false}, "no store": {nil, true, false}, "on": {st, true, true}} {
		r := &mergepipeline.Runner{}
		withPlanRecords(tc.st, tc.wt)(r)
		if (r.PlanRecords != nil) != tc.on || r.PostShip.WriteThrough != tc.on {
			t.Errorf("%s: writer %v, write_through %v, want %v", name, r.PlanRecords != nil, r.PostShip.WriteThrough, tc.on)
		}
	}
}
