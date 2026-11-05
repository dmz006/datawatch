package autonomous

import (
	"strconv"
	"strings"
	"testing"
)

// stageGraph3 is a m0 -> m1 -> m2 draft over harness PRDs 0, 1 and 2 with the
// given member stages ("" = unstaged), for the RenameAutomationStage tests.
func stageGraph3(t *testing.T, h *coordHarness, s0, s1, s2 string) *Automation {
	t.Helper()
	m0, m1, m2 := h.cm(0, "m1", "", ""), h.cm(1, "m2", "", ""), h.cm(2, "", "", "")
	m0.Stage, m1.Stage, m2.Stage = s0, s1, s2
	a, err := h.m.CreateAutomation(AutomationInput{
		Name: "stage", EntryMemberID: "m0",
		Members: []AutomationMember{m0, m1, m2},
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func memberStage(t *testing.T, a *Automation, id string) string {
	t.Helper()
	for _, m := range a.Members {
		if m.ID == id {
			return m.Stage
		}
	}
	t.Fatalf("no member %s in automation %s", id, a.ID)
	return ""
}

// TestAutomationMemberStageRoundTrip creates a draft whose members carry
// stages (one with surrounding whitespace) and checks that both members'
// stages come back through the store, with the whitespace form normalized so
// a blank stage stays blank.
func TestAutomationMemberStageRoundTrip(t *testing.T) {
	h, _ := newControlHarness(t, 2)
	members := []AutomationMember{h.cm(0, "m1", "", ""), h.cm(1, "", "", "")}
	members[0].Stage = " build "
	members[1].Stage = "review"
	a, err := h.m.CreateAutomation(AutomationInput{Name: "st", EntryMemberID: "m0", Members: members}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := h.m.GetAutomation(a.ID)
	if !ok {
		t.Fatal("created automation vanished")
	}
	if got.Members[0].Stage != "build" {
		t.Fatalf("m0 stage %q, want %q", got.Members[0].Stage, "build")
	}
	if got.Members[1].Stage != "review" {
		t.Fatalf("m1 stage %q, want %q", got.Members[1].Stage, "review")
	}
	if memberStage(t, got, "m0") != "build" || memberStage(t, got, "m1") != "review" {
		t.Fatalf("stages %+v", got.Members)
	}
}

// TestAutomationStoreStageValidation runs the store-level Stage rules on both
// the Create and Update paths: at most 64 characters after trimming, no ':'
// and no newline; empty (unstaged) is always valid.
func TestAutomationStoreStageValidation(t *testing.T) {
	dir := t.TempDir()
	prds := map[string]*PRD{"s1": {ID: "s1"}, "s2": {ID: "s2"}, "s3": {ID: "s3"}, "s4": {ID: "s4"}, "s5": {ID: "s5"}, "s6": {ID: "s6"}}
	s, err := NewAutomationStore(dir, storePRDs(t, prds))
	if err != nil {
		t.Fatal(err)
	}
	var next int
	base := func() []AutomationMember {
		next++
		return []AutomationMember{
			storeMember("m"+strconv.Itoa(next), "s"+strconv.Itoa(next), "", "", ""),
		}
	}
	rejectCreate := func(stage, want string) {
		t.Helper()
		mem := base()
		mem[0].Stage = stage
		if _, err := s.Create(&Automation{Name: "bad" + stage[:1], Members: mem}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("stage %q: want an error containing %q, got %v", stage, want, err)
		}
	}
	rejectCreate(strings.Repeat("b", 65), "at most 64 characters")
	rejectCreate("a:b", "must not contain ':'")
	rejectCreate("a\nb", "must not contain a newline")
	rejectCreate("a\rb", "must not contain a newline")

	ok64, err := s.Create(&Automation{Name: "ok", Members: func() []AutomationMember {
		mem := base()
		mem[0].Stage = strings.Repeat("b", 64)
		return mem
	}()})
	if err != nil {
		t.Fatalf("Create with a 64-char stage: %v", err)
	}
	if got := ok64.Members[0].Stage; got != strings.Repeat("b", 64) {
		t.Fatalf("stored stage %q", got)
	}

	spaced, err := s.Create(&Automation{Name: "spaced", Members: func() []AutomationMember {
		mem := base()
		mem[0].Stage = "  spaced  "
		return mem
	}()})
	if err != nil {
		t.Fatalf("Create with a padded stage: %v", err)
	}
	if got := spaced.Members[0].Stage; got != "spaced" {
		t.Fatalf("stored padded stage %q, want %q", got, "spaced")
	}

	// Update path: a mutation that introduces a bad stage is refused.
	if _, err := s.Update(spaced.ID, spaced.Version, func(a *Automation) error { a.Members[0].Stage = "ok:st"; return nil }); err == nil {
		t.Fatal("want a rejected stage on the Update path")
	}
}

// TestAutomationUpdateCarriesStage checks that an automation edit can set,
// change and clear Stage on existing members, and that a member added by the
// edit keeps its stage.
func TestAutomationUpdateCarriesStage(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	got, err := h.m.UpdateAutomation(a.ID, a.Version, AutomationInput{
		Name: "stage",
		Members: func() []AutomationMember {
			m0, m1, m2 := h.cm(0, "m1", "", ""), h.cm(1, "m2", "", ""), h.cm(2, "", "", "")
			m0.Stage, m1.Stage = "release", "release"
			return []AutomationMember{m0, m1, m2}
		}(),
	}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, a.Version+1)
	}
	if memberStage(t, got, "m0") != "release" || memberStage(t, got, "m1") != "release" || memberStage(t, got, "m2") != "" {
		t.Fatalf("stages after update: %+v", got.Members)
	}
}

// TestAutomationInsertCarriesStage covers the insert path: a direct draft
// insert records the Stage on the member, and a held insert keeps it on the
// pending record and applies it when the operator approves.
func TestAutomationInsertCarriesStage(t *testing.T) {
	t.Run("direct insert on a draft", func(t *testing.T) {
		h, _ := newControlHarness(t, 3)
		a := insertDraft(t, h)
		got, err := h.m.InsertMember(a.ID, InsertMemberInput{
			PRDID: h.prds[2].ID, Name: "gamma", Stage: " triage ", ExpectedVersion: a.Version,
		})
		if err != nil {
			t.Fatal(err)
		}
		added := insertedMemberByPRD(t, got, h.prds[2].ID)
		if added.Stage != "triage" {
			t.Fatalf("inserted member stage %q, want %q", added.Stage, "triage")
		}
	})
	t.Run("held insert keeps the stage through approval", func(t *testing.T) {
		h, _ := newControlHarness(t, 3)
		h.m.cfg.Automation.AgentInsertPolicy = PolicyRequireOperator
		a := insertDraft(t, h)
		got, err := h.m.InsertMember(a.ID, InsertMemberInput{
			PRDID: h.prds[2].ID, Stage: "docs", Actor: "agent:x1", Reason: "needed",
			ExpectedVersion: a.Version,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.PendingInserts) != 1 {
			t.Fatalf("pending %+v", got.PendingInserts)
		}
		if p := got.PendingInserts[0]; p.Stage != "docs" {
			t.Fatalf("pending stage %q, want %q", p.Stage, "docs")
		}
		approved, err := h.m.ApproveInsertedMember(a.ID, got.PendingInserts[0].ID, "operator", got.Version)
		if err != nil {
			t.Fatal(err)
		}
		if len(approved.Members) != 3 {
			t.Fatalf("approval dropped a member: %+v", approved.Members)
		}
		last := approved.Members[len(approved.Members)-1]
		if last.Stage != "docs" {
			t.Fatalf("approved member stage %q, want %q", last.Stage, "docs")
		}
	})
}

// TestRenameAutomationStageRenamesAllMatching renames a stage carried by two
// members, checks the Version bump, the Decision and the audit record, and
// that an unstaged member is untouched.
func TestRenameAutomationStageRenamesAllMatching(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	got, err := h.m.RenameAutomationStage(a.ID, "build", "release", a.Version)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version+1 {
		t.Fatalf("version %d, want %d", got.Version, a.Version+1)
	}
	if memberStage(t, got, "m0") != "release" || memberStage(t, got, "m1") != "release" || memberStage(t, got, "m2") != "" {
		t.Fatalf("stages after rename: %+v", got.Members)
	}
	d := got.Decisions[len(got.Decisions)-1]
	if d.Kind != "automation_rename_stage" {
		t.Fatalf("decision %+v", d)
	}
	if det := h.auditDetail("automation_rename_stage"); det == nil || det["to"] != "release" || det["members"] != 2 || det["version"] != got.Version {
		t.Fatalf("audit %v", det)
	}
}

// TestRenameAutomationStageRefusesCollision renames onto a stage label that
// another member already carries and checks that nothing changes.
func TestRenameAutomationStageRefusesCollision(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "release", "build")
	_, err := h.m.RenameAutomationStage(a.ID, "build", "release", a.Version)
	wantErr(t, err, "409", "already has stage")
	cur, ok := h.m.GetAutomation(a.ID)
	if !ok {
		t.Fatal("automation vanished")
	}
	if cur.Version != a.Version || memberStage(t, cur, "m0") != "build" || memberStage(t, cur, "m1") != "release" {
		t.Fatalf("state changed: version %d %+v", cur.Version, cur.Members)
	}
}

// TestRenameAutomationStageRefusesBadNames refuses empty or identical stage
// names before touching the store.
func TestRenameAutomationStageRefusesBadNames(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	_, err := h.m.RenameAutomationStage(a.ID, "", "release", a.Version)
	wantErr(t, err, "non-empty")
	_, err = h.m.RenameAutomationStage(a.ID, "build", "   ", a.Version)
	wantErr(t, err, "non-empty")
	_, err = h.m.RenameAutomationStage(a.ID, "build", "build", a.Version)
	wantErr(t, err, "different")
}

// TestRenameAutomationStageRefusesUnknownStage refuses a from name no member
// carries, without bumping the Version.
func TestRenameAutomationStageRefusesUnknownStage(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	_, err := h.m.RenameAutomationStage(a.ID, "stage-x", "release", a.Version)
	wantErr(t, err, "404", "no member")
	cur, ok := h.m.GetAutomation(a.ID)
	if !ok || cur.Version != a.Version {
		t.Fatalf("state changed: %+v", ok)
	}
}

// TestRenameAutomationStageVersionConflict refuses a stale expectedVersion
// and leaves the stages and Version exactly as they were.
func TestRenameAutomationStageVersionConflict(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	_, err := h.m.RenameAutomationStage(a.ID, "build", "release", a.Version+5)
	wantErr(t, err, "409", "version mismatch")
	cur, ok := h.m.GetAutomation(a.ID)
	if !ok {
		t.Fatal("automation vanished")
	}
	if cur.Version != a.Version || memberStage(t, cur, "m0") != "build" {
		t.Fatalf("state changed: version %d %+v", cur.Version, cur.Members)
	}
}

// TestRenameAutomationStageRefusesTerminal refuses to rename on a completed
// Automation.
func TestRenameAutomationStageRefusesTerminal(t *testing.T) {
	h, _ := newControlHarness(t, 3)
	a := stageGraph3(t, h, "build", "build", "")
	cur, err := h.m.automations.Update(a.ID, 0, func(a *Automation) error { a.Status = AutomationCompleted; return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.m.RenameAutomationStage(a.ID, "build", "release", cur.Version)
	wantErr(t, err, "409")
}
