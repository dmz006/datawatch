package changerecord

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// File statuses.
const (
	StatusAdded    = "added"
	StatusModified = "modified"
	StatusDeleted  = "deleted"
)

// Paths whose added lines carry settings, routes and locale keys.
const (
	configGoPath      = "internal/config/config.go"
	configRefPath     = "docs/config-reference.yaml"
	apiGoPath         = "internal/server/api.go"
	openAPIPath       = "docs/api/openapi.yaml"
	localeEnglishPath = "internal/server/web/locales/en.json"
)

// FileChange is one changed file.
type FileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// FileDiffData is the diff of one file: unified text plus the file contents
// before and after the change (empty when the file did not exist on that side).
type FileDiffData struct {
	Unified string
	Before  string
	After   string
}

// DiffSource supplies the change under inspection.
type DiffSource interface {
	ChangedFiles() ([]FileChange, error)
	FileDiff(path string) (FileDiffData, error)
}

// LocaleKey is an added locale key with its English value.
type LocaleKey struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CommandRun is one recorded command run with its outcome.
type CommandRun struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Pass     int    `json:"pass"`
	Fail     int    `json:"fail"`
}

// ChangeRecord is the structured description of a change.
type ChangeRecord struct {
	TaskID          string       `json:"task_id,omitempty"`
	AutomatonID     string       `json:"automaton_id,omitempty"`
	Timestamp       time.Time    `json:"timestamp"`
	Files           []FileChange `json:"files"`
	Packages        []string     `json:"packages"`
	SettingsAdded   []string     `json:"settings_added"`
	RoutesAdded     []string     `json:"routes_added"`
	LocaleKeysAdded []LocaleKey  `json:"locale_keys_added"`
	TestsAdded      []string     `json:"tests_added"`
	Commands        []CommandRun `json:"commands"`
	// Cost is the Automaton's spend read from the ledger; nil when no
	// ledger reading was attached.
	Cost *CostSummary `json:"cost,omitempty"`
}

// CostSummary is the ledger view of one Automaton's spend. The dollar figure
// is an API-equivalent estimate, never a charge.
type CostSummary struct {
	Sessions int `json:"sessions"`
	// SessionIDs lists the Automaton's sessions found in the ledger.
	SessionIDs    []string `json:"session_ids,omitempty"`
	TokensIn      int      `json:"tokens_in"`
	TokensOut     int      `json:"tokens_out"`
	CacheRead     int      `json:"cache_read"`
	CacheCreation int      `json:"cache_creation"`
	ToolCalls     int      `json:"tool_calls"`
	SpanSeconds   float64  `json:"span_seconds"`
	// USDLabel names the dollar figure (for example "API-equivalent
	// estimate, not billed"). Empty when there is no figure.
	USDLabel         string   `json:"usd_label,omitempty"`
	APIEquivalentUSD *float64 `json:"api_equivalent_usd,omitempty"`
	// WindowNote and UnknownNote carry the ledger's partial-window and
	// unknown-spend caveats verbatim.
	WindowNote  string `json:"window_note,omitempty"`
	UnknownNote string `json:"unknown_note,omitempty"`
}

// CostLedger reads one Automaton's cost from the spend ledger.
type CostLedger interface {
	AutomatonCost(automatonID string) (*CostSummary, error)
}

// AttachCost reads the Automaton's cost from the ledger onto the record. A
// nil ledger or an empty automaton id leaves the record unchanged.
func AttachCost(rec *ChangeRecord, ledger CostLedger) error {
	if rec == nil || ledger == nil || rec.AutomatonID == "" {
		return nil
	}
	c, err := ledger.AutomatonCost(rec.AutomatonID)
	if err != nil {
		return fmt.Errorf("changerecord: read cost: %w", err)
	}
	rec.Cost = c
	return nil
}

var (
	reYAMLTag  = regexp.MustCompile("`[^`]*yaml:\"([A-Za-z0-9_]+)[,\"]")
	reRefKey   = regexp.MustCompile(`^\s*#?\s*([a-z][a-z0-9_]*):`)
	reRoute    = regexp.MustCompile(`Handle(?:Func)?\(\s*"([^"]+)"`)
	reOpenPath = regexp.MustCompile(`^\s{2}(/[^\s:]*):\s*$`)
	reTestFunc = regexp.MustCompile(`^func (Test[A-Za-z0-9_]*)\(`)
	nowFunc    = func() time.Time { return time.Now().UTC() }
)

// BuildFromDiff derives a ChangeRecord from the diff and the recorded runs.
func BuildFromDiff(diff DiffSource, cmds []CommandRun) (*ChangeRecord, error) {
	if diff == nil {
		return nil, errors.New("changerecord: nil diff source")
	}
	files, err := diff.ChangedFiles()
	if err != nil {
		return nil, fmt.Errorf("changerecord: list changed files: %w", err)
	}
	rec := &ChangeRecord{Timestamp: nowFunc(), Commands: append([]CommandRun{}, cmds...)}
	var settings, routes, tests []string
	var locales []LocaleKey
	for _, f := range files {
		rec.Files = append(rec.Files, f)
		d, err := diff.FileDiff(f.Path)
		if err != nil {
			return nil, fmt.Errorf("changerecord: diff %s: %w", f.Path, err)
		}
		added := addedLines(d.Unified)
		switch {
		case f.Path == configGoPath:
			settings = append(settings, matchAll(added, reYAMLTag)...)
		case f.Path == configRefPath:
			settings = append(settings, matchAll(added, reRefKey)...)
		case f.Path == apiGoPath:
			routes = append(routes, matchAll(added, reRoute)...)
		case f.Path == openAPIPath:
			routes = append(routes, matchAll(added, reOpenPath)...)
		case f.Path == localeEnglishPath:
			ks, err := newLocaleKeys(d.Before, d.After)
			if err != nil {
				return nil, fmt.Errorf("changerecord: %s: %w", f.Path, err)
			}
			locales = append(locales, ks...)
		case strings.HasSuffix(f.Path, "_test.go") && f.Status != StatusDeleted:
			tests = append(tests, matchAll(added, reTestFunc)...)
		}
	}
	sortFiles(rec.Files)
	rec.Packages = packagesOf(rec.Files)
	rec.SettingsAdded = uniqSorted(settings)
	rec.RoutesAdded = uniqSorted(routes)
	rec.TestsAdded = uniqSorted(tests)
	rec.LocaleKeysAdded = uniqLocales(locales)
	return rec, nil
}

// addedLines returns the added lines of a unified diff, without the "+".
func addedLines(unified string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(unified))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "+++") || !strings.HasPrefix(l, "+") {
			continue
		}
		out = append(out, l[1:])
	}
	return out
}

func matchAll(lines []string, re *regexp.Regexp) []string {
	var out []string
	for _, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// newLocaleKeys returns keys present in after but not in before, flattened
// with dots, with their English value.
func newLocaleKeys(before, after string) ([]LocaleKey, error) {
	b, err := flattenJSON(before)
	if err != nil {
		return nil, fmt.Errorf("parse before: %w", err)
	}
	a, err := flattenJSON(after)
	if err != nil {
		return nil, fmt.Errorf("parse after: %w", err)
	}
	var out []LocaleKey
	for k, v := range a {
		if _, ok := b[k]; !ok {
			out = append(out, LocaleKey{Key: k, Value: v})
		}
	}
	return out, nil
}

func flattenJSON(s string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	flatten("", v, out)
	return out, nil
}

func flatten(prefix string, v interface{}, out map[string]string) {
	if m, ok := v.(map[string]interface{}); ok {
		for k, c := range m {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, c, out)
		}
		return
	}
	if s, ok := v.(string); ok {
		out[prefix] = s
		return
	}
	out[prefix] = fmt.Sprint(v)
}

func packagesOf(files []FileChange) []string {
	var dirs []string
	for _, f := range files {
		if strings.HasSuffix(f.Path, ".go") {
			dirs = append(dirs, path.Dir(f.Path))
		}
	}
	return uniqSorted(dirs)
}

func sortFiles(f []FileChange) {
	sort.Slice(f, func(i, j int) bool { return f[i].Path < f[j].Path })
}

func uniqSorted(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func uniqLocales(in []LocaleKey) []LocaleKey {
	seen := map[string]bool{}
	out := []LocaleKey{}
	for _, k := range in {
		if !seen[k.Key] {
			seen[k.Key] = true
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Merge combines task records into one Automaton record. Files are keyed by
// path (a later record's status wins), lists are unioned and sorted, commands
// are concatenated in order, and the AutomatonID is the first one set. The
// merged TaskID is empty; the timestamp is the latest of the inputs.
func Merge(records ...*ChangeRecord) *ChangeRecord {
	out := &ChangeRecord{}
	files := map[string]string{}
	var pk, st, rt, ts []string
	var lk []LocaleKey
	for _, r := range records {
		if r == nil {
			continue
		}
		if out.AutomatonID == "" {
			out.AutomatonID = r.AutomatonID
		}
		if r.Timestamp.After(out.Timestamp) {
			out.Timestamp = r.Timestamp
		}
		for _, f := range r.Files {
			files[f.Path] = f.Status
		}
		pk = append(pk, r.Packages...)
		st = append(st, r.SettingsAdded...)
		rt = append(rt, r.RoutesAdded...)
		ts = append(ts, r.TestsAdded...)
		lk = append(lk, r.LocaleKeysAdded...)
		out.Commands = append(out.Commands, r.Commands...)
		out.Cost = mergeCost(out.Cost, r.Cost)
	}
	for p, s := range files {
		out.Files = append(out.Files, FileChange{Path: p, Status: s})
	}
	sortFiles(out.Files)
	if out.Files == nil {
		out.Files = []FileChange{}
	}
	out.Packages, out.SettingsAdded, out.RoutesAdded, out.TestsAdded = uniqSorted(pk), uniqSorted(st), uniqSorted(rt), uniqSorted(ts)
	out.LocaleKeysAdded = uniqLocales(lk)
	if out.Commands == nil {
		out.Commands = []CommandRun{}
	}
	return out
}

// storeDir is the subdirectory of the Automaton data directory holding records.
const storeDir = "change-records"

func recordPath(dataDir, id string) (string, error) {
	if id == "" || id != filepath.Base(id) || id == "." || id == ".." {
		return "", fmt.Errorf("changerecord: invalid record id %q", id)
	}
	return filepath.Join(dataDir, storeDir, id+".json"), nil
}

// Save writes the record to <dataDir>/change-records/<id>.json atomically.
func Save(dataDir, id string, rec *ChangeRecord) error {
	p, err := recordPath(dataDir, id)
	if err != nil {
		return err
	}
	if rec == nil {
		return errors.New("changerecord: nil record")
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".record-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Load reads <dataDir>/change-records/<id>.json.
func Load(dataDir, id string) (*ChangeRecord, error) {
	p, err := recordPath(dataDir, id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p) // #nosec G304 -- id validated, path under dataDir
	if err != nil {
		return nil, err
	}
	var rec ChangeRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("changerecord: parse %s: %w", p, err)
	}
	return &rec, nil
}

// GitDiff is a DiffSource backed by the git binary, comparing the working
// tree in Dir against the base ref.
type GitDiff struct {
	Dir  string
	Base string
}

func (g GitDiff) git(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...) // #nosec G204 -- fixed git subcommands
	cmd.Dir = g.Dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// ChangedFiles lists files changed since the base ref.
func (g GitDiff) ChangedFiles() ([]FileChange, error) {
	out, err := g.git("diff", "--name-status", "--no-renames", g.Base)
	if err != nil {
		return nil, err
	}
	var files []FileChange
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(l, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		st := StatusModified
		switch parts[0] {
		case "A":
			st = StatusAdded
		case "D":
			st = StatusDeleted
		}
		files = append(files, FileChange{Path: parts[1], Status: st})
	}
	return files, nil
}

// FileDiff returns the unified diff and before/after contents of one file.
func (g GitDiff) FileDiff(p string) (FileDiffData, error) {
	u, err := g.git("diff", "--no-renames", g.Base, "--", p)
	if err != nil {
		return FileDiffData{}, err
	}
	d := FileDiffData{Unified: u}
	if before, err := g.git("show", g.Base+":"+p); err == nil {
		d.Before = before
	}
	if after, err := os.ReadFile(filepath.Join(g.Dir, p)); err == nil { // #nosec G304 -- path from git diff
		d.After = string(after)
	}
	return d, nil
}

// mergeCost sums two cost summaries so records from different Automatons keep
// all spend. Session ids are unioned; notes are joined without repeats.
func mergeCost(a, b *CostSummary) *CostSummary {
	if b == nil {
		return a
	}
	c := *b
	c.SessionIDs = append([]string(nil), b.SessionIDs...)
	if a == nil {
		return &c
	}
	o := *a
	o.SessionIDs = uniqSorted(append(append([]string(nil), a.SessionIDs...), b.SessionIDs...))
	if len(o.SessionIDs) > 0 && len(a.SessionIDs) == a.Sessions && len(b.SessionIDs) == b.Sessions {
		o.Sessions = len(o.SessionIDs)
	} else {
		o.Sessions += b.Sessions
	}
	o.TokensIn += b.TokensIn
	o.TokensOut += b.TokensOut
	o.CacheRead += b.CacheRead
	o.CacheCreation += b.CacheCreation
	o.ToolCalls += b.ToolCalls
	o.SpanSeconds += b.SpanSeconds
	switch {
	case a.APIEquivalentUSD != nil && b.APIEquivalentUSD != nil:
		v := *a.APIEquivalentUSD + *b.APIEquivalentUSD
		o.APIEquivalentUSD = &v
	case a.APIEquivalentUSD == nil && b.APIEquivalentUSD != nil:
		v := *b.APIEquivalentUSD
		o.APIEquivalentUSD, o.USDLabel = &v, b.USDLabel
	}
	o.WindowNote = joinNote(a.WindowNote, b.WindowNote)
	o.UnknownNote = joinNote(a.UnknownNote, b.UnknownNote)
	return &o
}

func joinNote(a, b string) string {
	if a == "" || a == b {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}
