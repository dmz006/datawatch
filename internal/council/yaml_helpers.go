// BL297 (v6.22.3) — YAML helpers for the persona-wizard drafter.

package council

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParsePersonaYAML decodes a YAML document into a Persona, tolerating
// extra fields (the LLM may emit `tags` which the existing Persona
// struct doesn't carry — those are returned via ExtractPersonaYAMLAndTags).
func ParsePersonaYAML(yamlSrc string) (Persona, error) {
	var p Persona
	if err := yaml.Unmarshal([]byte(yamlSrc), &p); err != nil {
		return Persona{}, err
	}
	return p, nil
}

// ExtractPersonaYAMLAndTags strips any code-fence wrapping the LLM may
// have added around its YAML output, then splits the YAML body from the
// `tags:` field (since the existing Persona struct doesn't carry tags).
//
// Returns (yamlBody_without_tags, csv_of_tags). The yaml body is the
// final form to pass to ParsePersonaYAML and AddPersona.
func ExtractPersonaYAMLAndTags(raw string) (string, string) {
	s := strings.TrimSpace(raw)
	// Strip common code-fence wrappers.
	for _, prefix := range []string{"```yaml", "```YAML", "```"} {
		s = strings.TrimPrefix(s, prefix)
	}
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	// Walk lines. Capture tags: line; rest is the body.
	lines := strings.Split(s, "\n")
	var bodyLines []string
	var tagsCSV string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "tags:") {
			tagsCSV = parseTagsLine(trim)
			continue
		}
		bodyLines = append(bodyLines, line)
	}
	body := strings.TrimSpace(strings.Join(bodyLines, "\n"))
	return body, tagsCSV
}

// parseTagsLine handles both flow-style `tags: [a, b, c]` and a
// best-effort plain CSV after the colon.
func parseTagsLine(line string) string {
	// "tags: [a, b, c]"  or  "tags: a, b, c"
	rest := strings.TrimPrefix(line, "tags:")
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "[")
	rest = strings.TrimSuffix(rest, "]")
	parts := strings.Split(rest, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"'`)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}
