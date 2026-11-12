package server

// Release approval mode writes (autonomous.merge_pipeline.release_approval.<size>,
// global or per project) reach the daemon through PUT /api/config. This file is
// the single place they are stored: each key is taken out of the patch and
// applied through autonomous.Manager.SetReleaseApprovalMode, so every change
// emits its audit event and stores its decision record. The calling surface is
// named by the ?surface= query parameter; a missing or unknown value is "rest".

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// releaseModeSurfaceParam is the query parameter a client sets to name the
// surface that makes a release approval mode change.
const releaseModeSurfaceParam = "surface"

// releaseModeSurfaceDefault is the surface of a PUT that names none.
const releaseModeSurfaceDefault = "rest"

// releaseModeSurfaces lists the surface names a release approval mode change
// may carry, the same vocabulary as autonomous.ReleaseModeChange.Surface.
var releaseModeSurfaces = map[string]bool{
	"pwa": true, "rest": true, "mcp": true, "comm": true, "cli": true, "config": true,
}

// errReleaseModeUnavailable means a release approval mode change cannot be
// recorded here (no autonomous manager, or no decision record service), so it
// is refused before anything is stored.
var errReleaseModeUnavailable = errors.New("release approval mode: the autonomous manager and decision records must be running to change it")

// releaseModeSetter is the part of *autonomous.Manager the PUT path needs.
type releaseModeSetter interface {
	SetReleaseApprovalMode(change autonomous.ReleaseModeChange, evidence autonomous.ReleaseReadiness, apply func(mode string) error, record autonomous.ReleaseModeRecorder) (string, error)
}

// releaseModeSurface reads the surface a PUT names, falling back to "rest".
func releaseModeSurface(r *http.Request) string {
	if r == nil {
		return releaseModeSurfaceDefault
	}
	s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(releaseModeSurfaceParam)))
	if releaseModeSurfaces[s] {
		return s
	}
	return releaseModeSurfaceDefault
}

// releaseModeKeyTarget reports whether key is a release approval mode key and,
// if so, the project it applies to ("" for global) and the release size.
func releaseModeKeyTarget(key string) (project, size string, ok bool) {
	pk, known, err := parseMergePipelineKey(key)
	if !known || err != nil {
		return "", "", false
	}
	size, ok = strings.CutPrefix(pk.field, "release_approval.")
	if !ok || (size != "patch" && size != "minor" && size != "major") {
		return "", "", false
	}
	return pk.project, size, true
}

// releaseModeRecordSource maps a change surface to the decision record channel
// the record store accepts (rest, mcp, cli or comm). The store overwrites a
// record's source with this value, so it cannot carry the surface name; the
// surface is kept in the audit event and the record's situation instead. The
// PWA and the configure body both reach the daemon over REST.
func releaseModeRecordSource(surface string) string {
	switch surface {
	case "mcp":
		return autonomous.DecisionSourceMCP
	case "cli":
		return autonomous.DecisionSourceCLI
	case "comm":
		return autonomous.DecisionSourceComm
	default:
		return autonomous.DecisionSourceREST
	}
}

// releaseModeOf picks the mode for one release size out of a resolved set.
func releaseModeOf(m config.ReleaseApprovalModes, size string) string {
	switch size {
	case "patch":
		return m.Patch
	case "minor":
		return m.Minor
	default:
		return m.Major
	}
}

// routeReleaseModeKeys removes every release approval mode key from patch and
// applies each one through SetReleaseApprovalMode, naming surface as the
// change's surface. Keys are applied in sorted order. The caller has already
// validated the patch, so a refusal here is a storage or record failure.
func (s *Server) routeReleaseModeKeys(surface string, patch map[string]interface{}) error {
	var keys []string
	for k := range patch {
		if _, _, ok := releaseModeKeyTarget(k); ok {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	setter, ok := s.autonomousMgr.(releaseModeSetter)
	if !ok || s.decisionRecords == nil {
		return errReleaseModeUnavailable
	}
	for _, k := range keys {
		v := patch[k]
		delete(patch, k)
		if err := s.applyReleaseModeKey(setter, surface, k, v); err != nil {
			return err
		}
	}
	return nil
}

// applyReleaseModeKey applies one release approval mode key for surface.
func (s *Server) applyReleaseModeKey(setter releaseModeSetter, surface, key string, v any) error {
	project, size, _ := releaseModeKeyTarget(key)
	mp := &s.cfg.Autonomous.MergePipeline

	// The old and new modes are the resolved ones the merge run acts on, so a
	// clear back to inherit is a change only when the inherited mode differs.
	oldMode := releaseModeOf(mp.EffectiveReleaseApprovalModes(project), size)
	next := mp.Clone()
	if _, err := applyMergePipelineKey(&next, key, v); err != nil {
		return err
	}
	newMode := releaseModeOf(next.EffectiveReleaseApprovalModes(project), size)

	// The no-op case stores the value without an event or record: the mode the
	// merge run acts on did not change, so there is nothing for the operator to
	// decide. The stored value is still saved, so the write is not dropped.
	if oldMode == newMode {
		_, err := applyMergePipelineKey(mp, key, v)
		return err
	}

	// The evidence is the readiness at this moment, read from the same stored
	// comparisons the readiness read loads, not an empty set.
	comparisons, plans, err := loadReleaseReadinessState(s.statePath())
	if err != nil {
		return fmt.Errorf("%s: release readiness state unreadable: %w", key, err)
	}
	records := autonomous.ReleaseReadinessRecordsFromComparisons(comparisons, plans, project)
	evidence, err := autonomous.ComputeReleaseReadiness(size, records, autonomous.DefaultReleaseReadinessBar())
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	change := autonomous.ReleaseModeChange{
		Size: size, OldMode: oldMode, NewMode: newMode,
		Actor: "operator", Surface: surface,
	}
	apply := func(string) error {
		_, err := applyMergePipelineKey(mp, key, v)
		return err
	}
	record := func(rec autonomous.DecisionRecord) (string, error) {
		view, _, err := s.decisionRecords.Create(rec, releaseModeRecordSource(surface), "")
		if err != nil {
			return "", err
		}
		return view.ID, nil
	}
	if _, err := setter.SetReleaseApprovalMode(change, evidence, apply, record); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}
