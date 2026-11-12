package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

const (
	requirementsReportCommUsage  = "usage: automation requirements-report [text|json]"
	modelPullConsentGetCommUsage = "usage: automation model-pull-consent-get"
	modelPullConsentSetCommUsage = "usage: automation model-pull-consent-set model=<name> [model=<name> ...] [deny=<name> ...] [publisher=<p> ...] (or clear)"
	nodeVersionsCommUsage        = "usage: automation node-versions <node-name>"
)

// RequirementsSurfacesCommVerbs lists the requirements surface comm verbs,
// for the parity test.
func RequirementsSurfacesCommVerbs() []string {
	return []string{"requirements-report", "model-pull-consent-get", "model-pull-consent-set", "node-versions"}
}

// replyRequirementsReport reads the requirements report over the REST
// loopback and replies with the text the autonomous package renders, or the
// JSON body when the caller asks for json.
func (r *Router) replyRequirementsReport(args []string) {
	const label = "requirements-report"
	format := "text"
	switch len(args) {
	case 1:
	case 2:
		format = strings.ToLower(strings.TrimSpace(args[1]))
		format = strings.TrimPrefix(format, "--format=")
	default:
		r.reply(label+" failed", requirementsReportCommUsage)
		return
	}
	if format != "text" && format != "json" {
		r.reply(label+" failed", requirementsReportCommUsage)
		return
	}
	out, err := r.commGet("/api/automation/requirements-report", url.Values{"format": {"json"}})
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	if format == "json" {
		r.reply(label, out)
		return
	}
	// Decode the report the way the REST route and the CLI do, and let the
	// shared renderer draw it, so this verb cannot drift out of shape from
	// the surfaces it mirrors.
	var rep autonomous.RequirementsReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		r.reply(label+" failed", "decode report: "+err.Error())
		return
	}
	r.reply(label, strings.TrimRight(rep.Text(), "\n"))
}

// replyModelPullConsentGet reads the consent policy over the REST loopback
// and replies with a readable rendering.
func (r *Router) replyModelPullConsentGet(_ []string) {
	const label = "model-pull-consent-get"
	out, err := r.commGet("/api/automation/model-consent", nil)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, strings.TrimRight(renderConsentCommView(out), "\n"))
}

// replyModelPullConsentSet sets the consent policy over the REST loopback.
// Tokens compose the complete policy (the PUT replaces it): model=<name>
// allows, deny=<name> records a refusal with a note, publisher=<p>
// allow-lists a publisher; clear wipes the policy.
func (r *Router) replyModelPullConsentSet(args []string) {
	const label = "model-pull-consent-set"
	type entry struct {
		Name    string `json:"name"`
		Allowed bool   `json:"allowed"`
		Note    string `json:"note,omitempty"`
	}
	var models []entry
	var publishers []string
	seen := map[string]bool{}
	clear := false
	given := 0
	for _, a := range args[1:] {
		token, value, ok := strings.Cut(a, "=")
		if !ok {
			token, value = a, ""
		}
		token = strings.ToLower(strings.TrimSpace(token))
		value = strings.TrimSpace(value)
		switch token {
		case "clear":
			clear = true
		case "model":
			if value == "" || seen[strings.ToLower(value)] {
				r.reply(label+" failed", "model=<name> needs a non-empty name; "+value+" is listed twice")
				return
			}
			seen[strings.ToLower(value)] = true
			models = append(models, entry{Name: value, Allowed: true})
			given++
		case "deny":
			if value == "" || seen[strings.ToLower(value)] {
				r.reply(label+" failed", "deny=<name> needs a non-empty name; "+value+" is listed twice")
				return
			}
			seen[strings.ToLower(value)] = true
			models = append(models, entry{Name: value, Allowed: false, Note: "denied by operator"})
			given++
		case "publisher":
			if value == "" {
				r.reply(label+" failed", "publisher=<name> needs a non-empty name")
				return
			}
			publishers = append(publishers, value)
			given++
		default:
			r.reply(label+" failed", modelPullConsentSetCommUsage)
			return
		}
	}
	if clear {
		if given > 0 {
			r.reply(label+" failed", "clear cannot be combined with model=, deny= or publisher=")
			return
		}
	} else if given == 0 {
		r.reply(label+" failed", modelPullConsentSetCommUsage)
		return
	}
	body, _ := json.Marshal(map[string]any{"models": models, "publishers": publishers})
	out, err := r.commJSON(http.MethodPut, "/api/automation/model-consent", string(body))
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	if strings.TrimSpace(out) != "" {
		r.reply(label, strings.TrimSpace(out))
		return
	}
	r.reply(label, fmt.Sprintf("model pull consent set: %d model(s), %d publisher(s)", len(models), len(publishers)))
}

// renderConsentCommView renders the GET consent body for the comm reply.
func renderConsentCommView(out string) string {
	var view struct {
		Models []struct {
			Name    string `json:"name"`
			Allowed bool   `json:"allowed"`
			Note    string `json:"note"`
		} `json:"models"`
		Publishers   []string `json:"publishers"`
		DecisionRule string   `json:"decision_rule"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		return out
	}
	var sb strings.Builder
	sb.WriteString("Model pull consent\n")
	sb.WriteString("  rule: " + view.DecisionRule + "\n")
	if len(view.Models) == 0 {
		sb.WriteString("  models: (none listed)\n")
	} else {
		sb.WriteString("  models:\n")
		for _, m := range view.Models {
			state := "allowed"
			if !m.Allowed {
				state = "refused"
			}
			line := "    " + m.Name + " " + state
			if m.Note != "" {
				line += " - " + m.Note
			}
			sb.WriteString(line + "\n")
		}
	}
	if len(view.Publishers) == 0 {
		sb.WriteString("  publishers: (none allow-listed)\n")
	} else {
		sb.WriteString("  publishers: " + strings.Join(view.Publishers, ", ") + "\n")
	}
	return sb.String()
}

// replyNodeBackendVersions reads one node's reported backend version and
// per-capability minimums over the REST loopback. args[0] is the verb,
// args[1] the node name.
func (r *Router) replyNodeBackendVersions(args []string) {
	const label = "node-versions"
	if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
		r.reply(label+" failed", nodeVersionsCommUsage)
		return
	}
	name := strings.TrimSpace(args[1])
	out, err := r.commGet("/api/compute/nodes/"+url.PathEscape(name)+"/versions", nil)
	if err != nil {
		r.reply(label+" failed", err.Error())
		return
	}
	r.reply(label, strings.TrimRight(renderNodeVersionsCommView(out), "\n"))
}

// renderNodeVersionsCommView renders the node-versions GET body for the
// comm reply.
func renderNodeVersionsCommView(out string) string {
	var view struct {
		Name            string `json:"name"`
		Kind            string `json:"kind"`
		Address         string `json:"address"`
		Backend         string `json:"backend"`
		BackendVersion  string `json:"backend_version"`
		VersionProbedAt string `json:"version_probed_at"`
		Capabilities    []struct {
			Capability string `json:"capability"`
			Minimum    string `json:"minimum"`
			OK         *bool  `json:"ok"`
			Unknown    bool   `json:"unknown"`
			Reason     string `json:"reason"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		return out
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Node %s (kind %s, %s)\n", view.Name, view.Kind, view.Address)
	if view.Backend != "" {
		line := "  backend: " + view.Backend + " " + view.BackendVersion
		if view.VersionProbedAt != "" {
			line += " (recorded " + view.VersionProbedAt + ")"
		}
		sb.WriteString(line + "\n")
	} else {
		sb.WriteString("  backend: no version recorded\n")
	}
	if len(view.Capabilities) == 0 {
		sb.WriteString("  no capability minimums apply\n")
		return sb.String()
	}
	for _, c := range view.Capabilities {
		if c.Unknown {
			fmt.Fprintf(&sb, "  %s: version unknown (requirement %s) - unknown versions fail closed%s\n", c.Capability, c.Minimum, commReasonTail(c.Reason))
			continue
		}
		state := "fails the minimum"
		if c.OK != nil && *c.OK {
			state = "meets the minimum"
		}
		fmt.Fprintf(&sb, "  %s: requirement %s - %s%s\n", c.Capability, c.Minimum, state, commReasonTail(c.Reason))
	}
	return sb.String()
}

func commReasonTail(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return ""
	}
	return " - " + strings.TrimSpace(reason)
}
