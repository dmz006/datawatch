package packs

import (
	"path/filepath"
	"sort"
	"strings"
)

// PackSummary is the operator-facing overview of what a pack contributes: the
// task titles, the verification script file names, and the tools and file
// scopes its tasks ask for.
type PackSummary struct {
	Name                string   `json:"name"`
	Version             string   `json:"version"`
	TaskTitles          []string `json:"task_titles"`
	VerificationScripts []string `json:"verification_scripts"`
	Tools               []string `json:"tools"`
	FileScopes          []string `json:"file_scopes"`
}

// Summarize builds the summary of pack. Task titles keep the manifest order.
// Verification scripts are reduced to their file names, so a path such as
// "verify/check.sh" lists as "check.sh". Tools and file scopes are de-duplicated
// and sorted. Empty entries are skipped. A nil pack gives an empty summary.
func Summarize(pack *Pack) PackSummary {
	s := PackSummary{
		TaskTitles:          []string{},
		VerificationScripts: []string{},
		Tools:               []string{},
		FileScopes:          []string{},
	}
	if pack == nil {
		return s
	}
	s.Name = pack.Name
	s.Version = pack.Version

	scripts := map[string]bool{}
	tools := map[string]bool{}
	scopes := map[string]bool{}
	for _, t := range pack.Tasks {
		if title := strings.TrimSpace(t.Title); title != "" {
			s.TaskTitles = append(s.TaskTitles, title)
		}
		if script := strings.TrimSpace(t.VerificationScript); script != "" {
			scripts[filepath.Base(script)] = true
		}
		for _, tool := range t.Tools {
			if tool = strings.TrimSpace(tool); tool != "" {
				tools[tool] = true
			}
		}
		for _, scope := range t.FileScopes {
			if scope = strings.TrimSpace(scope); scope != "" {
				scopes[scope] = true
			}
		}
	}
	s.VerificationScripts = sortedKeys(scripts)
	s.Tools = sortedKeys(tools)
	s.FileScopes = sortedKeys(scopes)
	return s
}

// sortedKeys returns the keys of set in sorted order, or an empty slice.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
