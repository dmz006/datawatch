// Package-local tolerant decoding for guardrail/verification Issues.
//
// Judge and verifier backends do not agree on the exact shape of the
// "issues" field: some emit a plain string array (the canonical shape),
// some emit arrays whose elements are objects like
// {"file": "a/b.go", "line": 42, "severity": "high", "message": "..."},
// some emit a single bare string or a single bare object instead of an
// array, and some mix strings and objects in one array.
//
// parseIssues normalizes all of those shapes into []string so that a
// non-canonical but well-formed issues value never makes the whole
// verdict/verification fail to decode or get dropped. It only returns
// an error for genuinely malformed JSON. Every other field of the two
// structs keeps strict typing: the custom UnmarshalJSON on
// GuardrailVerdict and VerificationResult only intercepts the "issues"
// key (via a json.RawMessage override inside an otherwise strict wire
// type), so unknown-type errors on any other field still surface
// exactly as before.
package autonomous

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// parseIssues accepts an absent/null/empty value (nil), a JSON array
// whose elements are each a string or an object (mixed allowed), a
// single bare string, a single bare object, or a bare number/bool
// (rendered as its JSON text). It never errors on a well-formed value.
func parseIssues(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var elems []json.RawMessage
		if err := json.Unmarshal(trimmed, &elems); err != nil {
			return nil, fmt.Errorf("autonomous: issues array: %w", err)
		}
		out := make([]string, 0, len(elems))
		for _, e := range elems {
			s, err := renderIssueElement(e)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
		}
		return out, nil
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, fmt.Errorf("autonomous: issues string: %w", err)
		}
		if s == "" {
			return nil, nil
		}
		return []string{s}, nil
	case '{':
		s, err := renderIssueObject(trimmed)
		if err != nil {
			return nil, err
		}
		return []string{s}, nil
	default:
		// Bare number or bool: keep the JSON text as the issue.
		return []string{string(trimmed)}, nil
	}
}

// renderIssueElement renders one element of an issues array: a string
// stays as-is, an object is rendered readably, and anything else
// (numbers, bools, null, nested arrays) becomes its JSON text.
func renderIssueElement(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", nil
	}
	switch trimmed[0] {
	case '"':
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", fmt.Errorf("autonomous: issues element: %w", err)
		}
		return s, nil
	case '{':
		return renderIssueObject(trimmed)
	default:
		if string(trimmed) == "null" {
			return "", nil
		}
		return string(trimmed), nil
	}
}

// issueText renders one field value of an issue object: strings lose
// their quotes, numbers come back without a spurious decimal, and
// anything else (bool, nested object/array) is its compact JSON text.
func issueText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	if f, err := strconv.ParseFloat(string(trimmed), 64); err == nil {
		if i := int64(f); float64(i) == f {
			return strconv.FormatInt(i, 10)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return string(trimmed)
}

// severityField / fileField / lineField pull the well-known issue
// object keys, tolerating either string or number values.
func severityField(obj map[string]json.RawMessage) string {
	if v, ok := obj["severity"]; ok {
		return issueText(v)
	}
	return ""
}

func fileField(obj map[string]json.RawMessage) string {
	if v, ok := obj["file"]; ok {
		return issueText(v)
	}
	return ""
}

func lineField(obj map[string]json.RawMessage) string {
	if v, ok := obj["line"]; ok {
		return issueText(v)
	}
	return ""
}

// descriptionField pulls the description, accepting issue/text/detail
// as fallbacks (first non-empty wins).
func descriptionField(obj map[string]json.RawMessage) string {
	for _, k := range []string{"description", "message", "issue", "text", "detail"} {
		if v, ok := obj[k]; ok {
			if s := issueText(v); s != "" {
				return s
			}
		}
	}
	return ""
}

// renderIssueObject renders one issue object into a readable single
// line, e.g. `[high] path/to/file.go:42: description text`. Key
// presence is best-effort: whatever of severity/file/line/description
// is available is included. An object with none of the known keys
// falls back to its compact JSON.
func renderIssueObject(raw json.RawMessage) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", fmt.Errorf("autonomous: issues object: %w", err)
	}
	severity := severityField(obj)
	file := fileField(obj)
	line := lineField(obj)
	desc := descriptionField(obj)

	if severity == "" && file == "" && line == "" && desc == "" {
		// No known keys at all: keep the compact JSON of the object.
		compact, encErr := json.Marshal(obj)
		if encErr != nil {
			return "", fmt.Errorf("autonomous: issues object fallback: %w", encErr)
		}
		return string(compact), nil
	}

	head := ""
	if severity != "" {
		head += "[" + severity + "]"
	}
	loc := ""
	if file != "" {
		loc = file
		if line != "" {
			loc += ":" + line
		}
	} else if line != "" {
		loc = line
	}
	if loc != "" {
		if head != "" {
			head += " "
		}
		head += loc
	}
	if desc != "" {
		if head != "" {
			if loc != "" {
				head += ": "
			} else {
				head += " "
			}
		}
		head += desc
	}
	return head, nil
}

// plain wire mirrors with only the issues field widened to a raw
// message; every other field keeps its exact declared type, so
// unknown-type errors on them still surface during unmarshaling.
type plainGuardrailVerdict GuardrailVerdict

type plainVerificationResult VerificationResult

type guardrailVerdictWire struct {
	plainGuardrailVerdict
	// Issues overrides the alias field with a raw capture; the alias
	// copy is what carries all the other fields.
	Issues json.RawMessage `json:"issues"`
}

type verificationResultWire struct {
	plainVerificationResult
	Issues json.RawMessage `json:"issues"`
}

// UnmarshalJSON decodes a GuardrailVerdict with a tolerant issues
// field (see parseIssues). All other fields are decoded strictly;
// a malformed issues JSON still errors instead of silently dropping
// the verdict.
func (v *GuardrailVerdict) UnmarshalJSON(b []byte) error {
	var w guardrailVerdictWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*v = GuardrailVerdict(w.plainGuardrailVerdict)
	issues, err := parseIssues(w.Issues)
	if err != nil {
		return err
	}
	v.Issues = issues
	return nil
}

// UnmarshalJSON decodes a VerificationResult with a tolerant issues
// field (see parseIssues). All other fields are decoded strictly.
func (r *VerificationResult) UnmarshalJSON(b []byte) error {
	var w verificationResultWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*r = VerificationResult(w.plainVerificationResult)
	issues, err := parseIssues(w.Issues)
	if err != nil {
		return err
	}
	r.Issues = issues
	return nil
}
