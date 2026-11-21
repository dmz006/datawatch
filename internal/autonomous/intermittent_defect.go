// Intermittent-gate defects: when the merge shadow reports a gate as
// intermittent (it failed on the first run and passed on the rerun), the
// shadow run stores a defect record (mergeshadow.IntermittentDefect). This
// file turns those records into the two things the operator sees: one alert
// per new record, titled with the plain test name, and the rows of the
// Housekeeping repairs view. The merge itself is never blocked, and nothing
// here touches a session.
package autonomous

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// intermittentDefectViewCap bounds the rows of the Housekeeping view.
const intermittentDefectViewCap = 50

// IntermittentDefectRow is one intermittent-gate defect in the Housekeeping
// repairs view: the failing test, the failure message, the first output lines,
// the date and the commit under test. Key is the stable identity.
type IntermittentDefectRow struct {
	Key         string `json:"key"`
	Test        string `json:"test"`
	Message     string `json:"message"`
	FirstLines  string `json:"first_lines"`
	Date        string `json:"date"`
	Commit      string `json:"commit"`
	AutomatonID string `json:"automaton_id"`
}

func intermittentRow(d mergeshadow.IntermittentDefect) IntermittentDefectRow {
	return IntermittentDefectRow{Key: d.Key, Test: d.Test, Message: d.Message, FirstLines: d.FirstLines,
		Date: d.Date, Commit: d.Commit, AutomatonID: d.AutomatonID}
}

// intermittentDefectRows reads the stored records, newest first, at most
// intermittentDefectViewCap of them. Never nil; an unreadable store reads as none.
func (m *Manager) intermittentDefectRows() []IntermittentDefectRow {
	rows := []IntermittentDefectRow{}
	if m == nil || m.dataDir == "" {
		return rows
	}
	defects, err := mergeshadow.ListIntermittentDefects(m.dataDir)
	if err != nil {
		return rows
	}
	for _, d := range defects {
		if len(rows) >= intermittentDefectViewCap {
			break
		}
		rows = append(rows, intermittentRow(d))
	}
	return rows
}

// intermittentAlertTitle is the plain alert title: the test name.
func intermittentAlertTitle(d mergeshadow.IntermittentDefect) string { return d.Test }

// intermittentAlertBody says what happened in plain words.
func intermittentAlertBody(d mergeshadow.IntermittentDefect) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This test failed on the first run of the merge shadow test gate and passed on the rerun, so the Automaton's merge is not blocked. It is a defect in the test or the code. Commit under test: %s. Date: %s.", d.Commit, d.Date)
	if d.Message != "" {
		fmt.Fprintf(&b, " Failure: %s", d.Message)
	}
	b.WriteString(" The record is on the Housekeeping repairs card.")
	return b.String()
}

// raiseIntermittentDefectAlerts raises one warn alert per newly created record
// through the Manager's alert sink. The records are new by construction (the
// store drops a test and commit it already holds), so a rerun of the same
// commit raises nothing. A nil sink raises nothing.
func (m *Manager) raiseIntermittentDefectAlerts(defects []mergeshadow.IntermittentDefect) {
	if m == nil || len(defects) == 0 {
		return
	}
	m.mu.Lock()
	sink := m.alertSink
	m.mu.Unlock()
	if sink == nil {
		return
	}
	for _, d := range defects {
		sink("warn", intermittentAlertTitle(d), intermittentAlertBody(d))
	}
}
