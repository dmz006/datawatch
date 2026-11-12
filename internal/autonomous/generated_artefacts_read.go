// generated_artefacts_read.go — read, preview and regenerate surface for the
// generated artefacts of one Automaton (REST and MCP use these).
package autonomous

import (
	"errors"
	"fmt"
	"os"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// Lookup errors for the generated-artefact surface; the server maps both to 404.
var (
	ErrGeneratedPRDNotFound  = errors.New("automaton not found")
	ErrGeneratedKindUnknown  = errors.New("unknown generated artefact kind")
	ErrGeneratedNoChangeData = errors.New("no change record stored for this automaton")
)

// GeneratedArtefactPreview is a rendered artefact that was not written.
type GeneratedArtefactPreview struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Path     string   `json:"path"`
	Enabled  bool     `json:"enabled"`
	Content  string   `json:"content"`
	Warnings []string `json:"warnings,omitempty"`
}

// GeneratedArtefactResult is the outcome of a regenerate.
type GeneratedArtefactResult struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Status   string   `json:"status"` // written | skipped | disabled
	Path     string   `json:"path"`
	Reason   string   `json:"reason,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

func (m *Manager) generatedSettingsPtr() *config.EffectiveGeneratedArtefactsSettings {
	if s, ok := m.generatedArtefactSettings(); ok {
		return &s
	}
	return nil
}

// GeneratedChangeRecord returns the stored merged change record for an
// Automaton. When only per-task records exist it merges them on the fly.
func (m *Manager) GeneratedChangeRecord(prdID string) (*changerecord.ChangeRecord, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok || prd == nil {
		return nil, ErrGeneratedPRDNotFound
	}
	if rec, err := changerecord.Load(m.dataDir, prd.ID); err == nil {
		if rec.Cost == nil {
			m.attachGeneratedCost(rec)
		}
		return rec, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var recs []*changerecord.ChangeRecord
	for _, st := range prd.Story {
		for i := range st.Tasks {
			if r, err := changerecord.Load(m.dataDir, st.Tasks[i].ID); err == nil {
				recs = append(recs, r)
			}
		}
	}
	if len(recs) == 0 {
		return nil, ErrGeneratedNoChangeData
	}
	merged := changerecord.Merge(recs...)
	merged.AutomatonID = prd.ID
	m.attachGeneratedCost(merged)
	return merged, nil
}

func (m *Manager) generatedContext(prdID, kind string) (*PRD, genartifacts.Kind, *changerecord.ChangeRecord, genartifacts.Options, error) {
	k := genartifacts.Kind(kind)
	if !k.Valid() {
		return nil, k, nil, genartifacts.Options{}, fmt.Errorf("%w: %q", ErrGeneratedKindUnknown, kind)
	}
	prd, ok := m.store.GetPRD(prdID)
	if !ok || prd == nil {
		return nil, k, nil, genartifacts.Options{}, ErrGeneratedPRDNotFound
	}
	rec, err := m.GeneratedChangeRecord(prdID)
	if errors.Is(err, ErrGeneratedNoChangeData) {
		rec = &changerecord.ChangeRecord{AutomatonID: prd.ID}
	} else if err != nil {
		return nil, k, nil, genartifacts.Options{}, err
	}
	opts := genartifacts.Options{Root: prd.ProjectDir, Settings: m.generatedSettingsPtr()}
	return prd, k, rec, opts, nil
}

// PreviewGeneratedArtefact renders an artefact without writing it.
func (m *Manager) PreviewGeneratedArtefact(prdID, kind string) (*GeneratedArtefactPreview, error) {
	_, k, rec, opts, err := m.generatedContext(prdID, kind)
	if err != nil {
		return nil, err
	}
	out := &GeneratedArtefactPreview{ID: prdID, Kind: kind, Path: genartifacts.TrackedPath(k, rec, opts), Enabled: opts.Enabled(k)}
	if !out.Enabled {
		return out, nil
	}
	content, warns, err := genartifacts.Render(k, rec, opts)
	if err != nil {
		return nil, err
	}
	out.Content, out.Warnings = content, warns
	return out, nil
}

// RegenerateGeneratedArtefact re-renders and writes an artefact. A file the
// person edited is kept: the result is Skipped with the reason.
func (m *Manager) RegenerateGeneratedArtefact(prdID, kind string) (*GeneratedArtefactResult, error) {
	_, k, rec, opts, err := m.generatedContext(prdID, kind)
	if err != nil {
		return nil, err
	}
	res, err := genartifacts.Generate(k, rec, opts)
	if err != nil {
		return nil, err
	}
	return &GeneratedArtefactResult{ID: prdID, Kind: kind, Status: string(res.Status), Path: res.Path, Reason: res.Reason, Warnings: res.Warnings}, nil
}
