// Rules digest — a short, deterministic summary of a project's rules
// file, cached by the file's content hash. Built for planning prompts of
// backends that cannot read files themselves (headless/ask-based, no
// tool access): instead of inlining the whole rules file (large — this
// repo's AGENT.md is ~2300 lines) or pointing at a path the backend
// cannot follow, the planner receives every heading plus only the
// lines that carry a mandatory marker. Session-based backends keep
// using the pointer text in rules_context.go; this is the fallback.
package autonomous

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// rulesDigestLineCap — kept lines longer than this are cut to this many
// bytes with an ellipsis, so one verbose rule line cannot blow the cap.
const rulesDigestLineCap = 240

// rulesDigestPreambleHeading — the synthetic section that carries
// mandatory lines appearing before the first markdown heading. Not
// counted as a section in the digest header.
const rulesDigestPreambleHeading = "(preamble)"

// rulesMandatoryRe — a line is "mandatory" when it contains one of the
// marker words as a whole word (case-insensitive) or the phrase "no
// exceptions". "is required" and "operator-required" both match via the
// required word.
var rulesMandatoryRe = regexp.MustCompile(`(?i)\b(must|mandatory|required|never|always)\b|\bno\s+exceptions\b`)

// rulesDigestSection is one heading and the mandatory lines kept under
// it, in file order.
type rulesDigestSection struct {
	heading string
	lines   []string
}

// clipLine cuts s to at most limit bytes ending in an ellipsis, never
// splitting a UTF-8 rune. Short lines pass through unchanged.
func clipLine(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	if cut <= 0 {
		return "..."
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// parseRulesDigest walks a rules file line by line: it keeps every
// markdown heading line (in order, including headings with no kept
// lines under them) and, under each heading, only the lines carrying a
// mandatory marker. Fenced code blocks are skipped entirely, including
// any heading- or marker-like lines inside them. Mandatory lines before
// the first heading land under a synthetic preamble section.
func parseRulesDigest(content []byte) []rulesDigestSection {
	var sections []rulesDigestSection
	fence := ""
	for _, raw := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(raw)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			fence = "```"
			continue
		}
		if strings.HasPrefix(trimmed, "~~~") {
			fence = "~~~"
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			sections = append(sections, rulesDigestSection{
				heading: strings.TrimSpace(strings.TrimLeft(trimmed, "#")),
			})
			continue
		}
		if trimmed == "" || !rulesMandatoryRe.MatchString(trimmed) {
			continue
		}
		if len(sections) == 0 {
			sections = append(sections, rulesDigestSection{heading: rulesDigestPreambleHeading})
		}
		last := &sections[len(sections)-1]
		last.lines = append(last.lines, clipLine(trimmed, rulesDigestLineCap))
	}
	return sections
}

func rulesDigestSectionCount(sections []rulesDigestSection) int {
	n := 0
	for _, s := range sections {
		if s.heading != rulesDigestPreambleHeading {
			n++
		}
	}
	return n
}

func rulesDigestMandatoryCount(sections []rulesDigestSection) int {
	n := 0
	for _, s := range sections {
		n += len(s.lines)
	}
	return n
}

// renderRulesDigestLines emits the digest as ordered lines: the header,
// then each section heading and its kept lines (two-space indent).
func renderRulesDigestLines(sections []rulesDigestSection) []string {
	header := fmt.Sprintf("Rules digest (generated, %d sections, %d mandatory lines):",
		rulesDigestSectionCount(sections), rulesDigestMandatoryCount(sections))
	ls := []string{header}
	for _, s := range sections {
		ls = append(ls, s.heading)
		for _, l := range s.lines {
			ls = append(ls, "  "+l)
		}
	}
	return ls
}

// renderRulesDigest deterministically fits the digest into maxBytes.
// First it drops mandatory lines from the end of the digest (every
// heading is kept while any fit); if even the bare heading list
// overflows, it cuts at a line boundary and appends the truncation
// note. The result always honors maxBytes or is empty.
func renderRulesDigest(sections []rulesDigestSection, maxBytes int) string {
	if len(sections) == 0 || maxBytes <= 0 {
		return ""
	}
	if joined := strings.Join(renderRulesDigestLines(sections), "\n"); len(joined) <= maxBytes {
		return joined
	}
	// Drop mandatory lines from the end until the digest fits.
	for {
		dropped := false
		for i := len(sections) - 1; i >= 0 && !dropped; i-- {
			if len(sections[i].lines) > 0 {
				sections[i].lines = sections[i].lines[:len(sections[i].lines)-1]
				dropped = true
			}
		}
		if !dropped {
			break
		}
		if joined := strings.Join(renderRulesDigestLines(sections), "\n"); len(joined) <= maxBytes {
			return joined
		}
	}
	// Headings alone overflow: cut at a line boundary, append the note.
	marker := fmt.Sprintf("[digest truncated at %d bytes]", maxBytes)
	budget := maxBytes - len(marker) - 1 // -1 for the separating newline
	ls := renderRulesDigestLines(sections)
	kept := 0
	used := 0
	for i, l := range ls {
		size := len(l)
		if i > 0 {
			size++
		}
		if used+size > budget {
			break
		}
		used += size
		kept = i + 1
	}
	if kept == 0 {
		if len(marker) <= maxBytes {
			return marker
		}
		return ""
	}
	return strings.Join(ls[:kept], "\n") + "\n" + marker
}

// BuildRulesDigest summarizes a rules file deterministically: every
// markdown heading, then under each heading the lines that carry a
// mandatory marker (see rulesMandatoryRe), in file order. Long lines
// are cut to rulesDigestLineCap bytes with an ellipsis; fenced code
// blocks are skipped. Result is maxBytes or less (see
// renderRulesDigest); maxBytes <= 0 yields "".
func BuildRulesDigest(content []byte, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	return renderRulesDigest(parseRulesDigest(content), maxBytes)
}

// RulesDigestSections returns the heading texts of a rules file,
// without the '#' marks, in file order (fences excluded) — the
// vocabulary for the rules checklist. Nil for a file with no headings.
func RulesDigestSections(content []byte) []string {
	var out []string
	for _, s := range parseRulesDigest(content) {
		if s.heading == rulesDigestPreambleHeading {
			continue
		}
		out = append(out, s.heading)
	}
	return out
}

// rulesDigestCache — package-level digest cache keyed by
// sha256(content) plus maxBytes, bounded to rulesDigestCacheEntries
// entries (oldest key dropped first), mutex-protected. rulesDigestBuilds
// counts builder runs so tests can prove a cache hit.
const rulesDigestCacheEntries = 64

var (
	rulesDigestCacheMu    sync.Mutex
	rulesDigestCache      = make(map[string]string)
	rulesDigestCacheOrder = make([]string, 0, rulesDigestCacheEntries)
	rulesDigestBuilds     int
)

func rulesDigestCacheGet(key string) (string, bool) {
	rulesDigestCacheMu.Lock()
	defer rulesDigestCacheMu.Unlock()
	v, ok := rulesDigestCache[key]
	return v, ok
}

func rulesDigestCachePut(key, val string) {
	rulesDigestCacheMu.Lock()
	if _, ok := rulesDigestCache[key]; !ok {
		rulesDigestCacheOrder = append(rulesDigestCacheOrder, key)
		if len(rulesDigestCacheOrder) > rulesDigestCacheEntries {
			old := rulesDigestCacheOrder[0]
			rulesDigestCacheOrder = rulesDigestCacheOrder[1:]
			delete(rulesDigestCache, old)
		}
	}
	rulesDigestCache[key] = val
	rulesDigestCacheMu.Unlock()
}

func rulesDigestBuildsNow() int {
	rulesDigestCacheMu.Lock()
	defer rulesDigestCacheMu.Unlock()
	return rulesDigestBuilds
}

func rulesDigestBuildsReset() {
	rulesDigestCacheMu.Lock()
	rulesDigestBuilds = 0
	rulesDigestCache = make(map[string]string)
	rulesDigestCacheOrder = rulesDigestCacheOrder[:0]
	rulesDigestCacheMu.Unlock()
}

// rulesDigestFor returns the digest of projectDir/fileName, cached by
// the file's content hash: an unchanged file is served from the cache
// without rebuilding. A file name that escapes projectDir after
// filepath.Join/Clean is rejected. size is the content byte length.
func (m *Manager) rulesDigestFor(projectDir, fileName string, maxBytes int) (digest string, sha string, size int, err error) {
	dir := filepath.Clean(projectDir)
	path := filepath.Join(dir, fileName)
	if path != dir && !strings.HasPrefix(path, dir+string(filepath.Separator)) {
		return "", "", 0, fmt.Errorf("rules digest: file name %q escapes project directory %q", fileName, projectDir)
	}
	content, err := os.ReadFile(path) // #nosec G304 -- path is confined to an operator-owned project directory by the escape check above
	if err != nil {
		return "", "", 0, err
	}
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])
	key := hexSum + ":" + strconv.Itoa(maxBytes)
	if d, ok := rulesDigestCacheGet(key); ok {
		return d, hexSum, len(content), nil
	}
	d := BuildRulesDigest(content, maxBytes)
	rulesDigestCachePut(key, d)
	rulesDigestCacheMu.Lock()
	rulesDigestBuilds++
	rulesDigestCacheMu.Unlock()
	return d, hexSum, len(content), nil
}
