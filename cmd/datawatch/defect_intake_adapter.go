package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// defectIntakeAdapter is the production DefectIntake implementation
// (internal/autonomous/defect_intake.go): it places a filed kind-bug defect
// into the named Automation's bugs stage through the manager's member
// insert (AutomationInsertStore, BL431 — automation_insert.go), where it
// lands as a PendingInsert held for operator approval on a non-draft
// Automation, or as a member at once on a draft. The name is read live on
// every offer, so a runtime config set takes effect on the next filing
// with no restart.
type defectIntakeAdapter struct {
	amgr *autonomouspkg.Manager
	// automationName is the live read of autonomous.
	// defect_intake_automation (main.go's liveAutonomousConfig).
	automationName func() string
}

// Offer implements autonomouspkg.DefectIntake. It is safe to call more than
// once per defect: the manager already de-duplicates the call per Automaton
// ID, and a duplicate member insert is additionally refused by the
// Automation store (checkPendingDuplicate).
func (a *defectIntakeAdapter) Offer(_ context.Context, d autonomouspkg.Defect) error {
	name := strings.TrimSpace(a.automationName())
	if name == "" {
		return errors.New("autonomous.defect_intake_automation is empty; no Automation is named for defect intake")
	}
	for _, auto := range a.amgr.AutomationStore().List() {
		if auto.Name != name {
			continue
		}
		_, err := a.amgr.InsertMember(auto.ID, autonomouspkg.InsertMemberInput{
			PRDID:           d.ID,
			Name:            d.Title,
			Actor:           "autonomous:defect_intake",
			Reason:          "defect intake: Automaton " + d.ID + " filed with kind bug",
			ExpectedVersion: auto.Version,
		})
		return err
	}
	return fmt.Errorf("no Automation named %q for defect intake; set autonomous.defect_intake_automation to an existing Automation", name)
}
