// Read-only REST view of which models each compute node serves, and the two
// Automaton actions that pin a task or a story to a node.
//
//	GET /api/compute/served-models   what every node serves, plus an optional
//	                                 model check (?node= ?model= ?backend= ?refresh=true)
//
// The model check in the answer is produced by the same function the pin
// setters ask, so this answer and a pin-time refusal can never disagree.

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// servedModelsReadTimeout bounds one whole served-models request, so a slow
// node cannot hold the request open longer than the probes of a few nodes.
const servedModelsReadTimeout = 60 * time.Second

// SetServedModelsCache wires the cache the served-models route reads through.
func (s *Server) SetServedModelsCache(c *compute.ServedModelsCache) { s.servedModels = c }

// SetModelHostCheck wires the function that answers the route's model check.
// It is the same function the Automaton manager asks at pin time.
func (s *Server) SetModelHostCheck(fn autonomous.ModelHostCheckFn) { s.modelHostCheck = fn }

// SetServedModelsCache (HTTPServer) hands the served-models cache to the API.
func (s *HTTPServer) SetServedModelsCache(c *compute.ServedModelsCache) {
	if s.api != nil {
		s.api.SetServedModelsCache(c)
	}
}

// SetModelHostCheck (HTTPServer) hands the model check to the API.
func (s *HTTPServer) SetModelHostCheck(fn autonomous.ModelHostCheckFn) {
	if s.api != nil {
		s.api.SetModelHostCheck(fn)
	}
}

// modelRoutingConfig returns the live autonomous.model_routing block, or the
// zero block (every key at its default) when there is no config.
func (s *Server) modelRoutingConfig() config.ModelRoutingConfig {
	if s.cfg == nil {
		return config.ModelRoutingConfig{}
	}
	return s.cfg.Autonomous.ModelRouting
}

// servedModelsNode is one node in the served-models answer.
type servedModelsNode struct {
	Node       string   `json:"node"`
	Kind       string   `json:"kind"`
	Models     []string `json:"models"`
	Known      bool     `json:"known"`
	Cached     bool     `json:"cached"`
	AgeSeconds int      `json:"age_seconds"`
	FetchedAt  string   `json:"fetched_at"`
	Error      string   `json:"error"`
	// Disabled is true for a node the operator switched off. Such a node is
	// listed but never read.
	Disabled bool `json:"disabled,omitempty"`
}

// servedModelsCheck is the model check in the served-models answer.
type servedModelsCheck struct {
	Model       string                          `json:"model"`
	Applicable  bool                            `json:"applicable"`
	Refused     bool                            `json:"refused"`
	NodeUnknown bool                            `json:"node_unknown"`
	ServedBy    []string                        `json:"served_by"`
	Checked     []autonomous.ModelHostNodeState `json:"checked"`
	Message     string                          `json:"message"`
}

// handleServedModels serves GET /api/compute/served-models.
func (s *Server) handleServedModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeRead) {
		return
	}
	if s.computeReg == nil || s.servedModels == nil {
		http.Error(w, "served models are not available", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	nodeName := strings.TrimSpace(q.Get("node"))
	model := strings.TrimSpace(q.Get("model"))
	backend := strings.TrimSpace(q.Get("backend"))
	refresh := strings.EqualFold(q.Get("refresh"), "true")

	var nodes []*compute.Node
	if nodeName != "" {
		n, err := s.computeReg.Get(nodeName)
		if err != nil || n == nil {
			http.Error(w, "unknown compute node "+nodeName, http.StatusNotFound)
			return
		}
		nodes = []*compute.Node{n}
	} else {
		nodes = s.computeReg.List()
	}

	ctx, cancel := context.WithTimeout(r.Context(), servedModelsReadTimeout)
	defer cancel()

	var enabled []*compute.Node
	for _, n := range nodes {
		if !n.Disabled {
			enabled = append(enabled, n)
		}
	}
	read := map[string]compute.ServedModels{}
	for _, sm := range s.servedModels.GetAll(ctx, enabled, refresh) {
		read[sm.Node] = sm
	}

	mr := s.modelRoutingConfig()
	out := map[string]any{
		"cache_seconds": int(mr.EffectiveServedModelsCacheAge() / time.Second),
		"refusal_mode":  mr.EffectiveRefusalMode(),
	}
	list := make([]servedModelsNode, 0, len(nodes))
	for _, n := range nodes {
		entry := servedModelsNode{Node: n.Name, Kind: string(n.Kind), Models: []string{}}
		if n.Disabled {
			entry.Disabled = true
		} else if sm, ok := read[n.Name]; ok {
			entry.Models = sm.Models
			entry.Known, entry.Cached, entry.AgeSeconds = sm.Known, sm.Cached, sm.AgeSeconds
			entry.Error = sm.Error
			if !sm.FetchedAt.IsZero() {
				entry.FetchedAt = sm.FetchedAt.UTC().Format(time.RFC3339)
			}
		}
		list = append(list, entry)
	}
	out["nodes"] = list

	if model != "" && s.modelHostCheck != nil {
		v := s.modelHostCheck(ctx, backend, model, nodeName)
		check := servedModelsCheck{
			Model: model, Applicable: v.Applicable, Refused: v.Refused,
			NodeUnknown: v.NodeUnknown, ServedBy: v.ServedBy, Checked: v.Checked, Message: v.Message,
		}
		if check.ServedBy == nil {
			check.ServedBy = []string{}
		}
		if check.Checked == nil {
			check.Checked = []autonomous.ModelHostNodeState{}
		}
		if check.Message == "" && !v.Applicable {
			check.Message = "the model check does not apply: the backend does not run on a compute node"
		}
		out["check"] = check
	}
	writeJSONOK(w, out)
}

// nodePinAPI is the part of the Automaton API that pins a task or a story to a
// compute node. It is separate from AutonomousAPI so the existing fakes of
// that interface stay valid.
type nodePinAPI interface {
	SetTaskNode(prdID, taskID, node, actor string) (any, error)
	SetStoryNode(prdID, storyID, node, actor string) (any, error)
}

// nodePinErrStatus maps a node pin error to an HTTP status. A refusal (the
// model is not served, the node is not registered, the pinned node cannot be
// used) is the caller's request being refused, never a server fault.
func nodePinErrStatus(err error) int {
	switch {
	case errors.Is(err, autonomous.ErrModelNotServed),
		errors.Is(err, autonomous.ErrNodeUnknown),
		errors.Is(err, autonomous.ErrPinnedNodeUnavailable):
		return http.StatusBadRequest
	}
	return modelBackendErrStatus(err, http.StatusBadRequest)
}
