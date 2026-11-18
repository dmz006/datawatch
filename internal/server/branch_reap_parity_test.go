// branch_reap_parity_test.go — the one parity test proving that the branch
// reaper's report (would-reap / held / backup refs) and the restore of one
// reaped branch are reachable AND consistent on every operator surface:
// REST (route registered and the handler answers), MCP (tools registered),
// CLI (commands present), the comm channel (verbs) and the PWA (app.js
// reads the report route, posts the restore, and the locale bundles carry
// the card strings).
//
// Strategy (modelled on plan_coverage_surfaces_parity_test.go): the shared
// contract literals (the two report/restore routes, the CLI commands, the
// MCP tool names, the comm verbs, the PWA loader key and locale keys) are
// defined ONCE below. The REST layer is exercised SEMANTICALLY here through
// the real handlers (the shared reapSpy/reapFixture doubles from
// branch_reap_test.go). The other five surfaces are thin proxies over that
// REST contract, so for them the test checks the shared literals
// mechanically against each surface's source and the locale bundles — a
// rename or omission on ANY one of them fails this test. The handler's own
// semantics (refusals, 404 for a never-reaped branch, 409 on a busy pass)
// are pinned in branch_reap_test.go; this test pins the SURFACE WIRING.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Shared contract (single source of truth for this test) ──────────
//
// Every surface must agree on these literals; a mismatch on any surface
// fails this test.
const (
	// REST routes — the report read and the restore write. The CLI, the
	// MCP tools, the comm verbs and the PWA all spell them as-is.
	reapParityReportRoute   = `/api/autonomous/branch-reap`
	reapParityRestoreRoute  = `/api/autonomous/branch-reap/restore`
	reapParityRestoreBranch = "automaton/prd-a--tasks-fix-t1"

	// CLI command names (`datawatch autonomous <command>`).
	reapParityCLIVerbReport  = "branch-reap"
	reapParityCLIVerbRestore = "branch-reap-restore"
	reapParityCLIReportCmd   = "newAutonomousBranchReapCmd"
	reapParityCLIRestoreCmd  = "newAutonomousBranchReapRestoreCmd"

	// MCP tool names and argument names.
	reapParityMCPReportTool   = "autonomous_branch_reap"
	reapParityMCPRestoreTool  = "autonomous_branch_reap_restore"
	reapParityMCPBranchArg    = "branch"
	reapParityMCPReportConst  = "branchReapPath"
	reapParityMCPRestoreConst = "branchReapRestore"

	// Comm channel verbs (`autonomous <verb>`), dash and underscore forms.
	reapParityCommVerbReport     = "branch-reap"
	reapParityCommVerbReportAlt  = "branch_reap"
	reapParityCommVerbRestore    = "branch-reap-restore"
	reapParityCommVerbRestoreAlt = "branch_reap_restore"
)

// reapParityLocaleKeys — the PWA card strings app.js renders from the report
// and the restore click; every locale bundle must carry them non-empty.
var reapParityLocaleKeys = []string{
	"branch_reap_card_title",
	"branch_reap_reaped_heading",
	"branch_reap_waiting_heading",
	"branch_reap_restore_btn",
	"branch_reap_restore_ok",
	"branch_reap_failed",
}

// reapParityRestore — POST the one branch to the restore route through the
// real handler and return the response.
func reapParityRestore(t *testing.T, s *Server) (int, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, reapParityRestoreRoute, strings.NewReader(`{"branch":"`+reapParityRestoreBranch+`"}`))
	rec := httptest.NewRecorder()
	s.handleAutonomousBranchReapRestore(rec, req)
	return rec.Code, rec
}

func TestReapParity(t *testing.T) {
	// The shared spy and fixture from branch_reap_test.go: the report is one
	// reaped branch with its backup ref, the restore re-creates it.
	rep, _ := reapFixture()
	if rep.Reaped[0].Branch != reapParityRestoreBranch {
		t.Fatalf("reapFixture branch = %q, parity restore branch = %q; keep them in step", rep.Reaped[0].Branch, reapParityRestoreBranch)
	}

	t.Run("REST routes registered", func(t *testing.T) {
		readRel(t, "internal/server/server.go",
			`apiMux.HandleFunc("`+reapParityReportRoute+`"`,
			`api.handleAutonomousBranchReap)`,
			`apiMux.HandleFunc("`+reapParityRestoreRoute+`"`,
			`api.handleAutonomousBranchReapRestore)`,
		)
	})

	t.Run("REST serves the report and the restore", func(t *testing.T) {
		spy := &reapSpy{
			preview:    rep,
			previewErr: nil,
			restoreRef: rep.Reaped[0].BackupRef,
			restoreSHA: "abc123",
		}
		s := &Server{autonomousMgr: spy}

		code, rec := reapCall(t, s, http.MethodGet, reapParityReportRoute, "")
		if code != http.StatusOK {
			t.Fatalf("report: status = %d, want 200: %s", code, rec.Body)
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("report is not a JSON object: %v: %s", err, rec.Body)
		}
		for _, key := range []string{"reaped", "waiting", "backups"} {
			if _, ok := body[key]; !ok {
				t.Fatalf("report body lacks %q: %s", key, rec.Body)
			}
		}
		if spy.previewCalls != 1 {
			t.Fatalf("report preview calls = %d, want 1", spy.previewCalls)
		}

		rCode, rRec := reapParityRestore(t, s)
		if rCode != http.StatusOK {
			t.Fatalf("restore: status = %d, want 200: %s", rCode, rRec.Body)
		}
		var res struct {
			Branch    string `json:"branch"`
			BackupRef string `json:"backup_ref"`
			SHA       string `json:"sha"`
		}
		if err := json.Unmarshal(rRec.Body.Bytes(), &res); err != nil {
			t.Fatalf("restore is not JSON: %v: %s", err, rRec.Body)
		}
		if res.Branch != reapParityRestoreBranch || res.BackupRef != rep.Reaped[0].BackupRef {
			t.Fatalf("restore result = %+v, want the fixture branch and its backup ref", res)
		}
		if spy.restoreCalls != 1 {
			t.Fatalf("restore calls = %d, want 1", spy.restoreCalls)
		}
	})

	t.Run("CLI commands present", func(t *testing.T) {
		readRel(t, "cmd/datawatch/cli_branch_reap.go",
			`Use:   "`+reapParityCLIVerbReport+`"`,
			`Use:   "`+reapParityCLIVerbRestore+` <branch>"`,
			`"`+reapParityReportRoute+`"`,
			`"`+reapParityRestoreRoute+`"`,
		)
		// Both commands are attached to `datawatch autonomous`.
		readRel(t, "cmd/datawatch/cli_autonomous.go",
			reapParityCLIReportCmd+"()",
			reapParityCLIRestoreCmd+"()",
		)
	})

	t.Run("MCP tools registered", func(t *testing.T) {
		readRel(t, "internal/mcp/reap_tools.go",
			`NewTool("`+reapParityMCPReportTool+`"`,
			`NewTool("`+reapParityMCPRestoreTool+`"`,
			`WithString("`+reapParityMCPBranchArg+`"`,
			reapParityMCPReportConst+"    = "+fmt.Sprintf("%q", reapParityReportRoute),
			fmt.Sprintf("%s = %s + %q", reapParityMCPRestoreConst, reapParityMCPReportConst, "/restore"),
		)
		// Both tools are attached to the MCP server.
		readRel(t, "internal/mcp/server.go",
			"toolAutonomousBranchReap()",
			"tracked(s.handleAutonomousBranchReap)",
			"toolAutonomousBranchReapRestore()",
			"tracked(s.handleAutonomousBranchReapRestore)",
		)
	})

	t.Run("comm verbs", func(t *testing.T) {
		readRel(t, "internal/router/merge_comm.go",
			`"merge", "safe-to-delete", "`+reapParityCommVerbReport+`", "`+reapParityCommVerbRestore+`"`,
			`"`+reapParityCommVerbReport+`", "`+reapParityCommVerbReportAlt+`"`+`:`,
			`"`+reapParityCommVerbRestore+`", "`+reapParityCommVerbRestoreAlt+`"`,
			`"`+reapParityReportRoute+`"`,
			`"`+reapParityRestoreRoute+`"`,
		)
		// The router dispatches both dash forms (and the underscore forms)
		// to the shared merge family handler.
		readRel(t, "internal/router/sx2_parity.go",
			`"`+reapParityCommVerbReport+`", "`+reapParityCommVerbReportAlt+`", "`+reapParityCommVerbRestore+`", "`+reapParityCommVerbRestoreAlt+`":`,
			"r.handleMergeVerb(verb, args)",
		)
	})

	t.Run("PWA card, restore button and loader", func(t *testing.T) {
		readRel(t, "internal/server/web/app.js",
			`'`+reapParityReportRoute+`'`,
			`'`+reapParityRestoreRoute+`'`,
			`data-branch-reap-restore`,
			`key: 'automation_branch_reap'`,
			`t('branch_reap_restore_btn')`,
		)
	})

	t.Run("locale bundles carry every card string", func(t *testing.T) {
		for _, lang := range []string{"en", "de", "es", "fr", "ja"} {
			raw, err := os.ReadFile(filepath.Join("web", "locales", lang+".json"))
			if err != nil {
				t.Fatalf("read %s locale: %v", lang, err)
			}
			var bundle map[string]string
			if err := json.Unmarshal(raw, &bundle); err != nil {
				t.Fatalf("%s locale is not a flat string map: %v", lang, err)
			}
			for _, key := range reapParityLocaleKeys {
				if strings.TrimSpace(bundle[key]) == "" {
					t.Errorf("%s locale lacks %q", lang, key)
				}
			}
		}
	})
}
