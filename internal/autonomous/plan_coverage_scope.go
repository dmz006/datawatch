// plan_coverage_scope.go — scoped requirement extraction for the plan
// coverage gate. Operator feature requests routinely flatten several
// labelled sections (scope, constraints, parity, tests, docs…) into one
// paragraph, and the unsuppressed gate then counts every loose path and
// config-key token from every section as a requirement of its own. This
// file cuts the extraction surface down to the section bodies the
// operator actually scoped: it locates the labelled sections in the
// spec and extracts requirements only from their text, with the
// standalone token pass turned off so a path or key counts only
// through the sentence that holds it.
package autonomous

import (
	"regexp"
	"strings"
)

// specHeadingRe recognises one inline heading inside a flattened spec
// paragraph. A heading is:
//
//   - at the start of the spec, at the start of a line, or right after
//     the terminal (. ! or ? plus whitespace) of the previous sentence;
//   - a short label of at most four words (letters, spaces and hyphens
//     only), with its first word captured as the section name source;
//   - optionally a parenthetical of any length holding no closing
//     parenthesis inside it;
//   - optionally a comma and a short qualifier (up to three words),
//     such as ", EXACTLY";
//   - a colon.
//
// Markdown "#" marks and "**" around the heading are noise the match
// ignores. A label without a colon (for instance a bare "## Scope"
// line) is not a heading: everything terminates on the colon.
var specHeadingRe = regexp.MustCompile(
	`(?im)` +
		`(?:^|[.!?][ \t]+)` + // start of spec/line, or after a sentence end
		`[*#]*[ \t]*` + // markdown decoration, ignored
		`([A-Za-z]+(?:-[A-Za-z]+)*)` + // label: first word (captured)
		`(?:[ \t]+[A-Za-z]+(?:-[A-Za-z]+)*){0,3}` + // label: up to 3 more words
		`(?:[ \t]*\([^)]*\))?` + // optional parenthetical
		`(?:[ \t]*,[ \t]+[A-Za-z]+(?:[ \t]+[A-Za-z]+){0,2})?` + // optional ", QUALIFIER"
		`[ \t]*:\**`) // the colon, ignoring a closing "**"

// scopedSpecText returns the bodies of every section named in sections
// whose heading appears in spec, joined with a single newline, plus
// found: true when at least one such section exists. A section's body
// runs from just after the heading colon to the next heading of ANY
// name, or to the end of the spec. Section names are compared
// lowercased. A spec with no recognised heading yields ("", false).
func scopedSpecText(spec string, sections []string) (text string, found bool) {
	want := make(map[string]bool, len(sections))
	for _, s := range sections {
		if name := strings.ToLower(strings.TrimSpace(s)); name != "" {
			want[name] = true
		}
	}

	type headingSpan struct {
		name   string
		start  int // where the full heading match begins (incl. preceding terminal)
		bodyAt int // just after the heading's colon
	}

	var heads []headingSpan
	for _, sub := range specHeadingRe.FindAllStringSubmatchIndex(spec, -1) {
		if sub[0] < 0 || sub[2] < 0 {
			continue
		}
		heads = append(heads, headingSpan{
			name:   strings.ToLower(spec[sub[2]:sub[3]]),
			start:  sub[0],
			bodyAt: sub[1],
		})
	}

	var parts []string
	for i, h := range heads {
		if !want[h.name] {
			continue
		}
		end := len(spec)
		if i+1 < len(heads) {
			end = heads[i+1].start
		}
		found = true
		parts = append(parts, strings.TrimSpace(spec[h.bodyAt:end]))
	}
	return strings.Join(parts, "\n"), found
}

// ExtractRequirementsScoped returns the requirements of the spec
// restricted to the named sections: scopedSpecText selects the
// section bodies, and the extraction runs on that text (or, when the
// spec has no recognised heading, on the whole spec) with the
// standalone token pass disabled — loose paths and config-key tokens
// never count by themselves here, only through the numbered item,
// obligation sentence or test sentence that holds them. Every
// requirement whose normalized text (see normalizeRequirement) is
// shorter than minChars characters is dropped; minChars of 0 sets no
// minimum.
func ExtractRequirementsScoped(spec string, sections []string, minChars int) []string {
	text := spec
	if scoped, found := scopedSpecText(spec, sections); found {
		text = scoped
	}
	var out []string
	for _, req := range extractRequirementsFrom(text, false) {
		if minChars > 0 {
			norm, _ := normalizeRequirement(req)
			if len(norm) < minChars {
				continue
			}
		}
		out = append(out, req)
	}
	return out
}
