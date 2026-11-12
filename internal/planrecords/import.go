package planrecords

// One-time, idempotent import of the plans archive into the store.
//
// The import reads the numbered rows of docs/plans/README.md and the findings
// files (docs/plans/**/*findings*.md by default) of a registered project and
// creates one record for each through the store. It only ever reads those
// files: they stay in place as the read-only archive. Nothing here opens a file
// for writing; every record goes through Store.Create or Store.Update, so the
// store's sealing and audit events apply.
//
// Idempotency. A row is identified by (project, kind, legacy_number), a
// finding by (project, kind, legacy_source). A second run creates nothing. When
// the source text of an already imported record changed, the run reports it as
// changed; the record is only brought up to date when nobody edited it since
// the import. Two links on each imported record carry what is needed to tell:
// import-hash (a hash of the source text) and import-fields (a hash of the
// title, body, state and shipped version as imported).
//
// Conflicts. Before anything is written the import builds the full preview:
// every title that is already taken inside the project, every duplicate row
// number in the README and every row that cannot be parsed. A real run applies
// the policy to every conflict, so it never stops half way: suffix (default)
// appends " (row <n>)" or " (<file>)" and flags the record needs_review,
// rename takes the new title from the caller's map, skip leaves the record out.
// A dry run returns the same preview and counts and writes nothing.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// Import defaults and names.
const (
	// DefaultImportProject is the project alias used when none is given.
	DefaultImportProject = "datawatch"
	// The import sources ImportOptions.Source accepts.
	ImportSourceRegistry = "registry"
	ImportSourceFindings = "findings"
	ImportSourceAll      = "all"
	// DefaultFindingsGlob selects the findings files, relative to the project.
	DefaultFindingsGlob = "docs/plans/**/*findings*.md"
	// RegistryREADME is the registry file, relative to the project.
	RegistryREADME = "docs/plans/README.md"
	// AuditImported is the summary audit action of a real import run.
	AuditImported = "plan_record_imported"

	linkImportHash   = "import-hash"
	linkImportFields = "import-fields"
	linkDoc          = "doc"
	maxImportFile    = 8 << 20
	titleCutRunes    = 120
)

// ErrUnknownProject is returned for a project alias that is not registered.
type ErrUnknownProject struct{ Alias string }

func (e *ErrUnknownProject) Error() string {
	return fmt.Sprintf("planrecords: unknown project alias %q (register it first)", e.Alias)
}

// Importer reads the archive of a registered project into a Store.
type Importer struct {
	Store *Store
	// ProjectDir returns the directory of a registered project alias.
	ProjectDir func(alias string) (dir string, ok bool)
	// Roots returns the allowed project roots (server.allowed_project_roots
	// plus the built-in ones). An empty list refuses every directory.
	Roots func() []string
}

// ImportOptions are the settings of one import run.
type ImportOptions struct {
	// Project is the registered project alias; empty means "datawatch".
	Project string
	// Source limits the run to "registry" (the README rows), "findings" (the
	// findings files) or "all"; empty means "all".
	Source string
	// FindingsGlob selects findings files relative to the project; empty means
	// DefaultFindingsGlob. "**" matches any number of directories.
	FindingsGlob string
	// Policy resolves conflicts; empty uses the store's default (suffix).
	Policy ConflictPolicy
	// Renames supplies the new title of a conflicting record for the rename
	// policy, keyed by "row <n>" (rows), the legacy_source (findings) or the
	// original title.
	Renames map[string]string
	// DryRun returns the preview and the counts and writes nothing.
	DryRun bool
	// Actor names who ran the import for the audit events.
	Actor string
}

// ImportCounts are the totals of a run. A dry run and a real run over the same
// state report the same numbers.
type ImportCounts struct {
	// Create is the number of records a dry run would create and a real run
	// created (conflicts resolved by suffix or rename included).
	Create    int `json:"create"`
	Unchanged int `json:"unchanged"`
	// Changed counts imported records whose source text changed since the import.
	Changed int `json:"changed"`
	// Conflicts counts every conflict found, however it was resolved.
	Conflicts int `json:"conflicts"`
	// Skipped counts source items left out: skipped conflicts, unparseable rows
	// and renames that were not supplied.
	Skipped int `json:"skipped"`
}

// ImportConflict is one conflict and what the policy did (or would do) to it.
type ImportConflict struct {
	// Source is the legacy source of the item ("docs/plans/README.md row 12").
	Source string `json:"source"`
	Title  string `json:"title"`
	// Reason is "title" or "duplicate_row_number".
	Reason string `json:"reason"`
	// With names the stored record (id) or earlier source item in the way.
	With string `json:"with"`
	// Action is "suffix", "rename" or "skip".
	Action string `json:"action"`
	// NewTitle is the title the record gets; empty when skipped.
	NewTitle string `json:"new_title,omitempty"`
	// Note explains a skip that the policy did not ask for.
	Note string `json:"note,omitempty"`
}

// ImportIssue is an item that could not be turned into a record.
type ImportIssue struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// ImportChange is an imported record whose source text changed.
type ImportChange struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Source string `json:"source"`
	// Edited is true when the record was edited since the import, so it was
	// not overwritten.
	Edited bool `json:"edited"`
	// Updated is true when a real run brought the record up to date.
	Updated bool `json:"updated"`
}

// ImportFlagged is a record created with needs_review set.
type ImportFlagged struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// ImportResult is the full outcome of a run.
type ImportResult struct {
	Project     string           `json:"project"`
	DryRun      bool             `json:"dry_run"`
	Policy      ConflictPolicy   `json:"policy"`
	Counts      ImportCounts     `json:"counts"`
	Conflicts   []ImportConflict `json:"conflicts"`
	Unparseable []ImportIssue    `json:"unparseable"`
	Changed     []ImportChange   `json:"changed"`
	Flagged     []ImportFlagged  `json:"flagged"`
	// Failed lists records the store refused after the preview (it never
	// stops the run).
	Failed []ImportIssue `json:"failed"`
	// Files lists what was read.
	Files []string `json:"files"`
}

// cand is one proposed record with the identity and hash the import needs.
type cand struct {
	rec     Record
	srcHash string
	short   string // file base name for the findings suffix
	dupRow  bool   // a later README row that repeats an earlier row number
	num     int    // README row number, also for duplicates
}

// Import runs the import.
func (im *Importer) Import(opts ImportOptions) (*ImportResult, error) {
	if im == nil || im.Store == nil {
		return nil, errors.New("planrecords: importer has no store")
	}
	project := strings.TrimSpace(opts.Project)
	if project == "" {
		project = DefaultImportProject
	}
	policy, err := ParsePolicy(string(opts.Policy))
	if err != nil {
		return nil, err
	}
	if opts.Policy == "" {
		policy = im.Store.opts.ImportConflictPolicy
	}
	root, err := im.readRoot(project)
	if err != nil {
		return nil, err
	}
	glob := opts.FindingsGlob
	if strings.TrimSpace(glob) == "" {
		glob = DefaultFindingsGlob
	}
	res := &ImportResult{Project: project, DryRun: opts.DryRun, Policy: policy,
		Conflicts: []ImportConflict{}, Unparseable: []ImportIssue{}, Changed: []ImportChange{},
		Flagged: []ImportFlagged{}, Failed: []ImportIssue{}, Files: []string{}}

	var cands []cand
	switch opts.Source {
	case "", ImportSourceAll, ImportSourceRegistry, ImportSourceFindings:
	default:
		return nil, fmt.Errorf("planrecords: source %q is not one of registry, findings or all", opts.Source)
	}
	wantRegistry := opts.Source != ImportSourceFindings
	wantFindings := opts.Source != ImportSourceRegistry
	var readme string
	if wantRegistry {
		readme, err = readArchiveFile(root, RegistryREADME)
	} else {
		err = fs.ErrNotExist
	}
	switch {
	case err == nil:
		res.Files = append(res.Files, RegistryREADME)
		rows, issues := parseRegistryTables(project, readme)
		cands = append(cands, rows...)
		res.Unparseable = append(res.Unparseable, issues...)
	case errors.Is(err, fs.ErrNotExist):
		// A project without a registry README imports findings only.
	default:
		return nil, fmt.Errorf("planrecords: read %s: %w", RegistryREADME, err)
	}
	var files []string
	if wantFindings {
		files, err = matchFindingsFiles(root, glob)
		if err != nil {
			return nil, err
		}
	}
	for _, rel := range files {
		text, err := readArchiveFile(root, rel)
		if err != nil {
			res.Unparseable = append(res.Unparseable, ImportIssue{Source: rel, Reason: "cannot read: " + err.Error()})
			continue
		}
		res.Files = append(res.Files, rel)
		fc, issues := parseFindingsFile(project, rel, text)
		cands = append(cands, fc...)
		res.Unparseable = append(res.Unparseable, issues...)
	}

	plan := im.plan(project, policy, opts, cands, res)
	if !opts.DryRun {
		im.apply(opts, plan, res)
	}
	return res, nil
}

// readRoot resolves the alias to a directory that passes the roots guard.
func (im *Importer) readRoot(alias string) (string, error) {
	if im.ProjectDir == nil {
		return "", &ErrUnknownProject{Alias: alias}
	}
	dir, ok := im.ProjectDir(alias)
	if !ok || strings.TrimSpace(dir) == "" {
		return "", &ErrUnknownProject{Alias: alias}
	}
	var roots []string
	if im.Roots != nil {
		roots = im.Roots()
	}
	clean, err := gitguard.ResolveContainedProjectDir(dir, roots)
	if err != nil {
		return "", fmt.Errorf("planrecords: project %q: %w", alias, err)
	}
	return clean, nil
}

// readArchiveFile reads one regular file under root. It never follows a
// symlink out of the root and never opens anything for writing.
func readArchiveFile(root, rel string) (string, error) {
	p, err := gitguard.ContainedPath(root, filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", rel)
	}
	if fi.Size() > maxImportFile {
		return "", fmt.Errorf("%s is larger than %d bytes", rel, maxImportFile)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---- findings file selection ----

// matchFindingsFiles returns the project-relative paths (slash form, sorted)
// of regular files that match glob.
func matchFindingsFiles(root, glob string) ([]string, error) {
	g := path.Clean(filepath.ToSlash(strings.TrimSpace(glob)))
	if g == "." || strings.HasPrefix(g, "/") || g == ".." || strings.HasPrefix(g, "../") || strings.Contains(g, "/../") {
		return nil, fmt.Errorf("planrecords: findings glob %q must be a relative path inside the project", glob)
	}
	segs := strings.Split(g, "/")
	for _, sg := range segs {
		if sg == "**" {
			continue
		}
		if _, err := path.Match(sg, ""); err != nil {
			return nil, fmt.Errorf("planrecords: findings glob %q: %w", glob, err)
		}
	}
	// Walk only from the literal directory prefix of the glob.
	var prefix []string
	for _, sg := range segs[:len(segs)-1] {
		if strings.ContainsAny(sg, "*?[") {
			break
		}
		prefix = append(prefix, sg)
	}
	start := filepath.Join(root, filepath.FromSlash(strings.Join(prefix, "/")))
	if _, err := gitguard.ContainedPath(root, start); err != nil {
		return nil, err
	}
	var out []string
	err := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if globMatch(segs, strings.Split(rel, "/")) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("planrecords: list findings files: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// globMatch matches path segments against glob segments; "**" matches zero or
// more whole segments.
func globMatch(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if globMatch(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return globMatch(pat[1:], name[1:])
}

// ---- registry README ----

var (
	mdLinkRe     = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]*)\)`)
	docLinkRe    = regexp.MustCompile(`\]\(([^)#\s]+\.md)(?:#[^)]*)?\)`)
	leadingIDRe  = regexp.MustCompile(`^(?:[A-Z]{1,3}\d+)(?:[:.)]|\s)\s*`)
	bareIDRe     = regexp.MustCompile(`^[A-Z]{1,3}\d+$`)
	sepCellRe    = regexp.MustCompile(`^:?-{2,}:?$`)
	versionRe    = regexp.MustCompile(`(?i)\bshipped(?:\s+in)?\s+v?\d+(?:\.\d+)+`)
	sentenceEndR = regexp.MustCompile(`[.!?](?:\s|$)`)
	statusWords  = []struct {
		word  string
		state State
	}{
		{"in progress", StateInProgress}, {"in_progress", StateInProgress}, {"partial", StateInProgress},
		{"in review", StateOpen}, {"open", StateOpen},
		{"planned", StatePlanned},
		{"done", StateDone}, {"completed", StateDone}, {"shipped", StateShipped},
		{"blocked", StateBlocked},
		{"paused", StateDeferred}, {"frozen", StateDeferred}, {"deferred", StateDeferred},
		{"cancelled", StateCancelled}, {"canceled", StateCancelled},
	}
)

// splitRow splits a markdown table line into trimmed cells (outer pipes
// removed). Escaped pipes (\|) stay inside a cell.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if line[i] == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(line[i])
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func isSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !sepCellRe.MatchString(c) {
			return false
		}
	}
	return true
}

// tableHeader is the column map of one table, read from its own header row.
type tableHeader struct {
	names  []string
	num    int // column of the row number, -1 when none
	item   int
	status int
}

func newHeader(cells []string) tableHeader {
	h := tableHeader{names: cells, num: -1, item: -1, status: -1}
	for i, c := range cells {
		n := strings.ToLower(strings.Trim(strings.TrimSpace(c), "*`_ "))
		switch {
		case h.num < 0 && (n == "#" || n == "no" || n == "no." || n == "number" || n == "row"):
			h.num = i
		case h.item < 0 && (n == "item" || n == "title" || n == "name" || n == "what"):
			h.item = i
		case h.status < 0 && (n == "status" || n == "state"):
			h.status = i
		}
	}
	return h
}

// parseRegistryTables reads every numbered row of every table in the README.
// Columns come from each table's own header row, so tables with different
// layouts both work. A table without a number column is not part of the
// numbered registry unless its first cell is a number.
func parseRegistryTables(project, text string) ([]cand, []ImportIssue) {
	var out []cand
	var issues []ImportIssue
	lines := strings.Split(text, "\n")
	var hdr *tableHeader
	seen := map[int]string{} // row number -> first source
	repeats := map[int]int{} // row number -> how many later rows repeat it
	inFence := false
	lastRow, lastRaw := -1, ""
	var lastHdr *tableHeader
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		t := strings.TrimSpace(line)
		if inFence || !strings.HasPrefix(t, "|") {
			switch {
			case t == "" || strings.HasPrefix(t, "#"):
				// A blank line or a heading ends the table.
				hdr, lastRow = nil, -1
			case lastRow >= 0:
				// A wrapped row: the line continues the previous row.
				out[lastRow].rec.Body += "\n" + t
				lastRaw += "\n" + t
				out[lastRow].srcHash = hashText(lastRaw)
			}
			continue
		}
		lastRow = -1
		cells := splitRow(t)
		if i+1 < len(lines) && isSeparator(splitRow(lines[i+1])) && !isSeparator(cells) {
			h := newHeader(cells)
			hdr = &h
			if h.num >= 0 && h.item >= 0 {
				lastHdr = &h
			}
			i++ // skip the separator
			continue
		}
		if isSeparator(cells) {
			continue
		}
		first := strings.Trim(cells[0], "*` ")
		n, convErr := strconv.Atoi(first)
		if convErr != nil || n <= 0 {
			continue // not a numbered row (B# rows, label tables, ...)
		}
		src := fmt.Sprintf("%s row %d", RegistryREADME, n)
		cur, orphan := hdr, false
		if cur == nil {
			// A row added below the table it belongs to (after a blank line or
			// other text): take the columns from the nearest earlier header.
			if lastHdr == nil {
				issues = append(issues, ImportIssue{Source: src, Reason: "numbered row before any table header row"})
				continue
			}
			cur, orphan = lastHdr, true
		}
		item := cur.item
		if item < 0 {
			item = 0
			for item == cur.num || item == cur.status {
				item++
			}
		}
		numCol := cur.num
		if numCol < 0 {
			numCol = 0
		}
		if numCol != 0 {
			// The number is not in the first column: read it from its column.
			if numCol < len(cells) {
				if v, err := strconv.Atoi(strings.Trim(cells[numCol], "*` ")); err == nil && v > 0 {
					n = v
					src = fmt.Sprintf("%s row %d", RegistryREADME, n)
				}
			}
		}
		if item >= len(cells) {
			issues = append(issues, ImportIssue{Source: src, Reason: "row has fewer cells than the table header"})
			continue
		}
		itemCell := cells[item]
		title, rest := splitTitle(itemCell)
		if NormTitle(title) == "" {
			issues = append(issues, ImportIssue{Source: src, Reason: "the Item cell has no title text"})
			continue
		}
		statusCell := ""
		if cur.status >= 0 && cur.status < len(cells) {
			statusCell = cells[cur.status]
		}
		state, known := parseState(statusCell)
		var b strings.Builder
		if rest != "" {
			b.WriteString(rest + "\n\n")
		}
		for ci, c := range cells {
			if ci == item || ci == numCol || strings.TrimSpace(c) == "" || strings.TrimSpace(c) == "-" || ci >= len(cur.names) {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", strings.Trim(cur.names[ci], "*` "), c)
		}
		fmt.Fprintf(&b, "\nOriginal item cell: %s\n", itemCell)
		rec := Record{
			Kind: KindRegistryRow, Project: project, State: state, Title: title,
			Body: strings.TrimSpace(b.String()), LegacyNumber: n, LegacySource: src,
			ShippedVersion: versionRe.FindString(statusCell),
		}
		if m := docLinkRe.FindStringSubmatch(itemCell); m != nil && !strings.Contains(m[1], "://") {
			rec.Links = append(rec.Links, Link{Kind: linkDoc, Ref: path.Join("docs/plans", m[1])})
		}
		if orphan {
			rec.NeedsReview = true
			rec.ReviewReason = "row found outside its table: columns taken from the nearest earlier table header"
		}
		if !known && strings.TrimSpace(statusCell) != "" {
			rec.NeedsReview = true
			rec.ReviewReason = joinReason(rec.ReviewReason, fmt.Sprintf("unrecognised status text %q: state set to open", clip(statusCell, 80)))
		}
		c := cand{rec: rec, srcHash: hashText(t), num: n}
		if first, dup := seen[n]; dup {
			c.dupRow = true
			c.rec.LegacyNumber = 0
			repeats[n]++
			c.rec.LegacySource = fmt.Sprintf("%s (repeat %d, first at %s)", src, repeats[n], first)
		} else {
			seen[n] = src
		}
		out = append(out, c)
		lastRow, lastRaw = len(out)-1, t
	}
	return out, issues
}

// splitTitle cleans an Item cell and splits it into the title and the rest.
func splitTitle(cell string) (title, rest string) {
	s := mdLinkRe.ReplaceAllString(cell, "$1")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(leadingIDRe.ReplaceAllString(s, ""))
	if bareIDRe.MatchString(s) {
		return "", ""
	}
	cut := len(s)
	if loc := sentenceEndR.FindStringIndex(s); loc != nil {
		cut = loc[0] + 1
		if loc[1] > len(s) {
			cut = len(s)
		}
	}
	if utf8Len(s[:cut]) > titleCutRunes {
		cut = cutAtRunes(s, titleCutRunes)
		if sp := strings.LastIndex(s[:cut], " "); sp > cut/2 {
			cut = sp
		}
	}
	title = strings.TrimRight(strings.TrimSpace(s[:cut]), " ,;:.-")
	rest = strings.TrimSpace(s[cut:])
	return title, rest
}

func utf8Len(s string) int { return len([]rune(s)) }

func cutAtRunes(s string, n int) int {
	i := 0
	for pos := range s {
		if i == n {
			return pos
		}
		i++
	}
	return len(s)
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8Len(s) <= n {
		return s
	}
	return s[:cutAtRunes(s, n)] + "..."
}

// parseState maps the status cell to a registry-row state. Only the leading
// phrase counts (up to the first bracket, colon, semicolon or comma), and the
// earliest status word in it wins. known is false when no word matched.
func parseState(cell string) (State, bool) {
	s := strings.ToLower(cell)
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	if i := strings.IndexAny(s, "(:;,"); i >= 0 {
		s = s[:i]
	}
	best, bestPos := StateOpen, -1
	for _, w := range statusWords {
		pos := wordIndex(s, w.word)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			best, bestPos = w.state, pos
		}
	}
	return best, bestPos >= 0
}

// wordIndex finds word in s on word boundaries.
func wordIndex(s, word string) int {
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], word)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(word)
		before := i == 0 || !isWordByte(s[i-1])
		after := end >= len(s) || !isWordByte(s[end])
		if before && after {
			return i
		}
		from = i + 1
	}
	return -1
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// ---- findings files ----

var (
	h2Re       = regexp.MustCompile(`^##\s+(.+?)\s*#*\s*$`)
	anyHeadRe  = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
	dateLineRe = regexp.MustCompile(`(?mi)^[\s>*_-]*date:[*_\s]*(\d{4}-\d{2}-\d{2})`)
	namedDate  = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})`)
	slugStrip  = regexp.MustCompile(`[^a-z0-9 _-]+`)
)

func headingSlug(h string) string {
	h = strings.NewReplacer("`", "", "*", "").Replace(strings.ToLower(h))
	h = slugStrip.ReplaceAllString(h, "")
	return strings.ReplaceAll(strings.TrimSpace(h), " ", "-")
}

// parseFindingsFile makes one finding per level-2 heading section, or one for
// the whole file when it has none.
func parseFindingsFile(project, rel, text string) ([]cand, []ImportIssue) {
	base := path.Base(rel)
	date := ""
	if m := dateLineRe.FindStringSubmatch(text); m != nil {
		date = m[1]
	} else if m := namedDate.FindStringSubmatch(base); m != nil {
		date = m[1]
	}
	lines := strings.Split(text, "\n")
	type section struct {
		title string
		start int
		end   int
	}
	var secs []section
	inFence := false
	firstHead := ""
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if firstHead == "" {
			if m := anyHeadRe.FindStringSubmatch(ln); m != nil {
				firstHead = cleanHeading(m[1])
			}
		}
		if m := h2Re.FindStringSubmatch(ln); m != nil && !strings.HasPrefix(ln, "###") {
			if len(secs) > 0 {
				secs[len(secs)-1].end = i
			}
			secs = append(secs, section{title: cleanHeading(m[1]), start: i, end: len(lines)})
		}
	}
	var out []cand
	var issues []ImportIssue
	mk := func(title, slug, body string) {
		src := rel
		if slug != "" {
			src = rel + "#" + slug
		}
		body = strings.TrimSpace(body)
		if date != "" && !strings.Contains(body, date) {
			body = "Date: " + date + "\n\n" + body
		}
		rec := Record{Kind: KindFinding, Project: project, State: StateOpen, Title: title, Body: body, LegacySource: src}
		if NormTitle(title) == "" {
			issues = append(issues, ImportIssue{Source: src, Reason: "no heading text to use as a title"})
			return
		}
		if utf8Len(title) > MaxTitleRunes {
			title = clip(title, titleCutRunes)
			rec.Title = title
		}
		if len(body) > MaxBodyBytes {
			issues = append(issues, ImportIssue{Source: src, Reason: fmt.Sprintf("section larger than %d bytes", MaxBodyBytes)})
			return
		}
		if utf8Len(src) > MaxSourceRunes {
			issues = append(issues, ImportIssue{Source: src, Reason: "source path too long"})
			return
		}
		out = append(out, cand{rec: rec, srcHash: hashText(body), short: base})
	}
	if len(secs) == 0 {
		title := firstHead
		if title == "" {
			title = strings.TrimSuffix(base, path.Ext(base))
		}
		mk(title, "", text)
		return out, issues
	}
	used := map[string]int{}
	for _, sc := range secs {
		slug := headingSlug(sc.title)
		if n := used[slug]; n > 0 {
			used[slug] = n + 1
			slug = fmt.Sprintf("%s-%d", slug, n)
		} else {
			used[slug] = 1
		}
		mk(sc.title, slug, strings.Join(lines[sc.start:sc.end], "\n"))
	}
	return out, issues
}

func cleanHeading(h string) string {
	h = mdLinkRe.ReplaceAllString(h, "$1")
	h = strings.NewReplacer("**", "", "__", "", "`", "").Replace(h)
	return strings.Join(strings.Fields(h), " ")
}

// ---- planning ----

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// fieldsHash covers what an operator can edit: title, body, state and the
// shipped version text.
func fieldsHash(title, body string, state State, version string) string {
	return hashText(strings.Join([]string{NormTitle(title), body, string(state), version}, "\x00"))
}

func linkRef(ls []Link, kind string) string {
	for _, l := range ls {
		if l.Kind == kind {
			return l.Ref
		}
	}
	return ""
}

// planItem is what a real run does for one candidate.
type planItem struct {
	c      cand
	rec    Record // the record to create, resolution applied
	update string // uid to update when the source changed and nobody edited it
}

// existingKeyIndex finds already imported records by their identity.
func (s *Store) existingKeyIndex(project string) (byNum map[string]Record, bySrc map[string]Record) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byNum = map[string]Record{}
	bySrc = map[string]Record{}
	for _, r := range s.byUID {
		if r.Project != project {
			continue
		}
		if r.Kind == KindRegistryRow && r.LegacyNumber > 0 {
			byNum[strconv.Itoa(r.LegacyNumber)] = r.clone()
		}
		if r.LegacySource != "" {
			bySrc[string(r.Kind)+"\x00"+r.LegacySource] = r.clone()
		}
	}
	return byNum, bySrc
}

// plan classifies every candidate, builds the conflict preview and fills the
// result counts. It writes nothing.
func (im *Importer) plan(project string, policy ConflictPolicy, opts ImportOptions, cands []cand, res *ImportResult) []planItem {
	s := im.Store
	byNum, bySrc := s.existingKeyIndex(project)
	claimed := map[string]string{} // titleKey -> description of who holds it
	s.mu.RLock()
	taken := func(title string) (string, bool) {
		k := titleKey(project, title)
		if uid, ok := s.byTitle[k]; ok {
			return "stored record " + s.byUID[uid].ID, true
		}
		if who, ok := claimed[k]; ok {
			return who, true
		}
		return "", false
	}
	defer s.mu.RUnlock()

	var items []planItem
	for _, c := range cands {
		// Already imported?
		var ex Record
		var found bool
		if c.rec.LegacyNumber > 0 {
			ex, found = byNum[strconv.Itoa(c.rec.LegacyNumber)]
		}
		if !found {
			ex, found = bySrc[string(c.rec.Kind)+"\x00"+c.rec.LegacySource]
		}
		if found {
			if linkRef(ex.Links, linkImportHash) == c.srcHash {
				res.Counts.Unchanged++
				continue
			}
			edited := linkRef(ex.Links, linkImportFields) != fieldsHash(ex.Title, ex.Body, ex.State, ex.ShippedVersion)
			res.Counts.Changed++
			ch := ImportChange{ID: ex.ID, Title: ex.Title, Source: c.rec.LegacySource, Edited: edited}
			res.Changed = append(res.Changed, ch)
			if !edited {
				items = append(items, planItem{c: c, update: ex.UID})
			}
			continue
		}

		rec := c.rec.clone()
		var cf *ImportConflict
		if c.dupRow {
			cf = &ImportConflict{Source: rec.LegacySource, Title: rec.Title, Reason: "duplicate_row_number",
				With: fmt.Sprintf("%s row %d", RegistryREADME, c.num)}
		} else if who, ok := taken(rec.Title); ok {
			cf = &ImportConflict{Source: rec.LegacySource, Title: rec.Title, Reason: ConflictTitle, With: who}
		}
		if cf != nil {
			res.Counts.Conflicts++
			action := string(policy)
			newTitle := ""
			switch policy {
			case PolicySkip:
			case PolicyRename:
				key := fmt.Sprintf("row %d", c.num)
				if c.rec.Kind == KindFinding {
					key = c.rec.LegacySource
				}
				nt := strings.TrimSpace(opts.Renames[key])
				if nt == "" {
					nt = strings.TrimSpace(opts.Renames[c.rec.Title])
				}
				ok := nt != ""
				if _, clash := taken(nt); ok && clash {
					ok = false
					cf.Note = fmt.Sprintf("the supplied title %q is already taken", nt)
				}
				if ok {
					newTitle = nt
				} else {
					action = string(PolicySkip)
					if cf.Note == "" {
						cf.Note = "rename policy: no new title supplied for this record, so it was skipped"
					}
				}
			default:
				newTitle = suffixTitle(c, taken)
			}
			cf.Action, cf.NewTitle = action, newTitle
			if newTitle == "" {
				res.Counts.Skipped++
				res.Conflicts = append(res.Conflicts, *cf)
				continue
			}
			rec.Title = newTitle
			if policy != PolicyRename || c.dupRow {
				rec.NeedsReview = true
				reason := fmt.Sprintf("title conflict with %s: imported as %q", cf.With, newTitle)
				if c.dupRow {
					reason = fmt.Sprintf("row number %d appears more than once in the README: imported as %q", c.num, newTitle)
				}
				rec.ReviewReason = joinReason(rec.ReviewReason, reason)
			}
			res.Conflicts = append(res.Conflicts, *cf)
		}
		claimed[titleKey(project, rec.Title)] = "earlier import item " + rec.LegacySource
		res.Counts.Create++
		if rec.NeedsReview {
			res.Flagged = append(res.Flagged, ImportFlagged{Title: rec.Title, Source: rec.LegacySource, Reason: rec.ReviewReason})
		}
		items = append(items, planItem{c: c, rec: rec})
	}
	// Unparseable items are skipped but never silent.
	res.Counts.Skipped += len(res.Unparseable)
	return items
}

func joinReason(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// suffixTitle builds " (row <n>)" for rows and " (<file>)" for findings, and
// adds a counter when even that is taken.
func suffixTitle(c cand, taken func(string) (string, bool)) string {
	base := strings.TrimSpace(c.rec.Title)
	var suffix string
	switch {
	case c.rec.Kind == KindRegistryRow && c.dupRow:
		suffix = fmt.Sprintf("row %d, repeated", c.num)
	case c.rec.Kind == KindRegistryRow:
		suffix = fmt.Sprintf("row %d", c.num)
	default:
		suffix = c.short
	}
	fit := func(sfx string) string {
		room := MaxTitleRunes - utf8Len(sfx) - 3
		b := base
		if utf8Len(b) > room {
			b = strings.TrimSpace(b[:cutAtRunes(b, room)])
		}
		return fmt.Sprintf("%s (%s)", b, sfx)
	}
	cand := fit(suffix)
	for n := 2; ; n++ {
		if _, ok := taken(cand); !ok {
			return cand
		}
		cand = fit(fmt.Sprintf("%s %d", suffix, n))
	}
}

// ---- apply ----

func (im *Importer) apply(opts ImportOptions, items []planItem, res *ImportResult) {
	s := im.Store
	actor := opts.Actor
	if actor == "" {
		actor = "import"
	}
	created := 0
	flaggedAt := map[string]int{}
	for i, f := range res.Flagged {
		flaggedAt[f.Source] = i
	}
	for _, it := range items {
		if it.update != "" {
			im.applyUpdate(it, actor, res)
			continue
		}
		rec := it.rec.clone()
		rec.Actor = actor
		rec.Links = append(rec.Links,
			Link{Kind: linkImportHash, Ref: it.c.srcHash},
			Link{Kind: linkImportFields, Ref: fieldsHash(rec.Title, rec.Body, rec.State, rec.ShippedVersion)})
		out, err := s.Create(rec)
		var tc *ErrTitleConflict
		if errors.As(err, &tc) {
			// A record appeared after the preview was built: resolve it the
			// same way instead of failing the run.
			rec.Title = lateTitle(rec.Title, it.c.rec.LegacySource)
			rec.NeedsReview = true
			rec.ReviewReason = joinReason(rec.ReviewReason, "title taken while the import ran: imported as "+strconv.Quote(rec.Title))
			rec.Links[len(rec.Links)-1].Ref = fieldsHash(rec.Title, rec.Body, rec.State, rec.ShippedVersion)
			out, err = s.Create(rec)
		}
		if err != nil {
			res.Failed = append(res.Failed, ImportIssue{Source: rec.LegacySource, Reason: err.Error()})
			res.Counts.Create--
			res.Counts.Skipped++
			continue
		}
		created++
		if i, ok := flaggedAt[out.LegacySource]; ok {
			res.Flagged[i].ID, res.Flagged[i].Title = out.ID, out.Title
		}
	}
	if s.opts.Audit != nil {
		s.opts.Audit(AuditImported, res.Project, map[string]any{
			"project": res.Project, "actor": actor, "policy": string(res.Policy),
			"created": created, "unchanged": res.Counts.Unchanged, "changed": res.Counts.Changed,
			"conflicts": res.Counts.Conflicts, "skipped": res.Counts.Skipped,
		})
	}
}

func lateTitle(title, src string) string {
	sum := hashText(src)
	return fmt.Sprintf("%s (%s)", clip(title, MaxTitleRunes-12), sum[:6])
}

// applyUpdate brings an unedited imported record up to date with its source.
// The title is left alone: it may carry a conflict suffix, and a rename is the
// operator's decision.
func (im *Importer) applyUpdate(it planItem, actor string, res *ImportResult) {
	body, state, ver := it.c.rec.Body, it.c.rec.State, it.c.rec.ShippedVersion
	cur, err := im.Store.Get(it.update)
	if err != nil {
		res.Failed = append(res.Failed, ImportIssue{Source: it.c.rec.LegacySource, Reason: err.Error()})
		return
	}
	links := make([]Link, 0, len(it.c.rec.Links)+2)
	for _, l := range cur.Links {
		if l.Kind != linkImportHash && l.Kind != linkImportFields && l.Kind != linkDoc {
			links = append(links, l)
		}
	}
	links = append(links, it.c.rec.Links...)
	links = append(links,
		Link{Kind: linkImportHash, Ref: it.c.srcHash},
		Link{Kind: linkImportFields, Ref: fieldsHash(cur.Title, body, state, ver)})
	_, err = im.Store.Update(it.update, Patch{Body: &body, State: &state, ShippedVersion: &ver, Links: &links, Actor: actor})
	if err != nil {
		res.Failed = append(res.Failed, ImportIssue{Source: it.c.rec.LegacySource, Reason: err.Error()})
		return
	}
	for i := range res.Changed {
		if res.Changed[i].ID == cur.ID {
			res.Changed[i].Updated = true
		}
	}
}
