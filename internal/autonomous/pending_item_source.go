// Pending item file registry — the optional file an operator can point
// pending items at. This file holds only pure parsing and a bounded file
// read; it never writes, never assumes a file name, a numbering sequence or
// any naming convention of this repository. The caller supplies the column
// mapping and the id pattern (both come from the autonomous.automation.
// pending_item_source.* settings).

package autonomous

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PendingItemSourceMaxBytes is the largest registry file that is read. It is a
// documented safety limit (a registry table is small; a huge file is almost
// certainly the wrong path), not a behaviour choice.
const PendingItemSourceMaxBytes = 2 << 20 // 2 MiB

// ErrPendingItemNoTable is returned by ParsePendingItemTable when the content
// holds no table whose header resolves both the id column and the title
// column.
var ErrPendingItemNoTable = errors.New("no usable table: no table header names the id and title columns")

// Errors returned by ReadPendingItemSourceFile.
var (
	// ErrPendingItemSourceEscapes: a relative path leaves the base directory,
	// by ".." or by a symbolic link.
	ErrPendingItemSourceEscapes = errors.New("registry path leaves the base directory")
	// ErrPendingItemSourceNotRegular: the target is not a regular file.
	ErrPendingItemSourceNotRegular = errors.New("registry path is not a regular file")
	// ErrPendingItemSourceTooLarge: the file is over PendingItemSourceMaxBytes.
	ErrPendingItemSourceTooLarge = errors.New("registry file is larger than the 2 MiB limit")
)

// PendingItemSourceRow is one parsed table row.
type PendingItemSourceRow struct {
	Ref   string // text of the id cell
	Title string // text of the title cell
	State string // text of the state cell; empty with no state column
	Done  bool   // the state cell equals one of the done values
	Line  int    // 1-based line number of the row in the content
}

// PendingItemSourceMapping says which table columns hold what.
type PendingItemSourceMapping struct {
	// IDColumn, TitleColumn and StateColumn are each a header name (matched
	// case-insensitively) or a 1-based column number. A header name wins over
	// a number when a header carries that exact text. An empty IDColumn means
	// "1" and an empty TitleColumn means "2" (the configured defaults); an
	// empty StateColumn means there is no state column.
	IDColumn    string
	TitleColumn string
	StateColumn string
	// IDPattern is required: only rows whose id cell matches it are read.
	IDPattern *regexp.Regexp
	// DoneValues are the state-cell texts (case-insensitive) that mean done.
	DoneValues []string
}

var (
	pendingItemSepCellRe = regexp.MustCompile(`^:?-+:?$`)
	pendingItemLinkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	pendingItemFenceRe   = regexp.MustCompile("^\\s{0,3}(```|~~~)")
)

// ParsePendingItemTable reads every GitHub-style pipe table in content whose
// header resolves the id and title columns of m. It returns the rows whose id
// cell matches m.IDPattern, and the number of rows skipped because the id did
// not match or the title was empty (a blank id is skipped without being
// counted). A nil IDPattern is an error. Content with no usable table returns
// zero rows and ErrPendingItemNoTable. Tables inside fenced code blocks are
// ignored.
func ParsePendingItemTable(content string, m PendingItemSourceMapping) (rows []PendingItemSourceRow, skipped int, err error) {
	if m.IDPattern == nil {
		return nil, 0, errors.New("id pattern is required")
	}
	idCol, titleCol := m.IDColumn, m.TitleColumn
	if strings.TrimSpace(idCol) == "" {
		idCol = "1"
	}
	if strings.TrimSpace(titleCol) == "" {
		titleCol = "2"
	}
	done := make(map[string]bool, len(m.DoneValues))
	for _, v := range m.DoneValues {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			done[v] = true
		}
	}

	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	foundTable := false
	fence := ""
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if f := pendingItemFenceRe.FindStringSubmatch(line); f != nil {
			switch fence {
			case "":
				fence = f[1]
			case f[1]:
				fence = ""
			}
			continue
		}
		if fence != "" || i+1 >= len(lines) || !strings.Contains(line, "|") {
			continue
		}
		header := splitPendingItemCells(line)
		if !isPendingItemSeparator(lines[i+1], len(header)) {
			continue
		}
		ic := resolvePendingItemColumn(header, idCol)
		tc := resolvePendingItemColumn(header, titleCol)
		sc := -1
		if strings.TrimSpace(m.StateColumn) != "" {
			sc = resolvePendingItemColumn(header, m.StateColumn)
		}
		// Walk the body whether or not this table is usable, so its rows are
		// not mistaken for a header of another table.
		j := i + 2
		for ; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || !strings.Contains(lines[j], "|") {
				break
			}
			if ic < 0 || tc < 0 {
				continue
			}
			cells := splitPendingItemCells(lines[j])
			ref := pendingItemCell(cells, ic)
			if ref == "" {
				continue
			}
			title := pendingItemCell(cells, tc)
			if !m.IDPattern.MatchString(ref) || title == "" {
				skipped++
				continue
			}
			row := PendingItemSourceRow{Ref: ref, Title: title, Line: j + 1}
			if sc >= 0 {
				row.State = pendingItemCell(cells, sc)
				row.Done = done[strings.ToLower(row.State)]
			}
			rows = append(rows, row)
		}
		if ic >= 0 && tc >= 0 {
			foundTable = true
		}
		i = j - 1
	}
	if !foundTable {
		return nil, 0, ErrPendingItemNoTable
	}
	return rows, skipped, nil
}

// splitPendingItemCells splits one table line into raw cells. A leading and a
// trailing pipe are optional; an escaped pipe (\|) stays inside its cell as a
// plain pipe.
func splitPendingItemCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	var (
		cells     []string
		cur       strings.Builder
		lastPipe  bool
		runes     = []rune(line)
		runeCount = len(runes)
	)
	for i := 0; i < runeCount; i++ {
		r := runes[i]
		lastPipe = false
		switch {
		case r == '\\' && i+1 < runeCount && runes[i+1] == '|':
			cur.WriteRune('|')
			i++
		case r == '|':
			cells = append(cells, cur.String())
			cur.Reset()
			lastPipe = true
		default:
			cur.WriteRune(r)
		}
	}
	if !lastPipe {
		cells = append(cells, cur.String())
	}
	return cells
}

// isPendingItemSeparator reports whether line is a table separator row such as
// |---|:--:| with exactly want cells.
func isPendingItemSeparator(line string, want int) bool {
	if !strings.Contains(line, "-") {
		return false
	}
	cells := splitPendingItemCells(line)
	if len(cells) != want {
		return false
	}
	for _, c := range cells {
		if !pendingItemSepCellRe.MatchString(strings.TrimSpace(c)) {
			return false
		}
	}
	return true
}

// resolvePendingItemColumn maps a column setting to a 0-based index in header,
// or -1 when it does not resolve. A header name (case-insensitive) is tried
// first, then a 1-based number.
func resolvePendingItemColumn(header []string, setting string) int {
	setting = strings.TrimSpace(setting)
	if setting == "" {
		return -1
	}
	for i, h := range header {
		if strings.EqualFold(cleanPendingItemCell(h), cleanPendingItemCell(setting)) {
			return i
		}
	}
	if n, err := strconv.Atoi(setting); err == nil && n >= 1 && n <= len(header) {
		return n - 1
	}
	return -1
}

// pendingItemCell returns the cleaned text of cell idx, or "" past the end.
func pendingItemCell(cells []string, idx int) string {
	if idx < 0 || idx >= len(cells) {
		return ""
	}
	return cleanPendingItemCell(cells[idx])
}

// cleanPendingItemCell trims a cell, reduces markdown links to their text and
// strips surrounding backticks and emphasis marks.
func cleanPendingItemCell(s string) string {
	s = strings.TrimSpace(s)
	s = pendingItemLinkRe.ReplaceAllString(s, "$1")
	for {
		t := strings.TrimSpace(strings.Trim(s, "`*_~"))
		if t == s {
			return s
		}
		s = t
	}
}

// ReadPendingItemSourceFile reads the registry file at path. An absolute path
// is used as given (the operator configured it). A relative path is resolved
// against baseDir, which is the daemon's default project directory
// (config Session.DefaultProjectDir); it may not escape baseDir by ".." or by
// a symbolic link. The target must be a regular file of at most
// PendingItemSourceMaxBytes. The file name is never assumed.
func ReadPendingItemSourceFile(baseDir, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("registry path is empty")
	}
	target := filepath.Clean(path)
	if !filepath.IsAbs(path) {
		if strings.TrimSpace(baseDir) == "" {
			return "", errors.New("registry path is relative and there is no base directory")
		}
		if target == ".." || strings.HasPrefix(target, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: %s", ErrPendingItemSourceEscapes, path)
		}
		realBase, err := filepath.EvalSymlinks(baseDir)
		if err != nil {
			return "", fmt.Errorf("resolve base directory: %w", err)
		}
		realTarget, err := filepath.EvalSymlinks(filepath.Join(realBase, target))
		if err != nil {
			return "", fmt.Errorf("resolve registry path: %w", err)
		}
		rel, err := filepath.Rel(realBase, realTarget)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("%w: %s", ErrPendingItemSourceEscapes, path)
		}
		target = realTarget
	}
	f, err := os.Open(target) // #nosec G304 -- path is operator configured and confined above when relative
	if err != nil {
		return "", fmt.Errorf("open registry file: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat registry file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrPendingItemSourceNotRegular, path)
	}
	if info.Size() > PendingItemSourceMaxBytes {
		return "", fmt.Errorf("%w: %s", ErrPendingItemSourceTooLarge, path)
	}
	// Read one byte past the limit so a file that grew after the stat is
	// still refused.
	data, err := io.ReadAll(io.LimitReader(f, PendingItemSourceMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read registry file: %w", err)
	}
	if len(data) > PendingItemSourceMaxBytes {
		return "", fmt.Errorf("%w: %s", ErrPendingItemSourceTooLarge, path)
	}
	return string(data), nil
}
