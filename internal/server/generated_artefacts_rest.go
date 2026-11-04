// generated_artefacts_rest.go — REST surface for the generated artefacts of
// an Automaton:
//
//	GET  /api/autonomous/generated-artefacts
//	GET  /api/autonomous/prds/{id}/change-record
//	GET  /api/autonomous/prds/{id}/generated/{kind}
//	POST /api/autonomous/prds/{id}/generated/{kind}/regenerate
//
// Reads use the autonomous read capability; regenerate writes a file in the
// project and uses the write capability.
package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/genartifacts"
)

// generatedArtefactAPI is the optional manager surface for the generated
// artefacts; AutonomousAPI fakes without it answer 501.
type generatedArtefactAPI interface {
	GeneratedChangeRecord(prdID string) (*changerecord.ChangeRecord, error)
	PreviewGeneratedArtefact(prdID, kind string) (*autonomous.GeneratedArtefactPreview, error)
	RegenerateGeneratedArtefact(prdID, kind string) (*autonomous.GeneratedArtefactResult, error)
}

// generatedArtefactConfigKeys lists the dotted config keys of the switches.
func generatedArtefactConfigKeys() []string {
	var out []string
	for _, k := range config.GeneratedArtefactKeys() {
		out = append(out, "autonomous.generated_artefacts."+k)
	}
	return out
}

// handleGeneratedArtefacts — GET effective settings plus the kinds.
func (s *Server) handleGeneratedArtefacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	eff := s.cfg.Autonomous.EffectiveGeneratedArtefacts()
	enabled := map[genartifacts.Kind]bool{
		genartifacts.GateReport: eff.GateReport, genartifacts.ChangelogDraft: eff.ChangelogDraft,
		genartifacts.ParityDraft: eff.ParityDraft, genartifacts.TrackerRows: eff.TrackerRows,
		genartifacts.RegistryRow: eff.RegistryRow,
	}
	paths := genartifacts.DefaultPaths()
	kinds := []map[string]any{}
	for _, k := range genartifacts.Kinds() {
		kinds = append(kinds, map[string]any{
			"kind":         string(k),
			"enabled":      enabled[k],
			"format":       genartifacts.Describe(k),
			"tracked_path": paths[k],
		})
	}
	writeJSONOK(w, map[string]any{"settings": eff, "kinds": kinds})
}

// handleGeneratedArtefactPRD serves the per-Automaton routes; action is the
// path after the id ("change-record" or "generated/{kind}[/regenerate]").
func (s *Server) handleGeneratedArtefactPRD(w http.ResponseWriter, r *http.Request, id, action string) {
	// Method and federation capability are checked before the Automaton is
	// looked up, so a peer without the capability gets the same answer for an
	// existing and a missing id.
	wantMethod, wantCap := http.MethodGet, federation.CapAutonomousRead
	if strings.HasSuffix(action, "/regenerate") {
		wantMethod, wantCap = http.MethodPost, federation.CapAutonomousWrite
	}
	if r.Method != wantMethod {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, wantCap) {
		return
	}
	if _, ok := s.autonomousMgr.GetPRD(id); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	api, ok := s.autonomousMgr.(generatedArtefactAPI)
	if !ok {
		http.Error(w, "generated artefacts are not supported by this autonomous manager", http.StatusNotImplemented)
		return
	}
	if action == "change-record" {
		rec, err := api.GeneratedChangeRecord(id)
		if err != nil {
			writeGeneratedErr(w, err)
			return
		}
		writeJSONOK(w, rec)
		return
	}
	parts := strings.Split(strings.TrimPrefix(action, "generated/"), "/")
	kind := parts[0]
	if !genartifacts.Kind(kind).Valid() {
		http.Error(w, "unknown generated artefact kind", http.StatusNotFound)
		return
	}
	switch {
	case len(parts) == 1:
		pv, err := api.PreviewGeneratedArtefact(id, kind)
		if err != nil {
			writeGeneratedErr(w, err)
			return
		}
		writeJSONOK(w, pv)
	case len(parts) == 2 && parts[1] == "regenerate":
		res, err := api.RegenerateGeneratedArtefact(id, kind)
		if err != nil {
			writeGeneratedErr(w, err)
			return
		}
		s.audit(r.Context(), "regenerate_artefact", "automaton", id, map[string]any{"kind": kind, "status": res.Status})
		writeJSONOK(w, res)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func writeGeneratedErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, autonomous.ErrGeneratedPRDNotFound), errors.Is(err, autonomous.ErrGeneratedKindUnknown),
		errors.Is(err, autonomous.ErrGeneratedNoChangeData):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
