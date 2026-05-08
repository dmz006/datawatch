// BL274 Sprint 1, v6.16.0 — Front-matter exec_steps parser.
//
// Operator-decided model (Q4d + Q4a): critical mutating howtos carry
// a YAML front-matter block declaring the deterministic MCP-call
// sequence; non-curated howtos fall back to LLM-translation at apply
// time. This file parses the curated form.
//
// Schema (in YAML front-matter at the top of howto/*.md):
//
//   ---
//   docs:
//     index: true                # already used by the corpus indexer
//   exec_steps:
//     - tool: secret_set
//       args: {name: "{{params.name}}", value: "{{params.value}}"}
//       description: "Save the secret"
//       read_only: false         # default false; true marks a non-mutating step
//     - tool: secret_get
//       args: {name: "{{params.name}}"}
//       description: "Read it back to confirm"
//       read_only: true
//   exec_params:
//     - name: name
//       description: "Secret name"
//       required: true
//     - name: value
//       description: "Secret value"
//       required: true
//   ---
//
// All exec_step args strings are template-expanded at docs_apply time
// using {{params.<key>}} substitution against operator-supplied params.
// A step is rejected if it references {{params.X}} where X isn't in
// exec_params.

package docsindex

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FrontMatter captures the operator-relevant fields. Non-listed fields
// in the YAML are tolerated (PAI + other tooling may add their own).
type FrontMatter struct {
	Docs       *DocsBlock `yaml:"docs,omitempty"`
	ExecSteps  []ExecStep `yaml:"exec_steps,omitempty"`
	ExecParams []ExecParam `yaml:"exec_params,omitempty"`
}

// DocsBlock is the index-opt-in for plugin/skill manifests AND the
// per-howto exec metadata pointer. For core howtos, Docs is rarely
// set explicitly — the corpus indexer auto-includes them.
type DocsBlock struct {
	Index          bool     `yaml:"index,omitempty"`
	Files          []string `yaml:"files,omitempty"`
	ExcludeDefault bool     `yaml:"exclude_default,omitempty"`
	Topics         []string `yaml:"topics,omitempty"`
	Howtos         []HowtoMeta `yaml:"howtos,omitempty"`
}

// HowtoMeta is the optional howto declaration in plugin/skill manifests.
type HowtoMeta struct {
	File   string   `yaml:"file"`
	Topic  string   `yaml:"topic,omitempty"`
	Params []string `yaml:"params,omitempty"`
}

// ExecStep is one curated MCP-call step. tool MUST exist in the live
// MCP registry (CI lint validates this); args may use {{params.X}}
// templates against ExecParams.
//
// Provenance tracks how the step was generated:
//   - "authored"        — came from a hand-written front-matter exec_steps block
//   - "llm_translated"  — generated at apply-time by the LLM-translation fallback (Sprint 3)
//
// Provenance is set by the caller (front-matter parse → "authored",
// translator → "llm_translated"); YAML tag exists so authored howtos
// can override (e.g. mark a partially-machine-generated step).
type ExecStep struct {
	Tool        string                 `yaml:"tool"`
	Args        map[string]interface{} `yaml:"args,omitempty"`
	Description string                 `yaml:"description"`
	ReadOnly    bool                   `yaml:"read_only,omitempty"`
	Provenance  string                 `yaml:"provenance,omitempty"`
}

// ExecParam declares one operator-supplied parameter.
type ExecParam struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	Default     string `yaml:"default,omitempty"`
}

var (
	frontMatterDelimRE = regexp.MustCompile(`(?ms)\A---\n(.*?)\n---\n`)
	paramRefRE         = regexp.MustCompile(`\{\{\s*params\.([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)
)

// ParseFrontMatter extracts the YAML front-matter block from the doc
// body. Returns a zero-value FrontMatter (no error) when absent.
func ParseFrontMatter(body string) (FrontMatter, error) {
	m := frontMatterDelimRE.FindStringSubmatch(body)
	if m == nil {
		return FrontMatter{}, nil
	}
	var fm FrontMatter
	if err := yaml.Unmarshal([]byte(m[1]), &fm); err != nil {
		return FrontMatter{}, fmt.Errorf("docsindex: parse frontmatter: %w", err)
	}
	return fm, nil
}

// ParseFrontMatterYAML parses raw YAML (no surrounding `---` markers) —
// used by the runtime for chunks that carry FrontmatterRaw stamped at
// chunk time. Returns a zero-value FrontMatter (no error) when raw is empty.
func ParseFrontMatterYAML(raw string) (FrontMatter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return FrontMatter{}, nil
	}
	var fm FrontMatter
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		return FrontMatter{}, fmt.Errorf("docsindex: parse frontmatter (raw): %w", err)
	}
	return fm, nil
}

// HasExecSteps reports whether a parsed front-matter has the curated
// exec_steps block (i.e. it's a "provenance: authored" howto).
func (fm FrontMatter) HasExecSteps() bool {
	return len(fm.ExecSteps) > 0
}

// ResolveExecSteps applies operator-supplied params to the templated
// args and returns the concrete step list. Errors when:
//   - A required ExecParam is missing from `params`.
//   - A step's args reference {{params.X}} where X isn't declared.
//
// Each step's read_only field is preserved so the docs_apply per-step
// risk-gate (Q3d) can distinguish read vs mutating steps.
func (fm FrontMatter) ResolveExecSteps(params map[string]string) ([]ExecStep, error) {
	// Validate required params present.
	declared := map[string]ExecParam{}
	for _, p := range fm.ExecParams {
		declared[p.Name] = p
		if p.Required {
			if _, ok := params[p.Name]; !ok {
				return nil, fmt.Errorf("required param %q missing", p.Name)
			}
		}
	}
	// Apply defaults.
	effective := map[string]string{}
	for name, p := range declared {
		if v, ok := params[name]; ok {
			effective[name] = v
		} else if p.Default != "" {
			effective[name] = p.Default
		}
	}
	// Allow additional params (operator might pass a few extra) even if
	// not declared — best-effort substitution. But declared params must
	// exist if a step references them.

	// Walk steps, expanding templates.
	out := make([]ExecStep, 0, len(fm.ExecSteps))
	for i, step := range fm.ExecSteps {
		if step.Tool == "" {
			return nil, fmt.Errorf("step %d: tool required", i)
		}
		newArgs, err := expandArgs(step.Args, effective, declared)
		if err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i, step.Tool, err)
		}
		prov := step.Provenance
		if prov == "" {
			prov = "authored"
		}
		out = append(out, ExecStep{
			Tool:        step.Tool,
			Args:        newArgs,
			Description: step.Description,
			ReadOnly:    step.ReadOnly,
			Provenance:  prov,
		})
	}
	return out, nil
}

// expandArgs walks a generic args map, applying {{params.X}} substitution
// to every string leaf. Non-string types (numbers, bools, nested maps,
// arrays) pass through; strings get template-expanded.
func expandArgs(args map[string]interface{}, effective map[string]string, declared map[string]ExecParam) (map[string]interface{}, error) {
	if args == nil {
		return nil, nil
	}
	out := make(map[string]interface{}, len(args))
	for k, v := range args {
		expanded, err := expandValue(v, effective, declared)
		if err != nil {
			return nil, err
		}
		out[k] = expanded
	}
	return out, nil
}

func expandValue(v interface{}, effective map[string]string, declared map[string]ExecParam) (interface{}, error) {
	switch t := v.(type) {
	case string:
		return expandString(t, effective, declared)
	case map[string]interface{}:
		return expandArgs(t, effective, declared)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			ev, err := expandValue(item, effective, declared)
			if err != nil {
				return nil, err
			}
			out[i] = ev
		}
		return out, nil
	default:
		return v, nil
	}
}

func expandString(s string, effective map[string]string, declared map[string]ExecParam) (string, error) {
	matches := paramRefRE.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(s[last:m[0]])
		paramName := s[m[2]:m[3]]
		// If the param is declared in exec_params, an undeclared use is
		// a curation bug. Flag it.
		if len(declared) > 0 {
			if _, declaredOK := declared[paramName]; !declaredOK {
				return "", fmt.Errorf("template references undeclared param {{params.%s}}", paramName)
			}
		}
		val, ok := effective[paramName]
		if !ok {
			return "", fmt.Errorf("template references unset param {{params.%s}}", paramName)
		}
		out.WriteString(val)
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String(), nil
}
