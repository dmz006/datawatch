package autonomous

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// PairingWarnings reports, in plain language, each autonomous model setting
// that will not reach its session because the model does not pair with the
// backend it is used with.
//
// cfg holds the effective planning and verification settings. decomposeRefs
// maps every decompose backend referenced by a non-finished PRD to the IDs of
// those PRDs. The planning check calls resolvePlannedModel, the same resolver
// that picks the model for a decompose session, so a warning is raised exactly
// when the resolver would pass no model. The verification check calls
// carriesForeignBackendPrefix, the same pairing rule the resolver applies to
// planning. An empty backend is skipped because its concrete backend is only
// known at session time.
//
// Pure: it reads only its arguments. The result is never nil and is sorted by
// decompose backend, so repeated calls give the same output.
func PairingWarnings(cfg Config, decomposeRefs map[string][]string) []string {
	out := []string{}
	if cfg.PlanningModel != "" {
		backends := make([]string, 0, len(decomposeRefs))
		for b := range decomposeRefs {
			if b != "" {
				backends = append(backends, b)
			}
		}
		sort.Strings(backends)
		for _, b := range backends {
			if resolvePlannedModel(b, "", "", "", cfg.PlanningBackend, cfg.PlanningModel) != "" {
				continue
			}
			out = append(out, planningPairingWarning(cfg, b, decomposeRefs[b]))
		}
	}
	if cfg.VerificationBackend != "" && cfg.VerificationModel != "" && carriesForeignBackendPrefix(cfg.VerificationModel, cfg.VerificationBackend) {
		prefix, _ := foreignModelPrefix(cfg.VerificationModel, cfg.VerificationBackend)
		out = append(out, fmt.Sprintf("verification model %q looks like it belongs to backend %q, but verification uses backend %q, so the model is sent to a session that may reject it.",
			cfg.VerificationModel, prefix, cfg.VerificationBackend))
	}
	return out
}

// planningPairingWarning words the planning warning for one decompose
// backend. It names the backend the planning model belongs to: the model's
// own "<prefix>/" when it has one, otherwise the configured planning backend.
func planningPairingWarning(cfg Config, decomposeBackend string, prdIDs []string) string {
	if prefix, foreign := foreignModelPrefix(cfg.PlanningModel, decomposeBackend); foreign {
		return fmt.Sprintf("planning model %q looks like it belongs to backend %q, but %s decomposes with backend %q, so no model will be passed to that session.",
			cfg.PlanningModel, prefix, prdNoun(prdIDs), decomposeBackend)
	}
	return fmt.Sprintf("planning model %q is set for planning backend %q, but %s decomposes with backend %q, so no model will be passed to that session.",
		cfg.PlanningModel, cfg.PlanningBackend, prdNoun(prdIDs), decomposeBackend)
}

// prdNoun renders the PRD IDs as "PRD x" or "PRDs x, y".
func prdNoun(ids []string) string {
	if len(ids) == 0 {
		return "a PRD"
	}
	if len(ids) == 1 {
		return "PRD " + ids[0]
	}
	return "PRDs " + strings.Join(ids, ", ")
}

// prdFinishedForPairing reports whether a PRD has reached a state where it
// will not decompose again. Failed PRDs are not finished: they can be reset
// and re-run.
func prdFinishedForPairing(s PRDStatus) bool {
	switch s {
	case PRDCompleted, PRDCancelled, PRDRejected, PRDArchived:
		return true
	}
	return false
}

// pairingDecomposeRefs groups the non-finished PRDs by the decompose backend
// the planner resolves for each one: the PRD's own decomposition profile,
// otherwise the global planning backend. It does not take m.mu, so callers
// that already hold it (Status) can use it.
func (m *Manager) pairingDecomposeRefs(cfg Config) map[string][]string {
	refs := map[string][]string{}
	for _, p := range m.store.ListPRDs() {
		if prdFinishedForPairing(p.Status) {
			continue
		}
		b := p.DecompositionProfile
		if b == "" {
			b = cfg.PlanningBackend
		}
		if b == "" {
			continue
		}
		refs[b] = append(refs[b], p.ID)
	}
	return refs
}

// PairingWarnings returns the current pairing warnings for the live config.
func (m *Manager) PairingWarnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return PairingWarnings(m.cfg, m.pairingDecomposeRefs(m.cfg))
}

// ReportPairingWarnings returns the current pairing warnings and writes each
// one to the daemon log the first time this process sees it. Startup and
// every autonomous config set call it, so a warning is logged once and then
// stays visible through the returned value and the status endpoint.
func (m *Manager) ReportPairingWarnings() []string {
	ws := m.PairingWarnings()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pairingLogged == nil {
		m.pairingLogged = map[string]bool{}
	}
	for _, w := range ws {
		if m.pairingLogged[w] {
			continue
		}
		m.pairingLogged[w] = true
		log.Printf("[autonomous] config warning: %s", w)
	}
	return ws
}
