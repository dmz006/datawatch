// Package docsindex (BL274 Sprint 1, v6.16.0) — section-level chunker
// for the markdown corpus that backs the Docs-as-MCP-Interface search
// surface.
//
// Design captured in docs/plans/2026-05-07-bl274-docs-as-mcp-plan.md.
//
// A "chunk" is one heading-bounded section of a doc:
//   - Skips YAML frontmatter (between leading `---` lines).
//   - Splits on `^## ` and `^### ` headings (h2 + h3).
//   - Strips the BL279 `<!-- BL279 see-also footer -->` marker so the
//     footer's links go into a separate `SeeAlso` field rather than
//     polluting the chunk body.
//   - Preserves the doc title (h1 or filename) on every chunk for
//     ranking + display.
//
// Section-level (not whole-doc, not per-paragraph) is the natural
// answer-shape: enough context to be useful, small enough to fit one
// MCP response.

package docsindex

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Chunk is one indexable unit.
type Chunk struct {
	// Path is the corpus-relative path (e.g. "howto/secrets-manager.md").
	Path string `json:"path"`

	// Anchor is the section anchor slug (e.g. "rotating-a-secret"). Empty
	// when the chunk is the doc preamble before the first ## heading.
	Anchor string `json:"anchor"`

	// Title is the doc-level title (h1 or filename) for display; not the
	// section heading itself, which is in Heading.
	Title string `json:"title"`

	// Heading is the chunk's own ## / ### heading text. Empty for the
	// pre-first-heading preamble chunk.
	Heading string `json:"heading"`

	// Body is the markdown body of this chunk (heading + content),
	// ready to display in search-result excerpts.
	Body string `json:"body"`

	// SeeAlso is the parsed BL279 see-also footer (relative paths). May
	// be empty.
	SeeAlso []string `json:"see_also,omitempty"`

	// Source is the trust tier this chunk belongs to: "core" | "skill:<n>"
	// | "plugin:<n>". Set by the caller during indexing, not by the
	// chunker.
	Source string `json:"source"`

	// ContentHash is the SHA-256 of Body, used for change-detection on
	// re-index (only chunks whose hash changed get re-embedded).
	ContentHash string `json:"content_hash"`
}

// ChunkID returns a stable identifier for the chunk, used as the key
// in the index.
func (c Chunk) ChunkID() string {
	return c.Source + ":" + c.Path + "#" + c.Anchor
}

var (
	frontmatterRE = regexp.MustCompile(`(?ms)\A---\n.*?\n---\n`)
	h1RE          = regexp.MustCompile(`(?m)^# (.+)$`)
	h2H3RE        = regexp.MustCompile(`(?m)^(##+) (.+)$`)
	bl279FooterRE = regexp.MustCompile(`(?ms)\n---\n\n<!-- BL279 see-also footer -->\n## See also\n\n(.*?)\z`)
	seeAlsoLinkRE = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
	slugRE        = regexp.MustCompile(`[^a-z0-9]+`)
)

// ChunkDoc splits a single markdown doc into chunks. `path` is the
// corpus-relative path, used for default-title fallback when no h1 is
// present.
func ChunkDoc(path, body string) []Chunk {
	// Strip YAML frontmatter — exec_steps live there but they're parsed
	// separately by the frontmatter package; the chunker doesn't need
	// them in the body.
	body = frontmatterRE.ReplaceAllString(body, "")

	// Pull the BL279 see-also footer out before chunking so the last
	// chunk doesn't carry the footer markup as if it were content.
	var seeAlso []string
	if m := bl279FooterRE.FindStringSubmatch(body); m != nil {
		for _, link := range seeAlsoLinkRE.FindAllStringSubmatch(m[1], -1) {
			if len(link) > 1 {
				seeAlso = append(seeAlso, link[1])
			}
		}
		body = bl279FooterRE.ReplaceAllString(body, "")
	}

	// Doc-level title: first h1, falling back to filename.
	var title string
	if m := h1RE.FindStringSubmatch(body); m != nil {
		title = strings.TrimSpace(m[1])
	} else {
		title = strings.TrimSuffix(strings.ToLower(path[strings.LastIndex(path, "/")+1:]), ".md")
	}

	// Find every h2/h3 heading position. Anything before the first one
	// is the preamble chunk.
	heads := h2H3RE.FindAllStringSubmatchIndex(body, -1)

	var out []Chunk
	emit := func(anchor, heading, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		hash := sha256.Sum256([]byte(content))
		c := Chunk{
			Path:        path,
			Anchor:      anchor,
			Title:       title,
			Heading:     heading,
			Body:        content,
			SeeAlso:     seeAlso, // every chunk in the doc shares the footer for navigation
			ContentHash: hex.EncodeToString(hash[:]),
		}
		out = append(out, c)
	}

	if len(heads) == 0 {
		emit("", "", body)
		return out
	}

	// Preamble (h1 + intro text before the first ## / ###).
	if heads[0][0] > 0 {
		emit("", "", body[:heads[0][0]])
	}

	for i, h := range heads {
		headingText := strings.TrimSpace(body[h[4]:h[5]]) // capture group 2 (heading text)
		anchor := slugify(headingText)
		var section string
		if i+1 < len(heads) {
			section = body[h[0]:heads[i+1][0]]
		} else {
			section = body[h[0]:]
		}
		emit(anchor, headingText, section)
	}
	return out
}

// slugify mirrors the diagrams.html viewer's anchor algorithm: lowercase,
// non-alnum runs collapse to '-', trim leading/trailing dashes.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
