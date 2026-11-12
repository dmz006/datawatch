package mergepipeline

import (
	"errors"
	"fmt"
	"strings"
)

// Conflict markers as git writes them with merge.conflictStyle=diff3, which
// the conflict handling always asks for so each hunk carries its common base.
const (
	markerOurs   = "<<<<<<<"
	markerBase   = "|||||||"
	markerSplit  = "======="
	markerTheirs = ">>>>>>>"
)

// Hunk is one conflicting region of a file: the text of the common base and
// of each side. Lines keep their trailing newline.
type Hunk struct {
	File   string
	Index  int // 1-based, in the text the hunk was read from
	Line   int // 1-based line of the opening marker in that text
	Ours   []string
	Base   []string
	Theirs []string
}

// ResolvedHunk records one hunk the mechanical resolver settled. Index and
// Line refer to the conflicted file as git wrote it.
type ResolvedHunk struct {
	File        string `json:"file"`
	Index       int    `json:"index"`
	Line        int    `json:"line"`
	OursLines   int    `json:"ours_lines"`
	TheirsLines int    `json:"theirs_lines"`
	KeptLines   int    `json:"kept_lines"`
	Method      string `json:"method"`
}

// MethodAdditive is the Method of a hunk both sides only added to.
const MethodAdditive = "additive"

// ErrNoConflictMarkers means the text holds no diff3 conflict hunk, as for a
// binary file or a delete-versus-modify conflict.
var ErrNoConflictMarkers = errors.New("no diff3 conflict markers in the file")

// segment is a run of plain lines or one hunk.
type segment struct {
	plain []string
	hunk  *Hunk
}

// linesOf splits text into lines that keep their newline, so joining them
// returns the text unchanged: every element ends in "\n" except
// possibly the last when the text has no final newline.
func linesOf(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func hasMarker(line, marker string) bool {
	return strings.HasPrefix(line, marker) && (len(line) == len(marker) || line[len(marker)] == ' ' || line[len(marker)] == '\n' || line[len(marker)] == '\r')
}

// parseConflicts splits text into plain runs and hunks. A hunk without a base
// section (not diff3 style) or with unbalanced markers is an error: nothing
// about it can be judged additive.
func parseConflicts(file, text string) ([]segment, error) {
	lines := linesOf(text)
	var segs []segment
	var plain []string
	flush := func() {
		if len(plain) > 0 {
			segs = append(segs, segment{plain: plain})
			plain = nil
		}
	}
	idx := 0
	for i := 0; i < len(lines); i++ {
		if !hasMarker(lines[i], markerOurs) {
			plain = append(plain, lines[i])
			continue
		}
		flush()
		idx++
		h := &Hunk{File: file, Index: idx, Line: i + 1}
		state := 0 // 0 ours, 1 base, 2 theirs
		closed := false
		for i++; i < len(lines); i++ {
			l := lines[i]
			switch {
			case state == 0 && hasMarker(l, markerBase):
				state = 1
			case state == 0 && hasMarker(l, markerSplit):
				return nil, fmt.Errorf("%s: hunk %d has no base section (not diff3 style)", file, idx)
			case state == 1 && hasMarker(l, markerSplit):
				state = 2
			case state == 2 && hasMarker(l, markerTheirs):
				closed = true
			default:
				switch state {
				case 0:
					h.Ours = append(h.Ours, l)
				case 1:
					h.Base = append(h.Base, l)
				default:
					h.Theirs = append(h.Theirs, l)
				}
			}
			if closed {
				break
			}
		}
		if !closed {
			return nil, fmt.Errorf("%s: hunk %d is not closed", file, idx)
		}
		segs = append(segs, segment{hunk: h})
	}
	flush()
	if idx == 0 {
		return nil, ErrNoConflictMarkers
	}
	return segs, nil
}

// ParseHunks returns the conflict hunks of text in order.
func ParseHunks(file, text string) ([]Hunk, error) {
	segs, err := parseConflicts(file, text)
	if err != nil {
		return nil, err
	}
	var out []Hunk
	for _, s := range segs {
		if s.hunk != nil {
			out = append(out, *s.hunk)
		}
	}
	return out, nil
}

// gapsAround matches base into side as a subsequence and returns the lines
// side added in each gap: gaps[k] sits before base[k], and the last gap after
// the final base line. It reports false when a base line is missing from
// side, which means that side changed or removed it.
func gapsAround(base, side []string) ([][]string, bool) {
	gaps := make([][]string, len(base)+1)
	pos := 0
	for k, b := range base {
		j := pos
		for j < len(side) && side[j] != b {
			j++
		}
		if j == len(side) {
			return nil, false
		}
		gaps[k] = side[pos:j]
		pos = j + 1
	}
	gaps[len(base)] = side[pos:]
	return gaps, true
}

// additiveMerge keeps both sides of a hunk in which neither changed nor
// removed a line of the common base. Each side's additions stay at the place
// they were made; where both added at the same place, ours come first and then
// theirs, and an identical addition is kept once. ok is false for any hunk
// that is not purely additive.
func additiveMerge(base, ours, theirs []string) (merged []string, ok bool) {
	og, okO := gapsAround(base, ours)
	tg, okT := gapsAround(base, theirs)
	if !okO || !okT {
		return nil, false
	}
	for k := 0; k <= len(base); k++ {
		merged = append(merged, og[k]...)
		if !equalLines(og[k], tg[k]) {
			merged = append(merged, tg[k]...)
		}
		if k < len(base) {
			merged = append(merged, base[k])
		}
	}
	return merged, true
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ensureNewline gives a final line without one a newline, so a following line
// never joins it.
func ensureNewline(lines []string) []string {
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		out := append([]string(nil), lines...)
		out[n-1] += "\n"
		return out
	}
	return lines
}

// ResolveAdditive settles every purely additive hunk of a conflicted file. It
// returns the file text with those hunks replaced by both sides, the hunks it
// settled, and the hunks it left alone with their markers still in the text.
// The remaining hunks are numbered by their place in the returned text, which
// is the numbering a later proposal applies against. The result depends only
// on text, so the same conflict always resolves the same way.
func ResolveAdditive(file, text string) (newText string, resolved []ResolvedHunk, remaining []Hunk, err error) {
	segs, err := parseConflicts(file, text)
	if err != nil {
		return text, nil, nil, err
	}
	var b strings.Builder
	next := 0
	for _, s := range segs {
		if s.hunk == nil {
			for _, l := range s.plain {
				b.WriteString(l)
			}
			continue
		}
		h := s.hunk
		merged, ok := additiveMerge(h.Base, ensureNewline(h.Ours), ensureNewline(h.Theirs))
		if ok {
			for _, l := range merged {
				b.WriteString(l)
			}
			resolved = append(resolved, ResolvedHunk{File: file, Index: h.Index, Line: h.Line,
				OursLines: len(h.Ours), TheirsLines: len(h.Theirs), KeptLines: len(merged), Method: MethodAdditive})
			continue
		}
		next++
		b.WriteString(renderHunk(h))
	}
	newText = b.String()
	if len(resolved) > 0 || next > 0 {
		hs, perr := ParseHunks(file, newText)
		if perr != nil && !errors.Is(perr, ErrNoConflictMarkers) {
			return text, nil, nil, perr
		}
		remaining = hs
	}
	return newText, resolved, remaining, nil
}

// renderHunk writes a hunk back out in diff3 form.
func renderHunk(h *Hunk) string {
	var b strings.Builder
	b.WriteString(markerOurs + " ours\n")
	for _, l := range ensureNewline(h.Ours) {
		b.WriteString(l)
	}
	b.WriteString(markerBase + " base\n")
	for _, l := range ensureNewline(h.Base) {
		b.WriteString(l)
	}
	b.WriteString(markerSplit + "\n")
	for _, l := range ensureNewline(h.Theirs) {
		b.WriteString(l)
	}
	b.WriteString(markerTheirs + " theirs\n")
	return b.String()
}

// containsMarker reports whether any line of text is an opening, base or
// closing conflict marker. The bare separator is not counted: it is an ordinary
// line in some formats.
func containsMarker(text string) bool {
	for _, l := range linesOf(text) {
		if hasMarker(l, markerOurs) || hasMarker(l, markerBase) || hasMarker(l, markerTheirs) {
			return true
		}
	}
	return false
}
