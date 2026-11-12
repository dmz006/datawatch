// Registry-versus-README check — report-only. It parses the registry table
// rows in docs/plans/README.md and compares them with the plan documents under
// docs/plans/. It never writes to any file; the checks policy mode
// (ChecksPolicy.RegistryReadme) only decides whether findings are recorded
// (shadow) or also alerted (on).
package autonomous

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Registry finding kinds.
const (
	RegistryMissingRow  = "missing_row"   // a plan document no row links to
	RegistryDuplicate   = "duplicate_row" // the same item number or id twice
	RegistryMissingFile = "missing_file"  // a row links to a file that does not exist
)

const registryTriageSource = "registry_readme_check"

// RegistryRow is one parsed table row.
type RegistryRow struct {
	Num  int    // the "#" column, 0 when not numeric
	Item string // the Item column text
	Link string // the first relative .md link target, "" when none
	Raw  string // the whole row
}

// RegistryFinding is one reported problem.
type RegistryFinding struct {
	Kind   string `json:"kind"`
	Row    int    `json:"row,omitempty"`
	File   string `json:"file,omitempty"`
	Detail string `json:"detail"`
}

var (
	registryRowRe  = regexp.MustCompile(`^\|\s*(\d+)\s*\|(.*)$`)
	registryLinkRe = regexp.MustCompile(`\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)
	registryIDRe   = regexp.MustCompile(`\bBL\d+\b`)
	planDocRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-.+\.md$`)
)

// ParseRegistryRows reads the README at path and returns every table row whose
// first cell is a number. Header and separator lines are skipped.
func ParseRegistryRows(path string) ([]RegistryRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rows []RegistryRow
	for _, line := range strings.Split(string(b), "\n") {
		m := registryRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		cells := strings.Split(m[2], "|")
		row := RegistryRow{Num: n, Raw: line}
		if len(cells) > 0 {
			row.Item = strings.TrimSpace(cells[0])
		}
		if lm := registryLinkRe.FindStringSubmatch(row.Item); lm != nil {
			row.Link = lm[1]
		} else if lm := registryLinkRe.FindStringSubmatch(line); lm != nil {
			row.Link = lm[1]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// CheckRegistryReadme compares the README registry in dir with the plan
// documents under dir/docs/plans. It only reads files. A README that cannot be
// read yields an error; otherwise findings are sorted and non-nil.
func CheckRegistryReadme(dir string) ([]RegistryFinding, error) {
	plans := filepath.Join(dir, "docs", "plans")
	rows, err := ParseRegistryRows(filepath.Join(plans, "README.md"))
	if err != nil {
		return nil, err
	}
	out := []RegistryFinding{}
	linked := map[string]bool{}
	seenNum := map[int]bool{}
	seenID := map[string]int{}
	for _, r := range rows {
		if seenNum[r.Num] {
			out = append(out, RegistryFinding{Kind: RegistryDuplicate, Row: r.Num, Detail: fmt.Sprintf("row number %d appears more than once", r.Num)})
		}
		seenNum[r.Num] = true
		// Only a leading id in the Item cell identifies a row; later mentions
		// are cross-references.
		if id := registryIDRe.FindString(r.Item); id != "" && strings.HasPrefix(strings.TrimLeft(r.Item, "*[ "), id) {
			if first, ok := seenID[id]; ok {
				out = append(out, RegistryFinding{Kind: RegistryDuplicate, Row: r.Num, Detail: fmt.Sprintf("%s appears in rows %d and %d", id, first, r.Num)})
			} else {
				seenID[id] = r.Num
			}
		}
		if r.Link == "" || strings.Contains(r.Link, "://") {
			continue
		}
		target := filepath.Clean(filepath.Join(plans, r.Link))
		linked[filepath.ToSlash(target)] = true
		if st, err := os.Stat(target); err != nil || st.IsDir() {
			out = append(out, RegistryFinding{Kind: RegistryMissingFile, Row: r.Num, File: r.Link, Detail: fmt.Sprintf("row %d points at %s, which does not exist", r.Num, r.Link)})
		}
	}
	entries, err := os.ReadDir(plans)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !planDocRe.MatchString(e.Name()) {
			continue
		}
		if !linked[filepath.ToSlash(filepath.Join(plans, e.Name()))] {
			out = append(out, RegistryFinding{Kind: RegistryMissingRow, File: e.Name(), Detail: "plan document " + e.Name() + " has no registry row"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Row != out[j].Row {
			return out[i].Row < out[j].Row
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// RecordRegistryReadmeCheck runs the check per the checks policy for dir: off
// does nothing; shadow records findings on the Automaton's triage record; on
// also marks it alerted. It writes no project file. Returns nil when off.
func (m *Manager) RecordRegistryReadmeCheck(prd *PRD, dir string) ([]RegistryFinding, error) {
	mode := ChecksPolicyFor(dir).RegistryReadme
	if mode == config.ChecksModeOff || mode == "" {
		return nil, nil
	}
	findings, err := CheckRegistryReadme(dir)
	if err != nil {
		return nil, err
	}
	if len(findings) > 0 && prd != nil {
		lines := make([]string, 0, len(findings))
		for _, f := range findings {
			lines = append(lines, f.Kind+": "+f.Detail)
		}
		m.recordCheckFindings(prd, mode, registryTriageSource, "registry-versus-README", lines, true)
	}
	return findings, nil
}
