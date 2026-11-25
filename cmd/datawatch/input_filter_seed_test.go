package main

import (
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// A fresh install must not carry any filter that types into a session.
func TestFreshInstallHasNoInputTypingFilter(t *testing.T) {
	fs, err := session.NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Seed(seededFilters); err != nil {
		t.Fatal(err)
	}
	for _, f := range fs.List() {
		if f.Action == session.FilterActionSendInput || f.Action == session.FilterActionSchedule {
			t.Errorf("fresh install seeds an input-typing filter: %s %q -> %q (enabled=%v)", f.Action, f.Pattern, f.Value, f.Enabled)
		}
	}
}

// With only the seeded filters, an approval prompt on screen schedules and
// types nothing.
func TestSeededFiltersOnApprovalPromptScheduleNothing(t *testing.T) {
	fs, err := session.NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.Seed(seededFilters); err != nil {
		t.Fatal(err)
	}
	var scheduled []string
	engine := session.NewFilterEngine(fs, session.ActionHandlers{
		MayType:     func(_, _ string) error { return nil },
		AddSchedule: func(_, _, command string) error { scheduled = append(scheduled, command); return nil },
		SendInput:   func(_, _, text string) error { scheduled = append(scheduled, text); return nil },
	})
	engine.ProcessLine(&session.Session{ID: "ab12", FullID: "h-ab12"}, " Do you want to proceed?")
	if len(scheduled) != 0 {
		t.Fatalf("an approval prompt scheduled input: %q", scheduled)
	}
}

func TestFilterInputGateAndAuditEntry(t *testing.T) {
	fs, err := session.NewFilterStore(filepath.Join(t.TempDir(), "filters.json"))
	if err != nil {
		t.Fatal(err)
	}
	fp, err := fs.Add("batch done", session.FilterActionSendInput, "continue")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	gate := filterInputGate(cfg, fs)
	if err := fs.SetEnabled(fp.ID, true); err != nil {
		t.Fatal(err)
	}
	if gate(fp.ID) == nil {
		t.Fatal("switch off by default: the filter must not type")
	}
	cfg.Session.InputFiltersEnabled = true
	if err := gate(fp.ID); err != nil {
		t.Fatal(err)
	}
	e := inputAuditEntry(session.InputAuditEvent{Action: session.InputAuditFilterTyped, SessionFullID: "h-a", Source: session.FilterSource(fp.ID), FilterID: fp.ID, Text: "marked line"})
	if e.Actor != "filter:"+fp.ID || e.Action != "filter_input" || e.Details["filter_id"] != fp.ID || e.Details["text"] != "marked line" || e.SessionID != "h-a" {
		t.Fatalf("entry = %+v", e)
	}
}
