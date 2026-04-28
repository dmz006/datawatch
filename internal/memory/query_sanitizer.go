// v5.26.70 — Mempalace QW#4 port: query_sanitizer.py.
//
// Strips prompt-injection patterns from search queries before they
// reach the embedder. Defense-in-depth — the embedder treats any
// input as opaque text, but a sanitized query reduces attack
// surface for downstream LLM consumers (memory_recall MCP tool,
// auto-loaded L1 facts) and improves semantic-search precision
// by filtering out boilerplate attacker phrasing.
//
// Operator-directed (mempalace audit, 2026-04-28). Pure Go port.

package memory

import (
	"regexp"
	"strings"
)

// Common prompt-injection patterns the sanitizer redacts. Each regex
// matches case-insensitively. Reviewed against the OWASP LLM Top 10
// (LLM01: Prompt Injection) reference patterns.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bignore\s+(previous|above|prior|all)\s+(instruction|directive|rule|prompt)s?\b`),
	regexp.MustCompile(`(?i)\bdisregard\s+(previous|above|prior|all)\s+(instruction|directive)s?\b`),
	regexp.MustCompile(`(?i)\bsystem\s*:\s*you\s+are\b`),
	regexp.MustCompile(`(?i)\b\[?(SYSTEM|ASSISTANT|USER)\]?\s*:\s*`),
	regexp.MustCompile(`(?i)\bnew\s+(instruction|directive|task|persona)s?\s*:`),
	regexp.MustCompile(`(?i)\boverride\s+(your|the)\s+(instruction|directive|prompt|persona|rule)s?\b`),
	regexp.MustCompile(`(?i)\bjailbreak\b`),
	regexp.MustCompile(`(?i)\bDAN\s+mode\b`),
	regexp.MustCompile(`(?i)\bact\s+as\s+(an?|the)\s+(unrestricted|jailbroken|uncensored)\b`),
	regexp.MustCompile(`(?i)\breveal\s+(your|the)\s+(system\s+prompt|instructions|directives)\b`),
}

// SanitizeQuery removes detected prompt-injection patterns from the
// query string. Returns the cleaned query + a count of redactions.
// When the input contains no patterns, returns the input unchanged
// + redactions=0 (zero allocations beyond the input).
func SanitizeQuery(q string) (string, int) {
	if q == "" {
		return q, 0
	}
	cleaned := q
	redactions := 0
	for _, re := range injectionPatterns {
		matches := re.FindAllStringIndex(cleaned, -1)
		if len(matches) == 0 {
			continue
		}
		redactions += len(matches)
		cleaned = re.ReplaceAllString(cleaned, "[redacted]")
	}
	// Collapse runs of [redacted] tokens that result from overlapping
	// patterns so the operator sees one marker instead of a chain.
	if redactions > 0 {
		cleaned = strings.ReplaceAll(cleaned, "[redacted] [redacted]", "[redacted]")
	}
	return cleaned, redactions
}
