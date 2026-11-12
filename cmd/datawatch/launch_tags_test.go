// Launch placement tags (docs/plans/2026-11-06-automata-pages-redesign.md,
// section 12: "tag every session at launch (worker, verifier, planner,
// judge, supervisor)" with role, kind, Automaton and stage) — the shared
// one-shot session body must forward the four launch_* fields to
// /api/sessions/start when a caller sets them, and must send none of them
// when the caller passes the zero value, so existing callers post
// byte-for-byte the body they sent before this feature existed.
package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/session"
)

// TestLaunchTagsPerSite pins the launch tags each one-shot launch site posts
// (docs/plans/2026-11-06-automata-pages-redesign.md, section 12 — the
// operator's "everything we can tag should be tagged"). The builder per
// site lives in session_judgment.go next to the launchTags type; the
// decompose/verifier/guardrail-judge sites in main.go and the supervisor
// site in supervisor_loop_models_wire.go all go through these, and the
// task-worker sites (an ordinary run and the auto-fix re-launch, tagged by
// the executor's placement write) are pinned by their production values
// below (role from the capacity vocabulary, kind from the autonomous
// package's TaskLaunchKind rule) — so a site silently losing its tags (or
// picking the wrong role/kind) fails here instead of showing up as
// untagged sessions later.
//
//	launch site                                             role       kind
//	decompose (decomposeFnSession, main.go)                 planner    decompose
//	verifier (runOneShotJSONSessionWithEffort, main.go)     verifier   task
//	guardrail judge (runOneShotJudgeSessionCaptureID)       judge      review (rejudge on a re-judgment)
//	supervisor loop (supervisorLoopSessionFn)                supervisor — (no Automaton)
//	task worker (executeOne, internal/autonomous/executor.go) worker     task
//	auto-fix (executeOne's auto-fix re-launch, ibid.)        worker     auto-fix
func TestLaunchTagsPerSite(t *testing.T) {
	const prd = "prd-1a2b"
	workerRole := string(capacity.RoleWorker)
	newTask := func(title string) *autonomous.Task { return &autonomous.Task{Title: title} }
	cases := []struct {
		name          string
		got           launchTags
		wantRole      string
		wantKind      string
		wantAutomaton string
		wantStage     string
	}{
		{"decompose", launchTagsForDecompose(prd), "planner", "decompose", prd, ""},
		{"verifier", launchTagsForVerify(prd, "story-1"), "verifier", "task", prd, "story-1"},
		{"guardrail judge rules-review", launchTagsForJudge(prd, false), "judge", "review", prd, ""},
		{"guardrail judge re-judgment", launchTagsForJudge(prd, true), "judge", "rejudge", prd, ""},
		{"supervisor loop", launchTagsForSupervisorLoop(), "supervisor", "", "", ""},
		// The worker sites tag through the executor's placement write, not the
		// one-shot body: the role is the capacity vocabulary's worker, and the
		// kind comes from the production rule autonomous.TaskLaunchKind (an
		// auto-fix launch is a re-run that carries the fix context, or a
		// synthesized "Auto-fix: " corrective task).
		{"task worker, fresh run", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("wire it"), "")}, "worker", "task", "", ""},
		{"auto-fix, re-run with fix context", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("wire it"), "fix the failing build")}, "worker", "auto-fix", "", ""},
		{"auto-fix, synthesized corrective task", launchTags{Role: workerRole, Kind: autonomous.TaskLaunchKind(newTask("Auto-fix: rules-review findings (attempt 1)"), "")}, "worker", "auto-fix", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Role != tc.wantRole {
				t.Errorf("role = %q, want %q", tc.got.Role, tc.wantRole)
			}
			if tc.got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", tc.got.Kind, tc.wantKind)
			}
			if tc.got.Automaton != tc.wantAutomaton {
				t.Errorf("automaton = %q, want %q", tc.got.Automaton, tc.wantAutomaton)
			}
			if tc.got.Stage != tc.wantStage {
				t.Errorf("stage = %q, want %q", tc.got.Stage, tc.wantStage)
			}
			// The values must stay inside the server's launch vocabulary —
			// validateSessionLaunch in internal/server refuses an unknown
			// role/kind with 400 at start time, so builder drift would break
			// real launches even if the expectations above stayed put.
			// Mirror of launchRoles/launchKinds in
			// internal/server/session_launch_tags.go.
			roles := map[string]bool{"worker": true, "verifier": true, "planner": true, "judge": true, "supervisor": true}
			kinds := map[string]bool{"task": true, "decompose": true, "review": true, "rejudge": true, "auto-fix": true}
			if tc.got.Role != "" && !roles[tc.got.Role] {
				t.Errorf("role %q is not in the server launch-role vocabulary", tc.got.Role)
			}
			if tc.got.Kind != "" && !kinds[tc.got.Kind] {
				t.Errorf("kind %q is not in the server launch-kind vocabulary", tc.got.Kind)
			}
		})
	}
}

func TestLaunchTags_PostedOnStartRequest(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-set.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, err := runOneShotJSONSession(cfg, "prd-tags-set", projectDir, "claude-code", "sonnet",
		"autonomous:test-tags", "do it", outputFile, "launch-tags-set.json", 10*time.Second,
		launchTags{Role: "judge", Kind: "review", Automaton: "b7910001", Stage: "verify"})
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}

	for key, wantVal := range map[string]string{
		"launch_role":      "judge",
		"launch_kind":      "review",
		"launch_automaton": "b7910001",
		"launch_stage":     "verify",
	} {
		got, ok := startBody[key]
		if !ok {
			t.Errorf("start request has no %s field: %v", key, startBody)
			continue
		}
		if got != wantVal {
			t.Errorf("start request %s = %v, want %q", key, got, wantVal)
		}
	}
}

// A partial launchTags posts exactly the fields it set and none of the
// empty ones — the omit-when-empty contract is per field, not all-or-nothing.
func TestLaunchTags_PartialPostsOnlySetFields(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-partial.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, err := runOneShotJSONSession(cfg, "prd-tags-partial", projectDir, "claude-code", "sonnet",
		"autonomous:test-tags", "do it", outputFile, "launch-tags-partial.json", 10*time.Second,
		launchTags{Role: "worker", Automaton: "dae00001"})
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}

	if got := startBody["launch_role"]; got != "worker" {
		t.Errorf("start request launch_role = %v, want worker", got)
	}
	if got := startBody["launch_automaton"]; got != "dae00001" {
		t.Errorf("start request launch_automaton = %v, want dae00001", got)
	}
	for _, key := range []string{"launch_kind", "launch_stage"} {
		if got, ok := startBody[key]; ok {
			t.Errorf("empty field %s posted as %v — it must be absent", key, got)
		}
	}
}

// Zero launchTags: not one of the four fields may appear in the start
// request at all (verified through the judge wrapper, so the
// runOneShotJSONSessionWithReader seam is the same one judges use).
func TestLaunchTags_ZeroPostsNothing(t *testing.T) {
	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".launch-tags-zero.json")
	const want = `{"ok":true}`

	srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)
	cfg := &config.Config{}
	cfg.DataDir = t.TempDir()

	_, _, err := runOneShotJudgeSessionCaptureID(cfg, "prd-tags-zero", projectDir, "claude-code", "sonnet", "",
		"autonomous:test-tags-zero", "do it", outputFile, "launch-tags-zero.json", 10*time.Second, "", launchTags{})
	if err != nil {
		t.Fatalf("runOneShotJudgeSessionCaptureID: %v", err)
	}

	if got := startBody["task"]; got == nil {
		t.Fatalf("start request has no task field at all — capture is broken: %v", startBody)
	}
	for _, key := range []string{"launch_role", "launch_kind", "launch_automaton", "launch_stage"} {
		if got, ok := startBody[key]; ok {
			t.Errorf("zero launchTags posted %s = %v — an unset caller must send none of the launch fields", key, got)
		}
	}
}

// oneShotSiteCase drives one real one-shot launch site through the fake
// /api/sessions/start endpoint and names what that site must post and tag.
type oneShotSiteCase struct {
	name       string
	node       string // compute node the session was pinned to ("" = local one-shot)
	wantRole   string
	wantKind   string
	wantStage  string // "" = the site must carry no stage tag
	wantNodeID string // the compute value the tag must carry
	// run starts the site's one-shot with the production launchTagsFor*
	// builder that the site itself passes.
	run func(cfg *config.Config, projectDir, outputFile, node string) error
}

// TestOneShotSitesSystemTags drives every one-shot launch site — the decompose
// planner, the verifier, the guardrail judge (rules review) and its rejudge —
// through the real runOneShotJSON* / runOneShotJudge* wrappers and the fake
// start endpoint. For each started session it asserts the launch fields the
// site posted, then the system tags the daemon derives from that start:
// compute (the node it ran on, "local" for a local one-shot), role, kind,
// automaton, stage (verifier only), backend, lane and project.
//
// The tags are derived with the same production functions the daemon uses
// (autonomous.SessionPlacement → autonomous.DerivePlacementTags under the
// default placement config). The only mirrored step is the request-to-record
// mapping in applySessionLaunchTags (internal/server/session_launch_tags.go),
// which this package cannot reach; its lane fill is mirrored by
// launchLaneForTest below.
func TestOneShotSitesSystemTags(t *testing.T) {
	const (
		prd     = "prd-tags-sites"
		backend = "claude-code"
		model   = "sonnet"
		remote  = "gpu-node-1"
	)
	cases := []oneShotSiteCase{
		{
			name: "decompose planner", wantRole: "planner", wantKind: "decompose", wantNodeID: "local",
			run: func(cfg *config.Config, projectDir, outputFile, node string) error {
				_, _, err := runOneShotJSONSessionCaptureID(cfg, prd, projectDir, backend, model, "",
					decomposeSessionName, "decompose it, write JSON to "+outputFile, outputFile, "decompose-output.json",
					10*time.Second, launchTagsForDecompose(prd))
				return err
			},
		},
		{
			name: "verifier", wantRole: "verifier", wantKind: "task", wantStage: "story-1", wantNodeID: "local",
			run: func(cfg *config.Config, projectDir, outputFile, node string) error {
				_, err := runOneShotJSONSessionWithEffort(cfg, prd, projectDir, backend, model, "",
					"autonomous:verify:task-1", "verify, write JSON to "+outputFile, outputFile, "verify-task-1.json",
					10*time.Second, "", launchTagsForVerify(prd, "story-1"))
				return err
			},
		},
		{
			name: "guardrail judge rules-review, local", wantRole: "judge", wantKind: "review", wantNodeID: "local",
			run: func(cfg *config.Config, projectDir, outputFile, node string) error {
				_, _, err := runOneShotJudgeSessionCaptureID(cfg, prd, projectDir, backend, model, "",
					"autonomous:judge:unit-1", "judge, write JSON to "+outputFile, outputFile, "judge-unit-1.json",
					10*time.Second, "", launchTagsForJudge(prd, false))
				return err
			},
		},
		{
			name: "guardrail judge rejudge, on a leased node", node: remote, wantRole: "judge", wantKind: "rejudge", wantNodeID: remote,
			run: func(cfg *config.Config, projectDir, outputFile, node string) error {
				_, _, err := runOneShotJudgeSessionCaptureID(cfg, prd, projectDir, backend, model, "",
					"autonomous:judge:unit-2", "rejudge, write JSON to "+outputFile, outputFile, "judge-unit-2.json",
					10*time.Second, node, launchTagsForJudge(prd, true))
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			outputFile := filepath.Join(projectDir, ".sites-output.json")
			srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, `{"ok":true}`, true)
			withFakeLoopback(t, srv)
			cfg := &config.Config{}
			cfg.DataDir = t.TempDir()

			if err := tc.run(cfg, projectDir, outputFile, tc.node); err != nil {
				t.Fatalf("one-shot site failed: %v", err)
			}

			// Launch fields the site posted.
			wantFields := map[string]string{
				"launch_role":      tc.wantRole,
				"launch_kind":      tc.wantKind,
				"launch_automaton": prd,
			}
			if tc.wantStage != "" {
				wantFields["launch_stage"] = tc.wantStage
			}
			for key, want := range wantFields {
				if got := startBody[key]; got != want {
					t.Errorf("start request %s = %v, want %q", key, got, want)
				}
			}
			if tc.wantStage == "" {
				if got, ok := startBody["launch_stage"]; ok {
					t.Errorf("start request posted launch_stage = %v; this site must carry no stage", got)
				}
			}
			if got := startBody["backend"]; got != backend {
				t.Errorf("start request backend = %v, want %q", got, backend)
			}
			if got := startBody["project_dir"]; got != projectDir {
				t.Errorf("start request project_dir = %v, want %q", got, projectDir)
			}
			if tc.node != "" {
				if got := startBody["compute_node"]; got != tc.node {
					t.Errorf("start request compute_node = %v, want %q", got, tc.node)
				}
			} else if _, ok := startBody["compute_node"]; ok {
				t.Errorf("local one-shot posted compute_node = %v; it must be left to the local node", startBody["compute_node"])
			}

			// System tags the daemon derives from that start.
			tags := systemTagsForStartBody(startBody)
			want := []string{
				"compute:" + tc.wantNodeID,
				"role:" + tc.wantRole,
				"kind:" + tc.wantKind,
				"automaton:" + prd,
				"backend:" + backend,
				"lane:" + launchLaneForTest(backend),
				"project:" + filepath.Base(projectDir),
			}
			if tc.wantStage != "" {
				want = append(want, "stage:"+tc.wantStage)
			}
			for _, w := range want {
				if !containsTag(tags, w) {
					t.Errorf("system tags %v missing %q", tags, w)
				}
			}
			if tc.wantStage == "" {
				for _, tag := range tags {
					if strings.HasPrefix(tag, "stage:") {
						t.Errorf("system tags %v carry stage tag %q; this site must carry none", tags, tag)
					}
				}
			}
		})
	}
}

// systemTagsForStartBody rebuilds the session record the daemon makes from a
// /api/sessions/start body and returns the placement tags it would carry under
// the default placement config (master on, every axis on).
func systemTagsForStartBody(body map[string]any) []string {
	str := func(k string) string {
		v, _ := body[k].(string)
		return v
	}
	rec := &session.Session{
		ProjectDir:     str("project_dir"),
		BackendFamily:  str("backend"),
		ComputeNodeRef: str("compute_node"),
		PRDID:          str("prd_id"),
		LaunchRole:     str("launch_role"),
		LaunchKind:     str("launch_kind"),
		LaunchStage:    str("launch_stage"),
	}
	p := autonomous.SessionPlacement(rec, launchLocalNodeForTest, nil)
	if p.Lane == "" {
		p.Lane = launchLaneForTest(rec.BackendFamily)
	}
	return autonomous.DerivePlacementTags(p, config.PlacementConfig{}.EffectivePlacement())
}

// launchLocalNodeForTest is the node name a local one-shot is placed on; it
// mirrors launchLocalNode in internal/server/session_launch_tags.go.
const launchLocalNodeForTest = "local"

// launchLaneForTest mirrors launchLaneForBackend in
// internal/server/session_launch_tags.go: the lane for the "claude-code"
// family, which autonomous.laneForBackend does not know.
func launchLaneForTest(backend string) string {
	if backend == "claude-code" {
		return string(autonomous.LaneTypeClaude)
	}
	return ""
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
