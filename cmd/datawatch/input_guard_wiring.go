// Wiring for the rules on input the daemon types by itself: the output-filter
// gate, the audit sink and the upgrade cleanup.

package main

import (
	"errors"
	"fmt"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// filterInputGate answers whether output filter id may type. cfg is read on
// every call, so turning the switch off takes effect at once.
func filterInputGate(cfg *config.Config, filters *session.FilterStore) func(string) error {
	return func(id string) error {
		if !cfg.Session.InputFiltersEnabled {
			return errors.New("session.input_filters_enabled is off")
		}
		return filters.MayType(id)
	}
}

// cancelUpgradedFilterInput cancels what the filters turned off at this start
// had already queued, so nothing they asked for is typed later.
func cancelUpgradedFilterInput(filters *session.FilterStore, schedules *session.ScheduleStore) {
	off, err := filters.TakeUpgradeCleanup()
	if err != nil {
		fmt.Printf("[filter] upgrade cleanup: %v\n", err)
	}
	for _, f := range off {
		ids, err := schedules.CancelQueuedFilterInput(f)
		if err != nil {
			fmt.Printf("[filter] upgrade cleanup of filter %s: %v\n", f.ID, err)
		}
		fmt.Printf("[filter] filter %s turned off (%s); cancelled %d queued command(s) %v\n", f.ID, f.DisabledReason, len(ids), ids)
	}
}

// inputAuditEntry is the audit-log line for one daemon input event: the
// actor names the filter or the daemon sender, never the operator.
func inputAuditEntry(ev session.InputAuditEvent) auditpkg.Entry {
	actor := "daemon:" + ev.Source
	details := map[string]any{"source": ev.Source, "text_len": ev.Length}
	if ev.FilterID != "" {
		actor = "filter:" + ev.FilterID
		details["filter_id"] = ev.FilterID
	}
	if ev.Text != "" {
		details["text"] = ev.Text
	}
	if ev.Reason != "" {
		details["reason"] = ev.Reason
	}
	return auditpkg.Entry{Actor: actor, Action: ev.Action, SessionID: ev.SessionFullID, Details: details}
}
