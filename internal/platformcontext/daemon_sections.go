package platformcontext

import "strings"

// marker pairs and header lines of the sections the datawatch daemon writes
// itself into a session's instructions file (CLAUDE.md or similar). Operators
// should not hand-edit these: the daemon rewrites them on every session start,
// so an operator editing the file in a way that overlaps them loses their
// change on the next write. StripDaemonSections is the inverse: it removes
// exactly those blocks, leaving everything the operator wrote untouched.
const (
	// rtk begin/end.
	rtkBegin = "<!-- rtk-instructions -->"
	rtkEnd   = "<!-- /rtk-instructions -->"

	// cross-session begin/end.
	crossBegin = "<!-- cross-session-communication-rule -->"
	crossEnd   = "<!-- /cross-session-communication-rule -->"

	// header lines that open a "# " section the daemon owns.
	memoryHeader     = "# Memory & Knowledge (datawatch)"
	guardrailsHeader = "# Session Guardrails"
)

// StripDaemonSections returns content with every section the daemon writes
// itself removed, touching nothing else. It removes:
//
//  1. the platform-context block between
//     `<!-- datawatch:platform-context v<N> -->` and
//     `<!-- /datawatch:platform-context -->`,
//  2. the RTK block from `<!-- rtk-instructions -->` through
//     `<!-- /rtk-instructions -->`,
//  3. the cross-session block from
//     `<!-- cross-session-communication-rule -->` through
//     `<!-- /cross-session-communication-rule -->`,
//  4. the section that starts at the line `# Memory & Knowledge (datawatch)`
//     and runs to the next line starting with `# ` (or end of file), and
//  5. the `# Session Guardrails` header block, which runs to the next line
//     starting with `# ` (the daemon rewrites it on every session start).
//
// The blank lines the removals leave behind are collapsed: trailing whitespace
// of the result is trimmed and runs of three or more newlines are reduced to
// two, so a file that contains one of these sections and the same file without
// it strip to identical text. The operation is idempotent: stripping an
// already-stripped file changes nothing.
func StripDaemonSections(content string) string {
	content = RemoveSection(content)
	content = removeMarkerBlock(content, rtkBegin, rtkEnd)
	content = removeMarkerBlock(content, crossBegin, crossEnd)
	content = removeHeaderSection(content, memoryHeader)
	content = removeHeaderSection(content, guardrailsHeader)
	return normalizeBlankLines(content)
}

// removeMarkerBlock deletes the block from the begin line through the end line
// (both inclusive). If the begin marker is never followed by the end marker,
// the rest of the file from the begin marker on is dropped. Content unchanged
// when the begin marker is absent.
func removeMarkerBlock(s, begin, end string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if trimCR(l) != begin {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if trimCR(lines[j]) == end {
				return joinLines(appendRange(lines, i, j+1))
			}
		}
		return joinLines(lines[:i])
	}
	return s
}

// removeHeaderSection deletes the section whose first line is header, running
// up to (not including) the next line that starts with `# ` (hash + space), or
// to end of file if there is none. Content unchanged when the header is absent.
func removeHeaderSection(s, header string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != header {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "# ") {
				end = j
				break
			}
		}
		return joinLines(appendRange(lines, i, end))
	}
	return s
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}

// appendRange returns a new slice containing lines[:from] and lines[to:],
// never reusing the source backing array so overlapping ranges are safe.
func appendRange(lines []string, from, to int) []string {
	out := make([]string, 0, len(lines)-(to-from))
	out = append(out, lines[:from]...)
	return append(out, lines[to:]...)
}

// normalizeBlankLines trims trailing whitespace on the whole result and
// collapses runs of three or more consecutive newlines down to two (i.e. at
// most one blank line between any two lines of text).
func normalizeBlankLines(s string) string {
	var b strings.Builder
	blankRun := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimRight(line, " \t\r") == "" {
			blankRun++
			if blankRun > 1 {
				continue
			}
		} else {
			blankRun = 0
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), " \t\r\n")
}

func trimCR(s string) string {
	return strings.TrimRight(s, "\r")
}
