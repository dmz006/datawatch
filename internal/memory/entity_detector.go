package memory

import (
	"regexp"
	"strings"
)

// DetectedEntity represents an automatically extracted entity.
type DetectedEntity struct {
	Name string `json:"name"`
	Type string `json:"type"` // "person", "project", "tool", "concept"
}

// DetectEntities extracts likely entity names from text using heuristics.
// This is a lightweight local detector — no LLM calls required.
func DetectEntities(text string) []DetectedEntity {
	var entities []DetectedEntity
	seen := make(map[string]bool)

	// Detect capitalized multi-word names (likely people or projects)
	nameRe := regexp.MustCompile(`\b([A-Z][a-z]+(?:\s+[A-Z][a-z]+)+)\b`)
	for _, match := range nameRe.FindAllString(text, -1) {
		lower := strings.ToLower(match)
		if !seen[lower] && !isCommonPhrase(lower) {
			seen[lower] = true
			entities = append(entities, DetectedEntity{Name: match, Type: "person"})
		}
	}

	// Detect tool/technology names (common patterns)
	toolRe := regexp.MustCompile(`\b(Go|Rust|Python|Node|React|Docker|Kubernetes|PostgreSQL|Redis|Ollama|Claude|OpenAI|GitHub|Slack|Signal|Telegram)\b`)
	for _, match := range toolRe.FindAllString(text, -1) {
		lower := strings.ToLower(match)
		if !seen[lower] {
			seen[lower] = true
			entities = append(entities, DetectedEntity{Name: match, Type: "tool"})
		}
	}

	// Detect project paths (likely project names)
	pathRe := regexp.MustCompile(`/([a-zA-Z][\w-]+)/?(?:\s|$)`)
	for _, matches := range pathRe.FindAllStringSubmatch(text, -1) {
		if len(matches) > 1 {
			name := matches[1]
			lower := strings.ToLower(name)
			if !seen[lower] && len(name) > 2 {
				seen[lower] = true
				entities = append(entities, DetectedEntity{Name: name, Type: "project"})
			}
		}
	}

	return entities
}

// PopulateKG adds detected entities to the knowledge graph.
func PopulateKG(kg *KnowledgeGraph, entities []DetectedEntity, context, source string) {
	for _, e := range entities {
		kg.AddTriple(e.Name, "is_a", e.Type, "", source) //nolint:errcheck
		if context != "" && len(context) < 100 {
			kg.AddTriple(e.Name, "mentioned_in", context, "", source) //nolint:errcheck
		}
	}
}

func isCommonPhrase(s string) bool {
	common := map[string]bool{
		"the first": true, "for example": true, "such as": true,
		"in order": true, "as well": true, "on the": true,
		"to the": true, "of the": true, "new york": true,
	}
	return common[s]
}
