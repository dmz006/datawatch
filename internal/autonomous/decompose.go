// BL24 — LLM-driven PRD → stories+tasks decomposition.
//
// The LLM is asked to return JSON shaped like:
//   {
//     "title": "...",
//     "stories": [
//       {
//         "title": "...",
//         "description": "...",
//         "depends_on": ["story-id-or-title"],
//         "tasks": [
//           {"title": "...", "spec": "...", "depends_on": ["task-id-or-title"]}
//         ]
//       }
//     ]
//   }
//
// We tolerate fenced code blocks, smart quotes, and trailing single-
// line comments — common LLM JSON-output pathologies that nightwire
// also handles in prd_builder.py.

package autonomous

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// DecompositionPrompt is the system+user prompt prefix the LLM sees.
// Operators can override via autonomous.decomposition_prompt config.
const DecompositionPrompt = `You are decomposing a feature request into a structured Product Requirements Document with stories and tasks.

Return ONLY a JSON object with this shape:
{
  "title": "<short feature title>",
  "stories": [
    {
      "title": "<story title>",
      "description": "<one-paragraph description>",
      "depends_on": [],
      "tasks": [
        {
          "title": "<task title>",
          "spec": "<concrete instructions for one worker session>",
          "depends_on": []
        }
      ]
    }
  ]
}

Rules:
- depends_on lists either story or task titles that must complete first.
- Each task's spec must be self-contained — the worker won't see other tasks.
- Aim for 1-3 stories per PRD, 1-5 tasks per story. Prefer fewer, well-scoped tasks.
- Do not include markdown fences. Return raw JSON only.

Feature request:
%s
`

// DecomposeRequest captures the LLM call inputs.
type DecomposeRequest struct {
	Spec     string
	Backend  string // empty = caller default
	Effort   Effort
}

// DecomposeFn is the indirection that lets tests inject a fake LLM
// while production wires through to /api/ask or a session.Manager.Start.
type DecomposeFn func(req DecomposeRequest) (jsonResponse string, err error)

// ParseDecomposition cleans common LLM JSON pathologies (fences,
// smart quotes, // comments) and unmarshals into a Story slice.
func ParseDecomposition(raw string) (title string, stories []Story, err error) {
	cleaned := cleanLLMJSON(raw)
	var doc struct {
		Title   string  `json:"title"`
		Stories []Story `json:"stories"`
	}
	if err := json.Unmarshal([]byte(cleaned), &doc); err != nil {
		return "", nil, fmt.Errorf("parse: %w (cleaned=%q)", err, truncate(cleaned, 200))
	}
	return doc.Title, doc.Stories, nil
}

// cleanLLMJSON strips fences, replaces smart quotes, removes // comments.
// Ported from nightwire/prd_builder.py clean_json_string.
func cleanLLMJSON(s string) string {
	s = strings.TrimSpace(s)
	// Remove surrounding ``` json fences.
	s = regexp.MustCompile("^```(?:json)?\\s*").ReplaceAllString(s, "")
	s = regexp.MustCompile("\\s*```$").ReplaceAllString(s, "")
	// Smart quotes → straight quotes.
	s = strings.NewReplacer(
		"\u201c", "\"", "\u201d", "\"",
		"\u2018", "'", "\u2019", "'",
	).Replace(s)
	// Strip line-comments outside strings.
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		out.WriteString(stripLineComment(line))
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}

// stripLineComment removes `// foo` from a line, respecting in-string
// occurrences of `//` (e.g. URLs).
func stripLineComment(line string) string {
	inStr := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if ch == '\\' && inStr && i+1 < len(line) {
			i++ // skip next
			continue
		}
		if ch == '"' {
			inStr = !inStr
			continue
		}
		if !inStr && ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
