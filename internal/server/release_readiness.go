package server

// GET /api/autonomous/release-readiness?size=patch|minor|major[&project=NAME]
//
// The readiness of one release size, judged from the stored shadow-merge
// comparisons: how many of the plans held up against the landed state, how
// many of those merges would have had conflicts, how many of them touched
// the PWA, and whether any comparison found a defect that blocks the
// release. The bar is the default (10 records, 2 conflicts, 2 PWA changes).
//
// A size with no comparisons is not hidden: the answer is the same full
// shape with zero counts and a sentence for each short bar, so an operator
// can tell a size that is short from a size that was never compared.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// releaseReadinessSizes are the release sizes the endpoint accepts.
var releaseReadinessSizes = map[string]bool{"patch": true, "minor": true, "major": true}

// releaseComparisonWire is one stored shadow-merge comparison file under
// <dataDir>/merge-comparisons, as the shadow-merge store writes it. Only the
// fields the readiness judgement uses are named here; the rest is ignored on
// decode.
type releaseComparisonWire struct {
	AutomatonID string `json:"automaton_id"`
	ReleaseSize string `json:"release_size"`
	Different   int    `json:"different"`
	Differences []struct {
		Field    string `json:"field"`
		Severity string `json:"severity"`
	} `json:"differences"`
}

// releasePlanWire is the readiness-relevant part of one stored shadow-merge
// plan file under <dataDir>/merge-plans.
type releasePlanWire struct {
	AutomatonID       string `json:"automaton_id"`
	Project           string `json:"project"`
	ParityIssueNeeded bool   `json:"parity_issue_needed"`
	Conflicts         []struct {
		Path string `json:"path"`
	} `json:"conflicts"`
}

// handleReleaseReadiness serves GET /api/autonomous/release-readiness.
// size is required and must be patch, minor or major; project is optional
// and keeps only the comparisons whose plan belongs to that project. A
// request without a usable size is a 400 that names the size it got; a
// request with one is always the full readiness structure, zeros included.
func (s *Server) handleReleaseReadiness(w http.ResponseWriter, r *http.Request) {
	if !s.mergePipelineGate(w, r, map[string]string{http.MethodGet: federation.CapAutonomousRead}) {
		return
	}
	q := r.URL.Query()
	size := strings.ToLower(strings.TrimSpace(q.Get("size")))
	if !releaseReadinessSizes[size] {
		writeJSONError(w, http.StatusBadRequest, map[string]string{
			"code":  "invalid_size",
			"error": fmt.Sprintf("The size parameter is required and must be patch, minor or major; got %q.", q.Get("size")),
		})
		return
	}
	project := strings.TrimSpace(q.Get("project"))

	comparisons, plans, err := loadReleaseReadinessState(s.statePath())
	if err != nil {
		http.Error(w, "release readiness state unreadable: "+err.Error(), http.StatusInternalServerError)
		return
	}
	records := autonomous.ReleaseReadinessRecordsFromComparisons(comparisons, plans, project)
	bar := autonomous.DefaultReleaseReadinessBar()
	readiness, err := autonomous.ComputeReleaseReadiness(size, records, bar)
	if err != nil {
		// Unreachable: the size was checked above, but name it if it ever happens.
		writeJSONError(w, http.StatusBadRequest, map[string]string{
			"code":  "invalid_size",
			"error": err.Error(),
		})
		return
	}
	if readiness.Missing == nil {
		readiness.Missing = []string{}
	}
	writeJSONOK(w, map[string]any{
		"size":      size,
		"project":   project,
		"readiness": readiness,
		"bar":       bar,
	})
}

// loadReleaseReadinessState reads the stored shadow-merge comparisons and
// plans under the data directory. A directory that does not exist is an
// empty set (a daemon that never ran a shadow merge); a store that exists
// but cannot be read is an error. Files that are not parseable comparisons
// are skipped: one corrupt record must not hide the readiness of the rest.
// The ids are sorted so the order of the records is stable.
func loadReleaseReadinessState(dataDir string) ([]autonomous.ComparisonInput, map[string]autonomous.PlanRef, error) {
	comparisons, err := readReleaseJSONFiles[releaseComparisonWire](filepath.Join(dataDir, "merge-comparisons"))
	if err != nil {
		return nil, nil, err
	}
	plans, err := readReleaseJSONFiles[releasePlanWire](filepath.Join(dataDir, "merge-plans"))
	if err != nil {
		return nil, nil, err
	}

	out := make([]autonomous.ComparisonInput, 0, len(comparisons))
	for _, c := range comparisons {
		diffs := make([]autonomous.DifferenceInput, 0, len(c.Differences))
		for _, d := range c.Differences {
			diffs = append(diffs, autonomous.DifferenceInput{Field: d.Field, Severity: d.Severity})
		}
		out = append(out, autonomous.ComparisonInput{
			AutomatonID: c.AutomatonID,
			ReleaseSize: c.ReleaseSize,
			Different:   c.Different,
			Differences: diffs,
		})
	}

	refs := make(map[string]autonomous.PlanRef, len(plans))
	for _, p := range plans {
		if p.AutomatonID == "" {
			continue
		}
		refs[p.AutomatonID] = autonomous.PlanRef{
			Project:           p.Project,
			Conflicts:         len(p.Conflicts),
			ParityIssueNeeded: p.ParityIssueNeeded,
		}
	}
	return out, refs, nil
}

// readReleaseJSONFiles lists <dir>/*.json and decodes each into T. A
// directory that does not exist returns an empty slice; an unreadable
// directory is an error. Entries that are not *.json files, or that fail
// to decode, are skipped.
func readReleaseJSONFiles[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var out []T
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue // unreadable files are skipped, not fatal to the read
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			continue
		}
		out = append(out, v)
	}
	return out, nil
}
