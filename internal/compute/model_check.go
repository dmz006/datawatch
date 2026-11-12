package compute

import (
	"fmt"
	"strings"
)

// Verdict states for one node in a model check.
const (
	VerdictServes     = "serves"
	VerdictNotServing = "not_serving"
	VerdictUnknown    = "unknown"
)

// UnknownNodeAssumeNot is the unknown_node_policy value under which a node
// whose models cannot be read counts as not serving. Any other value is
// treated as the default, assume_serves.
const UnknownNodeAssumeNot = "assume_not"

// NodeModelName returns the model name as a node knows it: the text after the
// first slash, which is the provider prefix. A name with no slash is returned
// unchanged.
func NodeModelName(model string) string {
	if i := strings.Index(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}

// withLatestTag adds :latest to a name that has no tag.
func withLatestTag(name string) string {
	name = strings.TrimSpace(name)
	last := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		last = name[i+1:]
	}
	if strings.Contains(last, ":") {
		return name
	}
	return name + ":latest"
}

// MatchesModel reports whether a node's served model name is the requested
// model. The provider prefix of the requested model is dropped, and a name
// with no tag equals the same name with :latest. A served name that equals
// the whole requested model also matches, for models whose own name holds a
// slash.
func MatchesModel(servedName, model string) bool {
	served := withLatestTag(servedName)
	if strings.TrimSpace(servedName) == "" || strings.TrimSpace(model) == "" {
		return false
	}
	return served == withLatestTag(NodeModelName(model)) || served == withLatestTag(model)
}

// NodeVerdict is the answer for one node. State is serves, not_serving or
// unknown (the models could not be read).
type NodeVerdict struct {
	Node   string `json:"node"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ModelCheckResult is the outcome of CheckModel. Served lists the nodes that
// serve the model, or are assumed to under the unknown-node policy. Refused is
// true when none does. NodeUnknown is true when a pinned node is not among the
// registered nodes.
type ModelCheckResult struct {
	Model       string        `json:"model"`
	PinnedNode  string        `json:"pinned_node,omitempty"`
	Served      []string      `json:"served"`
	Checked     []NodeVerdict `json:"checked"`
	Refused     bool          `json:"refused"`
	NodeUnknown bool          `json:"node_unknown"`
}

func modelCount(n int) string {
	if n == 1 {
		return "1 model"
	}
	return fmt.Sprintf("%d models", n)
}

func verdictFor(model string, s ServedModels) NodeVerdict {
	v := NodeVerdict{Node: s.Node}
	if !s.Known {
		v.State = VerdictUnknown
		reason := strings.TrimSpace(s.Error)
		if reason == "" {
			reason = "no model list"
		}
		v.Reason = "could not read models: " + reason
		return v
	}
	for _, name := range s.Models {
		if MatchesModel(name, model) {
			v.State = VerdictServes
			v.Reason = "serves " + name
			return v
		}
	}
	v.State = VerdictNotServing
	v.Reason = fmt.Sprintf("serves %s, none is %s", modelCount(len(s.Models)), model)
	return v
}

// CheckModel decides whether any registered node serves model. With no
// pinnedNode it is refused when no node in served is a serves verdict; a node
// whose models could not be read counts as serving under assume_serves and as
// not serving under assume_not. With a pinnedNode only that node is looked at,
// and a pinnedNode absent from served sets NodeUnknown and Refused.
func CheckModel(model, pinnedNode string, served []ServedModels, unknownPolicy string) ModelCheckResult {
	res := ModelCheckResult{Model: model, PinnedNode: pinnedNode, Served: []string{}, Checked: []NodeVerdict{}}
	counts := func(v NodeVerdict) bool {
		switch v.State {
		case VerdictServes:
			return true
		case VerdictUnknown:
			return unknownPolicy != UnknownNodeAssumeNot
		}
		return false
	}
	for _, s := range served {
		if pinnedNode != "" && s.Node != pinnedNode {
			continue
		}
		v := verdictFor(model, s)
		res.Checked = append(res.Checked, v)
		if counts(v) {
			res.Served = append(res.Served, v.Node)
		}
	}
	if pinnedNode != "" && len(res.Checked) == 0 {
		res.NodeUnknown = true
	}
	res.Refused = len(res.Served) == 0
	return res
}

// NoNodeServesModelError is returned when a model check is refused.
type NoNodeServesModelError struct {
	Result ModelCheckResult
}

func (e *NoNodeServesModelError) Error() string {
	r := e.Result
	if r.PinnedNode != "" {
		if r.NodeUnknown || len(r.Checked) == 0 {
			return fmt.Sprintf("node %q does not serve model %q: it is not a registered node", r.PinnedNode, r.Model)
		}
		return fmt.Sprintf("node %q does not serve model %q (%s)", r.PinnedNode, r.Model, r.Checked[0].Reason)
	}
	if len(r.Checked) == 0 {
		return fmt.Sprintf("no registered node serves model %q; no nodes are registered", r.Model)
	}
	parts := make([]string, 0, len(r.Checked))
	for _, v := range r.Checked {
		parts = append(parts, fmt.Sprintf("%s (%s)", v.Node, v.Reason))
	}
	return fmt.Sprintf("no registered node serves model %q; nodes checked: %s", r.Model, strings.Join(parts, ", "))
}
