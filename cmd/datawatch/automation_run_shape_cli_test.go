// CLI coverage for the Automation run-shape settings: --max-parallel on create
// and update, `automation set-max-parallel`, `automation set-stage-gate`,
// `automation insert --kind decision-wait --question`, and the limit, gate and
// waiting columns of the get and status output. The daemon side is a
// captureDaemon (see cli_autonomous_subscriptions_test.go).
package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestAutomationCLIMaxParallelCreateAndUpdate(t *testing.T) {
	c := &captureDaemon{replyBody: `{"id":"ab12","name":"shaped","status":"draft","version":2,"members":[]}`}
	withCaptureServer(t, c)

	var err error
	captureStdout(t, func() {
		err = runCmd(t, newAutomationCmd(), "create", "--name", "shaped", "--max-parallel", "3", "--stage-gate")
	})
	if err != nil {
		t.Fatalf("create --max-parallel: %v", err)
	}
	if c.gotMethod != http.MethodPost || c.gotPath != "/api/automations" {
		t.Errorf("proxied %s %s, want POST /api/automations", c.gotMethod, c.gotPath)
	}
	if body := c.body(t); body["max_parallel"] != float64(3) || body["stage_gate"] != true {
		t.Errorf("body = %v, want max_parallel 3 and stage_gate true", body)
	}

	// Left unset it is not sent.
	captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "create", "--name", "plain") })
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, ok := c.body(t)["max_parallel"]; ok {
		t.Errorf("unset --max-parallel was sent: %v", c.body(t))
	}

	// On update 0 is sent, so the limit can be cleared; only the named flag goes.
	captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "update", "ab12", "--max-parallel", "0") })
	if err != nil {
		t.Fatalf("update --max-parallel 0: %v", err)
	}
	body := c.body(t)
	if v, ok := body["max_parallel"]; !ok || v != float64(0) || len(body) != 1 {
		t.Errorf("update body = %v, want only max_parallel:0", body)
	}
	captureStdout(t, func() { err = runCmd(t, newAutomationCmd(), "update", "ab12", "--name", "renamed") })
	if err != nil {
		t.Fatalf("update --name: %v", err)
	}
	if _, ok := c.body(t)["max_parallel"]; ok {
		t.Errorf("unset --max-parallel was sent on update: %v", c.body(t))
	}

	// A negative limit never reaches the daemon.
	c.gotPath = ""
	for _, args := range [][]string{{"create", "--name", "x", "--max-parallel", "-1"}, {"update", "ab12", "--max-parallel", "-2"}} {
		var got error
		captureStdout(t, func() { got = runCmd(t, newAutomationCmd(), args...) })
		if got == nil || !strings.Contains(got.Error(), "must not be negative") {
			t.Errorf("%v: err = %v", args, got)
		}
		if c.gotPath != "" {
			t.Errorf("%v reached the daemon at %s", args, c.gotPath)
		}
	}
}

func TestAutomationCLISetMaxParallel(t *testing.T) {
	registered := map[string]bool{}
	for _, sub := range newAutomationCmd().Commands() {
		registered[sub.Name()] = true
	}
	for _, name := range []string{"set-max-parallel", "set-stage-gate"} {
		if !registered[name] {
			t.Fatalf("automation subcommand %s is not registered", name)
		}
	}
	_, run, check := setLLMHarness(t)
	path := "/api/automations/ab12/set_max_parallel"

	t.Run("limit", func(t *testing.T) {
		if err := run("set-max-parallel", "ab12", "--max-parallel", "3"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{"max_parallel": float64(3), "expected_version": float64(9)})
	})
	t.Run("zero clears", func(t *testing.T) {
		if err := run("set-max-parallel", "ab12", "--max-parallel", "0"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{"max_parallel": float64(0), "expected_version": float64(9)})
	})
	t.Run("fork and shared flags", func(t *testing.T) {
		if err := run("set-max-parallel", "ab12", "--max-parallel", "2", "--member", "split",
			"--actor", "dmz", "--reason", "gpu lanes", "--expected-version", "8"); err != nil {
			t.Fatal(err)
		}
		check(path, map[string]any{
			"max_parallel": float64(2), "member_id": "split", "actor": "dmz", "reason": "gpu lanes", "expected_version": float64(8),
		})
	})
}

func TestAutomationCLISetMaxParallelRefusals(t *testing.T) {
	c, run, _ := setLLMHarness(t)
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no limit":       {[]string{"set-max-parallel", "ab12"}, "--max-parallel is required"},
		"negative limit": {[]string{"set-max-parallel", "ab12", "--max-parallel", "-1"}, "must not be negative"},
		"no id":          {[]string{"set-max-parallel", "--max-parallel", "1"}, "accepts 1 arg"},
	} {
		c.gotPath = ""
		err := run(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, tc.want)
		}
		if c.gotPath != "" {
			t.Errorf("%s reached the daemon at %s", name, c.gotPath)
		}
	}
}

func TestAutomationCLISetStageGate(t *testing.T) {
	c, run, check := setLLMHarness(t)
	path := "/api/automations/ab12/set_stage_gate"

	if err := run("set-stage-gate", "ab12", "--stage-gate=true"); err != nil {
		t.Fatal(err)
	}
	check(path, map[string]any{"stage_gate": true, "expected_version": float64(9)})

	// false is sent, so the gate can be turned off.
	if err := run("set-stage-gate", "ab12", "--stage-gate=false", "--actor", "dmz", "--expected-version", "4"); err != nil {
		t.Fatal(err)
	}
	check(path, map[string]any{"stage_gate": false, "actor": "dmz", "expected_version": float64(4)})

	c.gotPath = ""
	err := run("set-stage-gate", "ab12")
	if err == nil || !strings.Contains(err.Error(), "--stage-gate is required") {
		t.Errorf("no flag: err = %v", err)
	}
	if c.gotPath != "" {
		t.Errorf("a refused call reached the daemon at %s", c.gotPath)
	}
}

func TestAutomationCLIInsertDecisionWait(t *testing.T) {
	c, run, check := setLLMHarness(t)
	path := "/api/automations/ab12/insert_member"

	if err := run("insert", "ab12", "--kind", "decision-wait", "--question", "  Ship it now?  ", "--reason", "sign-off",
		"--stage", "release", "--after", "m1", "--expected-version", "7"); err != nil {
		t.Fatal(err)
	}
	check(path, map[string]any{
		"member":           map[string]any{"kind": "decision_wait", "question": "Ship it now?", "stage": "release"},
		"urgent":           false,
		"after":            "m1",
		"reason":           "sign-off",
		"expected_version": float64(7),
	})

	// --question alone implies a decision wait, and decision_wait is accepted as the kind.
	if err := run("insert", "ab12", "--question", "Continue?", "--reason", "r", "--expected-version", "7"); err != nil {
		t.Fatal(err)
	}
	if m, _ := c.body(t)["member"].(map[string]any); m["kind"] != "decision_wait" || m["question"] != "Continue?" {
		t.Errorf("member = %v", c.body(t)["member"])
	}
	if err := run("insert", "ab12", "--kind", "decision_wait", "--question", "Continue?", "--reason", "r", "--expected-version", "7"); err != nil {
		t.Fatal(err)
	}

	// The Automaton form is unchanged.
	if err := run("insert", "ab12", "c9d0e1f2", "--reason", "gap", "--expected-version", "7"); err != nil {
		t.Fatal(err)
	}
	if m, _ := c.body(t)["member"].(map[string]any); m["prd_id"] != "c9d0e1f2" || m["question"] != nil || m["kind"] != nil {
		t.Errorf("prd member = %v", c.body(t)["member"])
	}

	c.gotPath = ""
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"kind without a question": {[]string{"insert", "ab12", "--kind", "decision-wait", "--reason", "r"}, "needs --question"},
		"another kind":            {[]string{"insert", "ab12", "--kind", "fork", "--question", "q", "--reason", "r"}, "only decision-wait"},
		"question and a PRD":      {[]string{"insert", "ab12", "c9d0e1f2", "--question", "q", "--reason", "r"}, "takes --question, not an Automaton ID"},
		"question and an item":    {[]string{"insert", "ab12", "--pending-item", "i1", "--question", "q", "--reason", "r"}, "takes --question, not an Automaton ID"},
		"nothing to insert":       {[]string{"insert", "ab12", "--reason", "r"}, "--kind decision-wait --question"},
		"no reason":               {[]string{"insert", "ab12", "--question", "q"}, "--reason is required"},
	} {
		err := run(tc.args...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", name, err, tc.want)
		}
		if c.gotPath != "" {
			t.Errorf("%s reached the daemon at %s", name, c.gotPath)
		}
	}
}

func TestPrintAutomationShowsRunShape(t *testing.T) {
	a := automationView{
		ID: "ab12", Name: "shaped", Status: "active", Version: 3, EntryMemberID: "f",
		MaxParallel: 3, StageGate: true,
		Members: []automationMemberView{
			{ID: "f", Kind: "fork", ForkMembers: []string{"b1", "b2"}, MaxParallel: 2, Status: "running"},
			{ID: "b1", PRDID: "p1", Status: "running"},
			{ID: "b2", PRDID: "p2", Status: "pending", WaitingForSlot: true},
			{ID: "b3", PRDID: "p3", Status: "pending", WaitingForStage: "build"},
			{ID: "ask", Kind: "decision_wait", Question: "Ship the migration now?", Status: "running", WaitingForOperator: true},
		},
	}
	var buf bytes.Buffer
	if err := printAutomation(&buf, a); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Max parallel: 3", "Stage gate: on",
		"branches b1,b2 (max 2 at once)",
		"waiting for a free slot", "waiting for stage build", "waiting for you",
		`asks: "Ship the migration now?"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	// A plain Automation prints as before: no limit or gate line.
	buf.Reset()
	if err := printAutomation(&buf, automationView{ID: "ab12", Name: "plain", Status: "draft", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Max parallel") || strings.Contains(buf.String(), "Stage gate") {
		t.Errorf("a plain Automation prints run-shape lines:\n%s", buf.String())
	}
}

func TestMemberTextHelpers(t *testing.T) {
	long := strings.Repeat("word ", 40)
	if got := truncateForTable(long, 20); !strings.HasSuffix(got, "…") || len([]rune(got)) != 21 {
		t.Errorf("truncateForTable = %q", got)
	}
	if got := truncateForTable("  a\n b  ", 20); got != "a b" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
	if got := memberStatusText(automationMemberView{Status: "completed"}); got != "completed" {
		t.Errorf("status = %q", got)
	}
	if got := memberStatusText(automationMemberView{}); got != "-" {
		t.Errorf("empty status = %q", got)
	}
}

func TestPrintAutomationStatusWaitingColumn(t *testing.T) {
	var buf bytes.Buffer
	plain := []automationStatusRow{{ID: "ab12", Name: "plain", Status: "active"}}
	if err := printAutomationStatus(&buf, plain); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "WAITING") {
		t.Errorf("a plain chain shows the WAITING column:\n%s", buf.String())
	}

	buf.Reset()
	rows := []automationStatusRow{
		{ID: "ab12", Name: "shaped", Status: "active", MaxParallel: 2, RunningMembers: 2,
			WaitingForSlot: []string{"b3", "b4"}, WaitingForStage: []string{"r1"}, WaitingForOperator: []string{"ask"}},
		{ID: "cd34", Name: "quiet", Status: "active"},
	}
	if err := printAutomationStatus(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"WAITING", "2 for a slot, 1 for a stage, 1 for you"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
	if got := automationWaitingText(automationStatusRow{}); got != "-" {
		t.Errorf("nothing waiting = %q, want -", got)
	}
}
