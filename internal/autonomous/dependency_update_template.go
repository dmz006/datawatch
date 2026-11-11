// dependency_update_template.go — the built-in Weekly dependency update
// Automation template. Its data lives in templates/dependency-update-automaton.json
// (go:embed cannot reach outside the package) and is seeded into the
// TemplateStore on every start.

package autonomous

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// TemplateDependencyUpdate is the stable id of the built-in template.
const TemplateDependencyUpdate = "dependency_update"

//go:embed templates/dependency-update-automaton.json
var dependencyUpdateTemplateJSON []byte

// dependencyUpdateTemplateData is the shape of the embedded data file.
type dependencyUpdateTemplateData struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Type        string        `json:"type"`
	Tags        []string      `json:"tags"`
	Vars        []TemplateVar `json:"vars"`
	Preamble    string        `json:"preamble"`
	Tasks       []struct {
		Title string `json:"title"`
		Spec  string `json:"spec"`
	} `json:"tasks"`
}

func parseDependencyUpdateData() (*dependencyUpdateTemplateData, error) {
	var d dependencyUpdateTemplateData
	if err := json.Unmarshal(dependencyUpdateTemplateJSON, &d); err != nil {
		return nil, err
	}
	if d.ID == "" || d.Title == "" || len(d.Tasks) == 0 {
		return nil, fmt.Errorf("dependency update template data incomplete")
	}
	return &d, nil
}

// DependencyUpdateTemplate builds the built-in template from the embedded data.
func DependencyUpdateTemplate() (*Template, error) {
	d, err := parseDependencyUpdateData()
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	sb.WriteString(d.Preamble)
	for i, t := range d.Tasks {
		fmt.Fprintf(&sb, "\n\nTask %d: %s. %s", i+1, t.Title, t.Spec)
	}
	spec := sb.String()
	now := time.Now()
	return &Template{
		ID:          d.ID,
		Title:       d.Title,
		Description: d.Description,
		Spec:        spec,
		Type:        d.Type,
		Tags:        d.Tags,
		Vars:        ExtractVars(spec, d.Vars),
		IsBuiltin:   true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// seedBuiltinTemplates adds the built-in templates that are missing. It is
// idempotent and never overwrites an existing entry, so an operator-edited
// copy is kept. Callers hold no lock (it runs from newTemplateStore).
func (ts *TemplateStore) seedBuiltinTemplates() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	tmpl, err := DependencyUpdateTemplate()
	if err != nil {
		return
	}
	if _, ok := ts.templates[tmpl.ID]; ok {
		return
	}
	ts.templates[tmpl.ID] = tmpl
	_ = ts.persist()
}
