// After-done checklist — a report-only check of a completed Automaton.
// Each item records pass, fail or n-a with a reason. The check never
// writes to any file; the checks policy mode (ChecksPolicy.AfterDone)
// only decides whether findings are recorded (shadow) or also alerted (on).
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// Checklist outcomes.
const (
	ChecklistPass = "pass"
	ChecklistFail = "fail"
	ChecklistNA   = "n-a"
)

// Checklist item names.
const (
	ChecklistVersionSync = "version_sync"
	ChecklistChangelog   = "changelog_entry"
	ChecklistConfigRef   = "config_reference"
	ChecklistParityIssue = "parity_issue"
	ChecklistRegistryRow = "registry_row"
)

// afterDoneTriageSource is the Source of the note written to the triage record.
const afterDoneTriageSource = "after_done_checklist"

// ChecklistItem is one recorded item.
type ChecklistItem struct {
	Item    string `json:"item"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

// AfterDoneInput carries what the checklist needs. All paths are relative to
// Dir. Empty optional inputs make the dependent item n-a.
type AfterDoneInput struct {
	Dir string
	// PreviousVersion is the previous release. When empty it is derived from
	// the CHANGELOG heading that follows the current version's heading.
	PreviousVersion string
	// ChangedFiles are the repo-relative files the Automaton touched.
	ChangedFiles []string
	// ConfigKeyChanged is true when the Automaton changed a config key.
	ConfigKeyChanged bool
	// ParityRef is the datawatch-app parity issue reference ("" = none);
	// ParityNotApplicable true marks an Automaton that changed no PWA surface.
	ParityRef           string
	ParityNotApplicable bool
	// RegistryKey is the text that must appear in a docs/plans/README.md
	// registry row for this Automaton (for example its BL id). Empty = n-a.
	RegistryKey string
}

var (
	versionVarRe    = regexp.MustCompile(`(?m)^\s*var\s+Version\s*=\s*"([^"]+)"`)
	changelogHeadRe = regexp.MustCompile(`(?m)^##\s+v?(\d+\.\d+\.\d+)\b`)
)

// RunAfterDoneChecklist evaluates the five items. It only reads files.
func RunAfterDoneChecklist(in AfterDoneInput) []ChecklistItem {
	items := []ChecklistItem{}
	add := func(item, outcome, reason string) {
		items = append(items, ChecklistItem{Item: item, Outcome: outcome, Reason: reason})
	}

	// 1. version sync and bump.
	mainV, e1 := readVersionVar(filepath.Join(in.Dir, "cmd/datawatch/main.go"))
	apiV, e2 := readVersionVar(filepath.Join(in.Dir, "internal/server/api.go"))
	cl, clErr := os.ReadFile(filepath.Join(in.Dir, "CHANGELOG.md"))
	switch {
	case e1 != nil || e2 != nil:
		add(ChecklistVersionSync, ChecklistFail, fmt.Sprintf("cannot read version strings (main.go: %v; api.go: %v)", e1, e2))
	case mainV != apiV:
		add(ChecklistVersionSync, ChecklistFail, fmt.Sprintf("main.go has %s but api.go has %s", mainV, apiV))
	default:
		prev := in.PreviousVersion
		if prev == "" && clErr == nil {
			prev = previousChangelogVersion(string(cl), mainV)
		}
		switch {
		case prev == "":
			add(ChecklistVersionSync, ChecklistNA, "versions match ("+mainV+"); no previous release known to compare")
		case compareSemver(mainV, prev) > 0:
			add(ChecklistVersionSync, ChecklistPass, fmt.Sprintf("both files have %s, higher than %s", mainV, prev))
		default:
			add(ChecklistVersionSync, ChecklistFail, fmt.Sprintf("version %s is not higher than the previous release %s", mainV, prev))
		}
	}

	// 2. CHANGELOG entry.
	switch {
	case e1 != nil:
		add(ChecklistChangelog, ChecklistNA, "no version string to look up")
	case clErr != nil:
		add(ChecklistChangelog, ChecklistFail, "CHANGELOG.md cannot be read: "+clErr.Error())
	case hasChangelogVersion(string(cl), mainV):
		add(ChecklistChangelog, ChecklistPass, "CHANGELOG.md has an entry for "+mainV)
	default:
		add(ChecklistChangelog, ChecklistFail, "CHANGELOG.md has no entry for "+mainV)
	}

	// 3. config reference.
	switch {
	case !in.ConfigKeyChanged:
		add(ChecklistConfigRef, ChecklistNA, "no config key changed")
	case containsPath(in.ChangedFiles, "docs/config-reference.yaml"):
		add(ChecklistConfigRef, ChecklistPass, "docs/config-reference.yaml was touched")
	default:
		add(ChecklistConfigRef, ChecklistFail, "a config key changed but docs/config-reference.yaml was not touched")
	}

	// 4. parity issue.
	switch {
	case strings.TrimSpace(in.ParityRef) != "":
		add(ChecklistParityIssue, ChecklistPass, "datawatch-app parity issue "+strings.TrimSpace(in.ParityRef))
	case in.ParityNotApplicable:
		add(ChecklistParityIssue, ChecklistNA, "no PWA surface changed")
	default:
		add(ChecklistParityIssue, ChecklistFail, "no datawatch-app parity issue reference")
	}

	// 5. registry row.
	if strings.TrimSpace(in.RegistryKey) == "" {
		add(ChecklistRegistryRow, ChecklistNA, "no registry key supplied")
	} else if rows, err := ParseRegistryRows(filepath.Join(in.Dir, "docs/plans/README.md")); err != nil {
		add(ChecklistRegistryRow, ChecklistFail, "docs/plans/README.md cannot be read: "+err.Error())
	} else {
		found := false
		for _, r := range rows {
			if strings.Contains(r.Raw, in.RegistryKey) {
				found = true
				break
			}
		}
		if found {
			add(ChecklistRegistryRow, ChecklistPass, "registry row for "+in.RegistryKey+" exists")
		} else {
			add(ChecklistRegistryRow, ChecklistFail, "no registry row for "+in.RegistryKey+" in docs/plans/README.md")
		}
	}
	return items
}

// ChecklistFailures returns only the failed items.
func ChecklistFailures(items []ChecklistItem) []ChecklistItem {
	var out []ChecklistItem
	for _, it := range items {
		if it.Outcome == ChecklistFail {
			out = append(out, it)
		}
	}
	return out
}

// RecordAfterDoneChecklist runs the checklist per the checks policy for the
// input's directory: off does nothing; shadow records the result as a triage
// note; on also marks the record alerted. Returns the items (nil when off).
// It writes no file in the project; only the Automaton's triage record.
func (m *Manager) RecordAfterDoneChecklist(prd *PRD, in AfterDoneInput) []ChecklistItem {
	mode := ChecksPolicyFor(in.Dir).AfterDone
	if mode == config.ChecksModeOff || mode == "" {
		return nil
	}
	items := RunAfterDoneChecklist(in)
	if prd == nil {
		return items
	}
	m.recordCheckFindings(prd, mode, afterDoneTriageSource, "after-done checklist", formatChecklist(items), len(ChecklistFailures(items)) > 0)
	return items
}

func formatChecklist(items []ChecklistItem) []string {
	lines := make([]string, 0, len(items))
	for _, it := range items {
		lines = append(lines, fmt.Sprintf("%s: %s (%s)", it.Item, it.Outcome, it.Reason))
	}
	return lines
}

// recordCheckFindings writes lines as one note on the Automaton's triage
// record, creating the record in its existing shape when absent. When alert is
// true and mode is on, the record is also marked alerted.
func (m *Manager) recordCheckFindings(prd *PRD, mode, source, label string, lines []string, alert bool) {
	if m == nil || m.store == nil || prd == nil {
		return
	}
	if s, ok := m.store.SnapshotPRD(prd.ID); !ok || s.Triage == nil {
		fresh := &TriageRecord{PRDID: prd.ID, PRDTitle: prd.Title, Status: string(prd.Status)}
		now := time.Now()
		fresh.FirstSeenAt, fresh.UpdatedAt = now, now
		if err := m.SaveTriageRecord(prd.ID, fresh); err != nil {
			return
		}
	}
	text := label + " (" + mode + "): " + strings.Join(lines, "; ")
	_ = m.AppendTriageNote(prd.ID, source, text)
	if alert && mode == config.ChecksModeOn {
		now := time.Now()
		if s, ok := m.store.SnapshotPRD(prd.ID); ok && s.Triage != nil && s.Triage.AlertedAt == nil {
			fresh := *s.Triage
			fresh.AlertedAt = &now
			fresh.UpdatedAt = now
			_ = m.SaveTriageRecord(prd.ID, &fresh)
		}
	}
}

func readVersionVar(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := versionVarRe.FindSubmatch(b)
	if m == nil {
		return "", fmt.Errorf("no var Version in %s", filepath.Base(path))
	}
	return string(m[1]), nil
}

func hasChangelogVersion(cl, v string) bool {
	for _, m := range changelogHeadRe.FindAllStringSubmatch(cl, -1) {
		if m[1] == v {
			return true
		}
	}
	return false
}

// previousChangelogVersion is the first heading version below cur.
func previousChangelogVersion(cl, cur string) string {
	best := ""
	for _, m := range changelogHeadRe.FindAllStringSubmatch(cl, -1) {
		if compareSemver(m[1], cur) < 0 && (best == "" || compareSemver(m[1], best) > 0) {
			best = m[1]
		}
	}
	return best
}

func compareSemver(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x > y {
				return 1
			}
			return -1
		}
	}
	return 0
}

func containsPath(files []string, want string) bool {
	for _, f := range files {
		if filepath.ToSlash(filepath.Clean(f)) == want {
			return true
		}
	}
	return false
}
