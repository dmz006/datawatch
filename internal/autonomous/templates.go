// BL221 (v6.2.0) — dedicated Template store for the Automata redesign.
// Templates are first-class spec blueprints distinct from is_template PRDs.
// Stored in templates.jsonl alongside prds.jsonl; loaded fully into memory.

package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TemplateStore holds templates in memory and persists to templates.jsonl.
type TemplateStore struct {
	mu        sync.Mutex
	templates map[string]*Template
	dir       string
}

func newTemplateStore(dir string) *TemplateStore {
	ts := &TemplateStore{
		templates: map[string]*Template{},
		dir:       dir,
	}
	_ = ts.load()
	return ts
}

func (ts *TemplateStore) load() error {
	path := filepath.Join(ts.dir, "templates.jsonl")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var tmpl Template
		if err := json.Unmarshal([]byte(line), &tmpl); err != nil {
			continue
		}
		ts.templates[tmpl.ID] = &tmpl
	}
	return nil
}

func (ts *TemplateStore) persist() error {
	path := filepath.Join(ts.dir, "templates.jsonl")
	var sb strings.Builder
	for _, tmpl := range ts.templates {
		b, err := json.Marshal(tmpl)
		if err != nil {
			return err
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(sb.String()), 0600)
}

func (ts *TemplateStore) Create(title, description, spec, typ string, tags []string) (*Template, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	now := time.Now()
	tmpl := &Template{
		ID:          newID(),
		Title:       title,
		Description: description,
		Spec:        spec,
		Type:        typ,
		Tags:        tags,
		Vars:        ExtractVars(spec, nil),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	ts.templates[tmpl.ID] = tmpl
	return tmpl, ts.persist()
}

func (ts *TemplateStore) Get(id string) (*Template, bool) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	t, ok := ts.templates[id]
	return t, ok
}

func (ts *TemplateStore) List() []*Template {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]*Template, 0, len(ts.templates))
	for _, t := range ts.templates {
		out = append(out, t)
	}
	// newest first
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func (ts *TemplateStore) Update(id, title, description, spec, typ string, tags []string, vars []TemplateVar) (*Template, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	tmpl, ok := ts.templates[id]
	if !ok {
		return nil, fmt.Errorf("template %q not found", id)
	}
	if tmpl.IsBuiltin {
		return nil, fmt.Errorf("template %q is built-in and cannot be edited", id)
	}
	if title != "" {
		tmpl.Title = title
	}
	if description != "" {
		tmpl.Description = description
	}
	if spec != "" {
		tmpl.Spec = spec
		tmpl.Vars = ExtractVars(spec, vars)
	} else if vars != nil {
		tmpl.Vars = vars
	}
	if typ != "" {
		tmpl.Type = typ
	}
	if tags != nil {
		tmpl.Tags = tags
	}
	tmpl.UpdatedAt = time.Now()
	return tmpl, ts.persist()
}

func (ts *TemplateStore) Delete(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, ok := ts.templates[id]; !ok {
		return fmt.Errorf("template %q not found", id)
	}
	delete(ts.templates, id)
	return ts.persist()
}

// RecordUse bumps UseCount and sets LastUsedAt. Called on instantiation.
func (ts *TemplateStore) RecordUse(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	tmpl, ok := ts.templates[id]
	if !ok {
		return
	}
	tmpl.UseCount++
	now := time.Now()
	tmpl.LastUsedAt = &now
	_ = ts.persist()
}

// Instantiate substitutes {{var_name}} in spec using provided vars and
// returns a substituted spec string. Validates required vars.
func (ts *TemplateStore) Instantiate(id string, vars map[string]string) (spec, title string, err error) {
	ts.mu.Lock()
	tmpl, ok := ts.templates[id]
	ts.mu.Unlock()
	if !ok {
		return "", "", fmt.Errorf("template %q not found", id)
	}
	resolved := make(map[string]string, len(tmpl.Vars))
	for _, v := range tmpl.Vars {
		if val, ok := vars[v.Name]; ok && val != "" {
			resolved[v.Name] = val
			continue
		}
		if v.Default != "" {
			resolved[v.Name] = v.Default
			continue
		}
		if v.Required {
			return "", "", fmt.Errorf("template var %q is required but missing", v.Name)
		}
	}
	subst := func(s string) string {
		for name, val := range resolved {
			s = strings.ReplaceAll(s, "{{"+name+"}}", val)
		}
		return s
	}
	ts.RecordUse(id)
	return subst(tmpl.Spec), subst(tmpl.Title), nil
}
