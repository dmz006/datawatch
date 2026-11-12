// api_dashboard_console.go — the operator console one-glance summary:
// GET /api/dashboard/console.
//
// The console rolls up five sections — questions_waiting (open operator
// questions, including held questions), held_blocked (automata stuck in a
// failed or blocked triage state, plus uncommitted-work holds), lanes (lane
// fill and the capacity admission wait queue), usage (the burn guard and the
// usage advice projection) and ci_release (repo CI watch records and the
// latest-release check) — so the operator sees at a glance which parts of
// the daemon need attention. Every section reports a count, a short
// operator-readable summary line and the PWA anchor where the operator acts
// on it.
//
// Sections the operator switched off (dashboard.console.enabled, or one of
// the show_* keys) report available=false with a reason naming the exact
// key; the same shape is returned when the section's backing subsystem is
// not wired in on this daemon. The console is always readable — a missing
// or failing subsystem never becomes an HTTP 5xx here.
//
//	GET /api/dashboard/console   dashboard:read
package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/usagecalc"
)

// PWA anchors the section's "link" points at. The anchors are the PWA's
// view ids; #settings is where the console switches live.
const (
	consoleAnchorQuestions   = "#questions"
	consoleAnchorHeldBlocked = "#held-questions"
	consoleAnchorLanes       = "#lanes"
	consoleAnchorUsage       = "#usage"
	consoleAnchorCIRelease   = "#releases"
	consoleAnchorSettings    = "#settings"
)

// consoleSection is one section of the console summary. Available is false
// (with Reason naming the key or the missing subsystem) when the operator
// hid it or the daemon cannot read it; Count and Summary are filled only
// when the section is available.
type consoleSection struct {
	Available bool   `json:"available"`
	Count     int    `json:"count"`
	Summary   string `json:"summary,omitempty"`
	Link      string `json:"link,omitempty"`
	Reason    string `json:"reason,omitempty"`
	// Release is the CI & release detail, set on the ci_release section only.
	Release *consoleReleaseDetail `json:"release,omitempty"`
}

// consoleReleaseDetail is the structured CI & release state the card draws:
// the running and latest release tags, the ship policy with the newest ship
// note, and the repo CI watch state. The watch being off is reported in
// WatchOff (the card then shows the next action from WatchLink) while the
// release and ship state stay filled in. FailingRuns lists the newest
// failing or hung runs, and only while the watch is on.
type consoleReleaseDetail struct {
	CurrentVersion string              `json:"current_version"`
	LatestRelease  string              `json:"latest_release,omitempty"`
	ReleaseState   string              `json:"release_state"`
	WatchMode      string              `json:"watch_mode"`
	WatchOff       bool                `json:"watch_off"`
	WatchLink      string              `json:"watch_link"`
	ShipPush       string              `json:"ship_push"`
	ShipTag        string              `json:"ship_tag"`
	ShipCIWatch    string              `json:"ship_ci_watch"`
	ShipLastNote   string              `json:"ship_last_note,omitempty"`
	FailingRuns    []consoleFailingRun `json:"failing_runs"`
}

// consoleFailingRun is one failing or hung CI run under the repo CI watch.
type consoleFailingRun struct {
	Repo     string `json:"repo"`
	Workflow string `json:"workflow"`
	RunID    string `json:"run_id"`
	Status   string `json:"status"`
	RunURL   string `json:"run_url,omitempty"`
}

// consoleFailingRunLimit caps the failing-run list on the card.
const consoleFailingRunLimit = 5

// consoleReleaseStates are the release_state values the card reads.
const (
	consoleReleaseUpToDate  = "up_to_date"
	consoleReleaseAvailable = "update_available"
	consoleReleaseCheckFail = "check_failed"
	consoleReleaseNotWired  = "not_wired"
)

type dashboardConsoleResponse struct {
	GeneratedAt      time.Time      `json:"generated_at"`
	QuestionsWaiting consoleSection `json:"questions_waiting"`
	HeldBlocked      consoleSection `json:"held_blocked"`
	Lanes            consoleSection `json:"lanes"`
	Usage            consoleSection `json:"usage"`
	CIRelase         consoleSection `json:"ci_release"`
}

// handleDashboardConsole serves the operator console one-glance summary.
// Read-only; gated on the session's (or peer's) dashboard:read capability.
// The dashboard.console.* switches hide the whole endpoint or one section
// each; both read their default, true, when unset.
func (s *Server) handleDashboardConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed: only GET is supported here", http.StatusNotImplemented)
		return
	}
	if !s.fedCap(w, r, federation.CapDashboardRead) {
		return
	}

	c := consoleConfigOf(s.cfg)
	ctx := r.Context()
	resp := dashboardConsoleResponse{GeneratedAt: time.Now().UTC()}

	if !c.EffectiveEnabled() {
		reason := "the operator console is disabled — set dashboard.console.enabled to true"
		resp.QuestionsWaiting = disabledConsoleSection(reason)
		resp.HeldBlocked = disabledConsoleSection(reason)
		resp.Lanes = disabledConsoleSection(reason)
		resp.Usage = disabledConsoleSection(reason)
		resp.CIRelase = disabledConsoleSection(reason)
		writeJSONOK(w, resp)
		return
	}

	resp.QuestionsWaiting = s.consoleSection(ctx, "dashboard.console.show_questions", consoleAnchorQuestions, c.EffectiveShowQuestions(), s.buildQuestionsWaiting)
	resp.HeldBlocked = s.consoleSection(ctx, "dashboard.console.show_held_blocked", consoleAnchorHeldBlocked, c.EffectiveShowHeld(), s.buildHeldBlocked)
	resp.Lanes = s.consoleSection(ctx, "dashboard.console.show_lanes", consoleAnchorLanes, c.EffectiveShowLanes(), s.buildLanes)
	resp.Usage = s.consoleSection(ctx, "dashboard.console.show_usage", consoleAnchorUsage, c.EffectiveShowUsage(), s.buildUsage)
	var release *consoleReleaseDetail
	resp.CIRelase = s.consoleSection(ctx, "dashboard.console.show_ci_release", consoleAnchorCIRelease, c.EffectiveShowCIRelease(),
		func(ctx context.Context) (int, string, error) {
			count, summary, detail, err := s.ciReleaseState(ctx)
			release = detail
			return count, summary, err
		})
	if resp.CIRelase.Available {
		resp.CIRelase.Release = release
	}

	writeJSONOK(w, resp)
}

// disabledConsoleSection is the section shape for a console the operator
// has switched off, or an individual section the show_* key hides: nothing
// but a reason pointing at the settings switch.
func disabledConsoleSection(reason string) consoleSection {
	return consoleSection{Reason: reason, Link: consoleAnchorSettings}
}

// consoleSection assembles one console section: the operator's show switch
// off yields an unavailable section naming that key, the builder's error
// (a subsystem this daemon cannot read) yields an unavailable section with
// that reason; otherwise the live count, summary and anchor link are
// returned.
func (s *Server) consoleSection(ctx context.Context, key, link string, show bool, build func(context.Context) (int, string, error)) consoleSection {
	if !show {
		return consoleSection{Reason: "hidden by the " + key + " switch — set it to true to show this section", Link: consoleAnchorSettings}
	}
	count, summary, err := build(ctx)
	if err != nil {
		return consoleSection{Reason: err.Error(), Link: consoleAnchorSettings}
	}
	return consoleSection{Available: true, Count: count, Summary: summary, Link: link}
}

// consoleConfigOf isolates the dashboard.console block; a nil daemon config
// (never expected, but this endpoint must stay defensive) reads every
// switch at its default, true.
func consoleConfigOf(cfg *config.Config) config.DashboardConsoleConfig {
	if cfg == nil {
		return config.DashboardConsoleConfig{}
	}
	return cfg.Dashboard.Console
}

// buildQuestionsWaiting counts the open operator questions — the question
// channel including held questions — splitting blocking from waiting.
func (s *Server) buildQuestionsWaiting(context.Context) (int, string, error) {
	if s.cfg != nil && !s.cfg.Autonomous.Questions.Enabled {
		return 0, "", fmt.Errorf("operator questions are disabled — set autonomous.questions.enabled to true")
	}
	if s.questionStore == nil {
		return 0, "", fmt.Errorf("the operator question channel is not wired in on this daemon")
	}
	open := s.questionStore.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen})
	blocking, held := 0, 0
	for _, q := range open {
		if q.Blocking {
			blocking++
		}
		if q.Source == autonomous.QuestionSourceHeld {
			held++
		}
	}
	return len(open), fmt.Sprintf("%d open question(s), %d blocking, %d held", len(open), blocking, held), nil
}

// buildHeldBlocked counts the automata stuck in a failed or blocked triage
// state (the merge-pending ones excluded — they are on their way) plus the
// uncommitted-work holds.
func (s *Server) buildHeldBlocked(context.Context) (int, string, error) {
	if s.autonomousMgr == nil {
		return 0, "", fmt.Errorf("the autonomous manager is not wired in on this daemon (autonomous mode off)")
	}
	tr, ok := s.autonomousMgr.(triageReader)
	if !ok {
		return 0, "", fmt.Errorf("the failed/blocked triage records are unavailable on this daemon")
	}
	records := tr.ListTriageRecords()
	failed, blocked := 0, 0
	for _, rec := range records {
		switch rec.Status {
		case "failed":
			failed++
		case "blocked":
			blocked++
		}
	}
	parts := []string{fmt.Sprintf("%d automaton(s) stuck: %d failed, %d blocked", len(records), failed, blocked)}
	if uw, okay := s.autonomousMgr.(uncommittedWorkReader); okay {
		if st := uw.UncommittedWorkStatus(""); len(st.Held) > 0 {
			parts = append(parts, fmt.Sprintf("%d uncommitted-work hold(s)", len(st.Held)))
		}
	}
	return len(records), strings.Join(parts, "; "), nil
}

// buildLanes rolls up the lane fill lane states and the capacity admission
// wait queue; either subsystem alone is enough to serve the section.
func (s *Server) buildLanes(context.Context) (int, string, error) {
	if s.autonomousMgr == nil && s.capacityLedger == nil {
		return 0, "", fmt.Errorf("neither lane fill nor the capacity ledger is wired in on this daemon")
	}
	var parts []string
	count := 0
	if s.autonomousMgr != nil {
		st := s.autonomousMgr.LaneFillStatus(0)
		count += len(st.Lanes)
		inUse, idle := 0, 0
		for _, lane := range st.Lanes {
			if lane.State == "idle" {
				idle++
			} else {
				inUse++
			}
		}
		parts = append(parts, fmt.Sprintf("%d of %d lane(s) in use (lane fill mode %s, %d idle)", inUse, len(st.Lanes), st.Mode, idle))
	}
	if s.capacityLedger != nil {
		if st := s.capacityLedger.Snapshot(); len(st.Waiting) > 0 {
			count += len(st.Waiting)
			parts = append(parts, fmt.Sprintf("%d automaton(s) waiting for capacity", len(st.Waiting)))
		}
	}
	if len(parts) == 0 {
		return 0, "no lanes or capacity waiters to report", nil
	}
	return count, strings.Join(parts, "; "), nil
}

// buildUsage rolls up the burn guard state and the usage advice projection;
// the count is the weekly usage percent at the reading when the advice is
// usable. The section is available when at least one of the two subsystems
// can be read.
func (s *Server) buildUsage(ctx context.Context) (int, string, error) {
	if s.burnGuard == nil && s.usageCalc == nil {
		return 0, "", fmt.Errorf("neither the burn guard nor the usage calculator is wired in on this daemon")
	}
	var parts []string
	count := -1
	if s.burnGuard != nil {
		st := s.burnGuard.Status()
		line := fmt.Sprintf("burn guard %s (level %d)", st.State, st.Level)
		if p := st.LastProjection; p != nil && p.Needs == "" {
			if p.Target > 0 {
				line += fmt.Sprintf(", target %.0f%%", p.Target)
			}
			if p.Over {
				line += ", projected over target"
			}
		}
		parts = append(parts, line)
	}
	if s.usageCalc != nil {
		res, err := s.usageCalc.Advice(ctx, usagecalc.Options{})
		if err != nil {
			if s.burnGuard == nil {
				return 0, "", fmt.Errorf("the usage calculator is wired in but its advice is unavailable: %v", err)
			}
			parts = append(parts, "usage advice unavailable: "+err.Error())
		} else {
			pct := int(res.Advice.Percent + 0.5)
			if count < 0 {
				count = pct
			}
			line := fmt.Sprintf("%d%% of the weekly window used", pct)
			if res.Stale {
				line += " (stale reading)"
			}
			if res.Advice.NextResetAt != nil {
				line += ", next reset in " + humanDuration(time.Until(*res.Advice.NextResetAt))
			}
			parts = append(parts, line)
		}
	}
	if count < 0 {
		count = 0
	}
	return count, strings.Join(parts, "; "), nil
}

// buildCIRelase rolls up the repo CI watch records (splitting the failing
// or hung ones) and the latest-release check.
func (s *Server) ciReleaseState(context.Context) (int, string, *consoleReleaseDetail, error) {
	if s.autonomousMgr == nil {
		return 0, "", nil, fmt.Errorf("the autonomous manager is not wired in on this daemon (autonomous mode off)")
	}
	rw, ok := s.autonomousMgr.(repoCIWatchReader)
	if !ok {
		return 0, "", nil, fmt.Errorf("the repo CI watch is not wired in on this daemon")
	}
	st := rw.RepoCIWatchStatus()
	attention := 0
	for _, rec := range st.Records {
		if rec.Status == "failing" || rec.Status == "hung" {
			attention++
		}
	}
	detail := &consoleReleaseDetail{
		CurrentVersion: Version,
		WatchMode:      st.Mode,
		WatchOff:       st.Mode == autonomous.RepoCIWatchModeOff || st.Mode == "",
		WatchLink:      consoleAnchorSettings,
		FailingRuns:    []consoleFailingRun{},
	}
	parts := []string{fmt.Sprintf("%d CI run(s) under watch across %d repo(s), %d failing or hung (watch mode %s)", len(st.Records), len(st.Repos), attention, st.Mode)}

	if !detail.WatchOff {
		detail.FailingRuns = consoleFailingRuns(st.Records)
	}

	if tr, ok := s.autonomousMgr.(triageReader); ok {
		ship := autonomous.BuildShipStatus(tr.ListTriageRecords(), "", "")
		detail.ShipPush = ship.Policy.Push
		detail.ShipTag = ship.Policy.Tag
		detail.ShipCIWatch = ship.Policy.CIWatch
		if len(ship.Findings) > 0 {
			detail.ShipLastNote = ship.Findings[0].Text
		}
		parts = append(parts, fmt.Sprintf("ship: push %s, tag %s, CI watch %s", ship.Policy.Push, ship.Policy.Tag, ship.Policy.CIWatch))
	}

	switch s.latestVersion {
	case nil:
		detail.ReleaseState = consoleReleaseNotWired
		parts = append(parts, "release: the latest-version check is not wired in")
	default:
		latest, err := s.latestVersion()
		switch {
		case err != nil:
			detail.ReleaseState = consoleReleaseCheckFail
			parts = append(parts, "release: latest-version check failed ("+err.Error()+")")
		case latest == "" || !semverIsNewer(latest, Version):
			detail.ReleaseState = consoleReleaseUpToDate
			detail.LatestRelease = latest
			parts = append(parts, "release: up to date (v"+Version+")")
		default:
			detail.ReleaseState = consoleReleaseAvailable
			detail.LatestRelease = latest
			parts = append(parts, "release: update available — v"+latest)
		}
	}
	return len(st.Records), strings.Join(parts, "; "), detail, nil
}

// consoleFailingRuns picks the newest failing or hung runs, capped at
// consoleFailingRunLimit. The input is not modified.
func consoleFailingRuns(recs []*autonomous.RepoCIWatchRecord) []consoleFailingRun {
	var hit []*autonomous.RepoCIWatchRecord
	for _, rec := range recs {
		if rec != nil && (rec.Status == "failing" || rec.Status == "hung") {
			hit = append(hit, rec)
		}
	}
	sort.SliceStable(hit, func(i, j int) bool { return hit[i].UpdatedAt.After(hit[j].UpdatedAt) })
	out := []consoleFailingRun{}
	for _, rec := range hit {
		if len(out) == consoleFailingRunLimit {
			break
		}
		out = append(out, consoleFailingRun{
			Repo: rec.Repo, Workflow: rec.Workflow, RunID: rec.RunID, Status: rec.Status, RunURL: rec.RunURL,
		})
	}
	return out
}

// humanDuration renders a non-negative interval ("Xd Yh", "Yh Zm", "Zm")
// for console summary lines.
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalMin := int(d / time.Minute)
	days, rem := totalMin/(24*60), totalMin%(24*60)
	hours, min := rem/60, rem%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dmin", hours, min)
	default:
		return fmt.Sprintf("%dmin", min)
	}
}
