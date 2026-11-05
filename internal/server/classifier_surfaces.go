// classifier_surfaces.go — the classification judge's operator surfaces:
//
//	GET  /api/llm/classifier                 verification modes, probe and calibration status, allowlist
//	GET  /api/llm/classifier/publishers      the publisher allowlist
//	POST /api/llm/classifier/publishers      add a publisher (explicit confirm required, audited)
//	GET  /api/llm/classifier/verdicts        verdict records of an Automaton (mode, scores, route)
//
// The MCP tool, CLI and chat commands proxy these routes.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/classifier"
	"github.com/dmz006/datawatch/internal/federation"
)

// ClassifierStatusSource reads the classifier's live state. The daemon installs
// one; tests pass a fake.
type ClassifierStatusSource interface {
	// Probes returns the cached capability probe results.
	Probes() []classifier.ProbeStatus
	// Calibrations returns the stored calibration records.
	Calibrations() ([]*classifier.Record, error)
}

// SetClassifierStatus wires the classifier status source into the server.
func (s *Server) SetClassifierStatus(src ClassifierStatusSource) { s.classifierStatus = src }

// classifierVerificationMode is one LLM entry's resolved verification mode.
type classifierVerificationMode struct {
	LLM  string `json:"llm"`
	Mode string `json:"verification_mode"`
}

// classifierCalibrationView is one calibration record as the status shows it.
type classifierCalibrationView struct {
	Model           string  `json:"model"`
	Quantization    string  `json:"quantization,omitempty"`
	Node            string  `json:"node,omitempty"`
	FingerprintHash string  `json:"fingerprint_hash"`
	Threshold       float64 `json:"threshold"`
	Test            any     `json:"test"`
	CreatedAt       string  `json:"created_at"`
}

// handleClassifierStatus serves GET /api/llm/classifier.
func (s *Server) handleClassifierStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapLLMsRead) {
		return
	}
	modes := []classifierVerificationMode{}
	if s.inferenceReg != nil {
		for _, l := range s.inferenceReg.List() {
			modes = append(modes, classifierVerificationMode{LLM: l.Name, Mode: string(l.EffectiveVerificationMode())})
		}
		sort.Slice(modes, func(i, j int) bool { return modes[i].LLM < modes[j].LLM })
	}
	out := map[string]any{
		"verification_modes":  modes,
		"probes":              []classifier.ProbeStatus{},
		"calibrations":        []classifierCalibrationView{},
		"publisher_allowlist": s.classifierAllowlist(),
		"wired":               s.classifierStatus != nil,
	}
	if s.classifierStatus != nil {
		if p := s.classifierStatus.Probes(); p != nil {
			out["probes"] = p
		}
		recs, err := s.classifierStatus.Calibrations()
		if err != nil {
			// A calibration store that cannot be read is reported, never hidden.
			out["calibration_error"] = err.Error()
		}
		views := make([]classifierCalibrationView, 0, len(recs))
		for _, rec := range recs {
			views = append(views, classifierCalibrationView{
				Model: rec.Fingerprint.Model, Quantization: rec.Fingerprint.Quantization, Node: rec.Fingerprint.Node,
				FingerprintHash: rec.FingerprintHash, Threshold: rec.Threshold, Test: rec.Test,
				CreatedAt: rec.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		out["calibrations"] = views
	}
	writeJSONOK(w, out)
}

// classifierAllowlist returns a copy of the configured publisher allowlist.
func (s *Server) classifierAllowlist() []string {
	if s.cfg == nil {
		return []string{}
	}
	return append([]string{}, s.cfg.Classifier.PublisherAllowlist...)
}

// publisherAddReq is the body of POST /api/llm/classifier/publishers. Confirm
// must be true: adding a publisher widens which models may judge.
type publisherAddReq struct {
	Publisher string `json:"publisher"`
	Confirm   bool   `json:"confirm"`
	Actor     string `json:"actor"`
}

// handleClassifierPublishers serves GET and POST /api/llm/classifier/publishers.
func (s *Server) handleClassifierPublishers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapLLMsRead) {
			return
		}
		writeJSONOK(w, map[string]any{"publisher_allowlist": s.classifierAllowlist()})
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapLLMsWrite) {
			return
		}
		var req publisherAddReq
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		pub := classifier.NormalizePublisher(req.Publisher)
		if pub == "" {
			http.Error(w, "publisher required", http.StatusBadRequest)
			return
		}
		if !req.Confirm {
			http.Error(w, fmt.Sprintf("adding publisher %q widens which models may judge; resend with confirm=true to confirm", pub), http.StatusBadRequest)
			return
		}
		if s.cfg == nil || s.cfgPath == "" {
			http.Error(w, "config not available", http.StatusServiceUnavailable)
			return
		}
		s.classifierMu.Lock()
		defer s.classifierMu.Unlock()
		next, changed := classifier.AddPublisher(s.cfg.Classifier.PublisherAllowlist, pub)
		if changed {
			prev := s.cfg.Classifier.PublisherAllowlist
			s.cfg.Classifier.PublisherAllowlist = next
			if err := s.saveConfig(); err != nil {
				s.cfg.Classifier.PublisherAllowlist = prev
				http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			actor := strings.TrimSpace(req.Actor)
			if actor == "" {
				actor = "operator"
			}
			s.audit(r.Context(), "classifier_publisher_add", "classifier", pub, map[string]any{
				"publisher": pub, "actor": actor, "confirmed": true,
			})
		}
		writeJSONOK(w, map[string]any{"ok": true, "added": changed, "publisher": pub, "publisher_allowlist": s.classifierAllowlist()})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClassifierVerdicts serves GET /api/llm/classifier/verdicts?automaton=ID
// [&task=ID][&low_confidence=true].
func (s *Server) handleClassifierVerdicts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous is disabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	id := q.Get("automaton")
	if id == "" {
		http.Error(w, "automaton query parameter required", http.StatusBadRequest)
		return
	}
	raw, ok := s.autonomousMgr.GetPRD(id)
	if !ok || isNilPRD(raw) {
		http.Error(w, fmt.Sprintf("automaton %q not found", id), http.StatusNotFound)
		return
	}
	prd, err := toPRD(raw)
	if err != nil {
		http.Error(w, "automaton: "+err.Error(), http.StatusInternalServerError)
		return
	}
	low := q.Get("low_confidence") == "true"
	recs := autonomous.ClassifyRecords(prd, q.Get("task"), low)
	if recs == nil {
		recs = []autonomous.ClassifyRecord{}
	}
	writeJSONOK(w, map[string]any{"automaton_id": id, "verdicts": recs})
}
