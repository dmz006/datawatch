package autonomous

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Read models for the checks and ship status surfaces (REST, MCP, CLI and
// the comm channel). Findings are the notes the checks and the ship step
// already append to an Automaton's triage record; nothing here writes.

// ChecksFindingSources are the triage-note sources written by the automatic
// checks.
var ChecksFindingSources = []string{prestartTriageSource, afterDoneTriageSource, registryTriageSource}

// ShipFindingSources are the triage-note sources written by the ship step.
var ShipFindingSources = []string{shipTriageSource, shipCIWatchTriageSource}

// StatusFinding is one recorded finding.
type StatusFinding struct {
	PRDID  string    `json:"prd_id"`
	At     time.Time `json:"at"`
	Source string    `json:"source"`
	Text   string    `json:"text"`
}

// ChecksPolicyView is the resolved checks policy, as JSON.
type ChecksPolicyView struct {
	PreStartVerifier string `json:"pre_start_verifier"`
	BlockWhenMissing bool   `json:"block_when_missing"`
	PlanRules        string `json:"plan_rules"`
	AfterDone        string `json:"after_done"`
	RegistryReadme   string `json:"registry_readme"`
}

// ShipPolicyView is the resolved ship policy, as JSON.
type ShipPolicyView struct {
	Push        string   `json:"push"`
	Tag         string   `json:"tag"`
	CIWatch     string   `json:"ci_watch"`
	TagFormat   string   `json:"tag_format"`
	Remote      string   `json:"remote"`
	CIWorkflows []string `json:"ci_workflows"`
}

// ChecksStatus is the body of the checks status read.
type ChecksStatus struct {
	PRDID      string           `json:"prd_id,omitempty"`
	ProjectDir string           `json:"project_dir,omitempty"`
	Policy     ChecksPolicyView `json:"policy"`
	Findings   []StatusFinding  `json:"findings"`
	// Attribution is the cost-report attribution check (Automaton-launched
	// sessions lacking an automaton id); nil when no ledger is wired.
	Attribution *AttributionStatus `json:"attribution,omitempty"`
}

// AttributionStatus mirrors costreport.AttributionCheck for the status body.
type AttributionStatus struct {
	AutomatonLaunched int      `json:"automaton_launched"`
	Unattributed      int      `json:"unattributed"`
	SessionIDs        []string `json:"session_ids"`
	OK                bool     `json:"ok"`
	Note              string   `json:"note"`
}

// ShipStatus is the body of the ship status read.
type ShipStatus struct {
	PRDID      string          `json:"prd_id,omitempty"`
	ProjectDir string          `json:"project_dir,omitempty"`
	Policy     ShipPolicyView  `json:"policy"`
	Findings   []StatusFinding `json:"findings"`
}

// CollectStatusFindings returns the notes of the records (narrowed to prdID
// when set) whose source is in sources, newest first.
func CollectStatusFindings(recs []*TriageRecord, prdID string, sources []string) []StatusFinding {
	want := map[string]bool{}
	for _, s := range sources {
		want[s] = true
	}
	out := []StatusFinding{}
	for _, rec := range recs {
		if rec == nil || (prdID != "" && rec.PRDID != prdID) {
			continue
		}
		for _, n := range rec.Notes {
			if want[n.Source] {
				out = append(out, StatusFinding{PRDID: rec.PRDID, At: n.At, Source: n.Source, Text: n.Text})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	return out
}

// BuildChecksStatus resolves the checks policy for projectDir and collects
// the recorded check findings.
func BuildChecksStatus(recs []*TriageRecord, prdID, projectDir string) ChecksStatus {
	p := ChecksPolicyFor(projectDir)
	return ChecksStatus{
		PRDID: prdID, ProjectDir: projectDir,
		Policy:   ChecksPolicyView(p),
		Findings: CollectStatusFindings(recs, prdID, ChecksFindingSources),
	}
}

// BuildShipStatus resolves the ship policy for projectDir and collects the
// recorded ship outcomes.
func BuildShipStatus(recs []*TriageRecord, prdID, projectDir string) ShipStatus {
	p := ShipPolicyFor(projectDir)
	wf := p.CIWorkflows
	if wf == nil {
		wf = []string{}
	}
	return ShipStatus{
		PRDID: prdID, ProjectDir: projectDir,
		Policy: ShipPolicyView{
			Push: p.Push, Tag: p.Tag, CIWatch: p.CIWatch,
			TagFormat: p.TagFormat, Remote: p.Remote, CIWorkflows: wf,
		},
		Findings: CollectStatusFindings(recs, prdID, ShipFindingSources),
	}
}

// ParseChecksStatus decodes a checks status body.
func ParseChecksStatus(b []byte) (ChecksStatus, error) {
	var s ChecksStatus
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("decode checks status: %w", err)
	}
	return s, nil
}

// ParseShipStatus decodes a ship status body.
func ParseShipStatus(b []byte) (ShipStatus, error) {
	var s ShipStatus
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("decode ship status: %w", err)
	}
	return s, nil
}

func renderFindings(sb *strings.Builder, fs []StatusFinding) {
	if len(fs) == 0 {
		sb.WriteString("no findings recorded\n")
		return
	}
	for _, f := range fs {
		fmt.Fprintf(sb, "%s  %s  [%s] %s\n", f.At.Format("2006-01-02 15:04"), f.PRDID, f.Source, f.Text)
	}
}

// RenderChecksStatusText is the one wording the CLI and the comm verb share.
func RenderChecksStatusText(s ChecksStatus) string {
	var sb strings.Builder
	p := s.Policy
	fmt.Fprintf(&sb, "checks: pre_start_verifier=%s block_when_missing=%t plan_rules=%s after_done=%s registry_readme=%s\n",
		p.PreStartVerifier, p.BlockWhenMissing, p.PlanRules, p.AfterDone, p.RegistryReadme)
	if a := s.Attribution; a != nil {
		fmt.Fprintf(&sb, "%s\n", a.Note)
	}
	renderFindings(&sb, s.Findings)
	return sb.String()
}

// RenderShipStatusText is the one wording the CLI and the comm verb share.
func RenderShipStatusText(s ShipStatus) string {
	var sb strings.Builder
	p := s.Policy
	fmt.Fprintf(&sb, "ship: push=%s tag=%s ci_watch=%s tag_format=%s remote=%s ci_workflows=%s\n",
		p.Push, p.Tag, p.CIWatch, p.TagFormat, p.Remote, strings.Join(p.CIWorkflows, ","))
	renderFindings(&sb, s.Findings)
	return sb.String()
}
