// The exact form of StripDaemonSections, for decisions where being wrong lets
// a change through: the protected-paths check and the pre-merge cleanup.
//
// StripDaemonSections finds sections by their markers and headings alone, so
// whatever anyone puts there is removed with them. The functions here remove a
// block only when its lines are exactly a text the daemon writes. Everything
// else, including text between daemon markers, under a daemon heading or after
// a marker that is never closed, stays in the content and is compared like any
// other line.
package platformcontext

import "strings"

const (
	// guardrailsTail opens the fixed part of the Session Guardrails header:
	// the lines above it name the session and its task.
	guardrailsTail = "## Constraints"
	// guardrailsDirPrefix opens the one line below it that names the
	// session's directory.
	guardrailsDirPrefix = "- Work only within: "
)

// SameBesidesDaemonText reports whether cur equals base once the text the
// daemon is known to write is set aside on both sides (blank-line differences
// are ignored, as in StripDaemonSections). written lists those texts, each one
// a whole block; the platform context block is always among them.
//
// One more difference is the daemon's: it refreshes the Session Guardrails
// header in place. When cur carries a header from written, a header in base
// that differs from it only in the session, task and directory is set aside
// too. When this function cannot tell, it answers false.
func SameBesidesDaemonText(cur, base string, written []string) bool {
	curLines, headers := removeWrittenBlocks(strings.Split(cur, "\n"), written)
	baseLines, _ := removeWrittenBlocks(strings.Split(base, "\n"), written)
	for _, h := range headers {
		baseLines = removeStaleHeader(baseLines, h)
	}
	return normalizeBlankLines(joinLines(curLines)) == normalizeBlankLines(joinLines(baseLines))
}

// RemoveDaemonText returns content without the blocks that are exactly a text
// from written (or the platform context block), blank lines normalised as in
// StripDaemonSections. removed is false, and content is returned unchanged,
// when there is no such block.
func RemoveDaemonText(content string, written []string) (rest string, removed bool) {
	lines := strings.Split(content, "\n")
	kept, _ := removeWrittenBlocks(lines, written)
	if len(kept) == len(lines) {
		return content, false
	}
	return normalizeBlankLines(joinLines(kept)), true
}

// removeWrittenBlocks removes every run of whole lines equal to one of the
// written blocks. headers lists the Session Guardrails headers it removed.
func removeWrittenBlocks(lines, written []string) (kept []string, headers [][]string) {
	kept = lines
	all := append([]string{block(PlatformContext, PlatformContextVersion)}, written...)
	for _, w := range all {
		want := strings.Split(strings.Trim(w, "\n"), "\n")
		if strings.TrimSpace(want[0]) == "" {
			continue
		}
		found := false
		for i := 0; i+len(want) <= len(kept); {
			if !sameLines(kept[i:i+len(want)], want) {
				i++
				continue
			}
			kept = appendRange(kept, i, i+len(want))
			found = true
		}
		if found && trimCR(want[0]) == guardrailsHeader {
			headers = append(headers, want)
		}
	}
	return kept, headers
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if trimCR(a[i]) != trimCR(b[i]) {
			return false
		}
	}
	return true
}

// removeStaleHeader removes from lines each Session Guardrails section that is
// the same header as fresh written for another session. A section with any
// other difference, an added line included, stays.
func removeStaleHeader(lines, fresh []string) []string {
	for i := 0; i < len(lines); i++ {
		if trimCR(lines[i]) != guardrailsHeader {
			continue
		}
		end := i + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "# ") {
			end++
		}
		for end > i+1 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		if sameHeaderTemplate(lines[i:end], fresh) {
			return appendRange(lines, i, end)
		}
	}
	return lines
}

// sameHeaderTemplate reports whether old is the daemon's header as fresh is:
// the heading, a blank line and the session line, then the same fixed lines,
// where only the directory may differ.
func sameHeaderTemplate(old, fresh []string) bool {
	if len(old) < 3 || strings.TrimSpace(old[1]) != "" ||
		!strings.HasPrefix(old[2], "Session: ") || !strings.Contains(old[2], " | Task: ") {
		return false
	}
	tail := func(lines []string) []string {
		for i, l := range lines {
			if trimCR(l) == guardrailsTail {
				return lines[i:]
			}
		}
		return nil
	}
	a, b := tail(old), tail(fresh)
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if trimCR(a[i]) == trimCR(b[i]) {
			continue
		}
		if i != 1 || !strings.HasPrefix(a[i], guardrailsDirPrefix) || !strings.HasPrefix(b[i], guardrailsDirPrefix) {
			return false
		}
	}
	return true
}

// Block returns the marked platform context block holding body.
func Block(body string, version int) string { return block(body, version) }
