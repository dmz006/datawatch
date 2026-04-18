// F10 sprint 2 S2.7 — `profile` command handler for comm channels
// (signal, telegram, discord, slack, matrix, webhooks).
//
// Only read-only operations are exposed over chat: list, show, smoke.
// Create / update / delete require structured input that doesn't fit
// a chat-line UX, and the blast radius of mis-typed profile changes
// from an untrusted chat is too large. Those paths stay on the
// API / MCP / CLI / UI surfaces where we have more validation.

package router

import (
	"fmt"
	"strings"
)

// handleProfile dispatches a parsed CmdProfile command.
func (r *Router) handleProfile(cmd Command) {
	// Any pre-parse error carried in Text → show help + the error
	if cmd.ProfileKind == "" || cmd.ProfileVerb == "" {
		r.send(profileHelpText(r.hostname, cmd.Text))
		return
	}
	switch cmd.ProfileVerb {
	case ProfileVerbList:
		r.profileList(cmd.ProfileKind)
	case ProfileVerbShow:
		r.profileShow(cmd.ProfileKind, cmd.ProfileName)
	case ProfileVerbSmoke:
		r.profileSmoke(cmd.ProfileKind, cmd.ProfileName)
	default:
		r.send(profileHelpText(r.hostname, "unknown verb: "+cmd.ProfileVerb))
	}
}

func (r *Router) profileList(kind string) {
	lines := []string{fmt.Sprintf("[%s] %s profiles:", r.hostname, kind)}
	switch kind {
	case ProfileKindProject:
		if r.projectStore == nil {
			r.send(fmt.Sprintf("[%s] project profile store not configured", r.hostname))
			return
		}
		ps := r.projectStore.List()
		if len(ps) == 0 {
			lines = append(lines, "  (none)")
		}
		for _, p := range ps {
			sidecar := p.ImagePair.Sidecar
			if sidecar == "" {
				sidecar = "(solo)"
			}
			lines = append(lines,
				fmt.Sprintf("  %-20s %s + %s  [%s]",
					p.Name, p.ImagePair.Agent, sidecar, p.Git.URL))
		}
	case ProfileKindCluster:
		if r.clusterStore == nil {
			r.send(fmt.Sprintf("[%s] cluster profile store not configured", r.hostname))
			return
		}
		cs := r.clusterStore.List()
		if len(cs) == 0 {
			lines = append(lines, "  (none)")
		}
		for _, c := range cs {
			lines = append(lines, fmt.Sprintf("  %-20s kind=%s context=%s ns=%s",
				c.Name, c.Kind, c.Context, c.EffectiveNamespace()))
		}
	default:
		r.send(profileHelpText(r.hostname, "unknown kind: "+kind))
		return
	}
	r.send(strings.Join(lines, "\n"))
}

func (r *Router) profileShow(kind, name string) {
	if name == "" {
		r.send(fmt.Sprintf("[%s] profile %s show: name required", r.hostname, kind))
		return
	}
	switch kind {
	case ProfileKindProject:
		if r.projectStore == nil {
			r.send(fmt.Sprintf("[%s] project profile store not configured", r.hostname))
			return
		}
		p, err := r.projectStore.Get(name)
		if err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
			return
		}
		sidecar := p.ImagePair.Sidecar
		if sidecar == "" {
			sidecar = "(solo)"
		}
		lines := []string{
			fmt.Sprintf("[%s] project profile: %s", r.hostname, p.Name),
			fmt.Sprintf("  description: %s", strDefault(p.Description, "(none)")),
			fmt.Sprintf("  git: %s  branch=%s  provider=%s",
				p.Git.URL, strDefault(p.Git.Branch, "default"),
				strDefault(p.Git.Provider, "-")),
			fmt.Sprintf("  image: %s + %s", p.ImagePair.Agent, sidecar),
			fmt.Sprintf("  memory: mode=%s ns=%s",
				strDefault(string(p.Memory.Mode), "sync-back"),
				p.EffectiveNamespace()),
			fmt.Sprintf("  idle_timeout: %s", p.IdleTimeout),
			fmt.Sprintf("  spawn: allow=%v budget_total=%d per_min=%d",
				p.AllowSpawnChildren, p.SpawnBudgetTotal, p.SpawnBudgetPerMinute),
		}
		if len(p.PostTaskHooks) > 0 {
			lines = append(lines, fmt.Sprintf("  post_task_hooks: %d", len(p.PostTaskHooks)))
		}
		r.send(strings.Join(lines, "\n"))
	case ProfileKindCluster:
		if r.clusterStore == nil {
			r.send(fmt.Sprintf("[%s] cluster profile store not configured", r.hostname))
			return
		}
		c, err := r.clusterStore.Get(name)
		if err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
			return
		}
		lines := []string{
			fmt.Sprintf("[%s] cluster profile: %s", r.hostname, c.Name),
			fmt.Sprintf("  description: %s", strDefault(c.Description, "(none)")),
			fmt.Sprintf("  kind: %s", c.Kind),
			fmt.Sprintf("  context: %s", strDefault(c.Context, "-")),
			fmt.Sprintf("  endpoint: %s", strDefault(c.Endpoint, "-")),
			fmt.Sprintf("  namespace: %s", c.EffectiveNamespace()),
			fmt.Sprintf("  registry: %s", strDefault(c.ImageRegistry, "-")),
			fmt.Sprintf("  trusted_cas: %d", len(c.TrustedCAs)),
		}
		if c.CredsRef.Provider != "" {
			lines = append(lines, fmt.Sprintf("  creds: %s:%s", c.CredsRef.Provider, c.CredsRef.Key))
		}
		r.send(strings.Join(lines, "\n"))
	default:
		r.send(profileHelpText(r.hostname, "unknown kind: "+kind))
	}
}

func (r *Router) profileSmoke(kind, name string) {
	if name == "" {
		r.send(fmt.Sprintf("[%s] profile %s smoke: name required", r.hostname, kind))
		return
	}
	var (
		lines  []string
		passed bool
		warns  []string
	)
	switch kind {
	case ProfileKindProject:
		if r.projectStore == nil {
			r.send(fmt.Sprintf("[%s] project profile store not configured", r.hostname))
			return
		}
		res, err := r.projectStore.Smoke(name)
		if err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
			return
		}
		passed = res.Passed()
		lines = append(lines, fmt.Sprintf("[%s] smoke project/%s: %s",
			r.hostname, name, passOrFail(passed)))
		for _, c := range res.Checks {
			lines = append(lines, "  ✓ "+c)
		}
		for _, e := range res.Errors {
			lines = append(lines, "  ✗ "+e)
		}
		warns = res.Warnings
	case ProfileKindCluster:
		if r.clusterStore == nil {
			r.send(fmt.Sprintf("[%s] cluster profile store not configured", r.hostname))
			return
		}
		res, err := r.clusterStore.Smoke(name)
		if err != nil {
			r.send(fmt.Sprintf("[%s] %v", r.hostname, err))
			return
		}
		passed = res.Passed()
		lines = append(lines, fmt.Sprintf("[%s] smoke cluster/%s: %s",
			r.hostname, name, passOrFail(passed)))
		for _, c := range res.Checks {
			lines = append(lines, "  ✓ "+c)
		}
		for _, e := range res.Errors {
			lines = append(lines, "  ✗ "+e)
		}
		warns = res.Warnings
	default:
		r.send(profileHelpText(r.hostname, "unknown kind: "+kind))
		return
	}
	for _, w := range warns {
		lines = append(lines, "  ⚠ "+w)
	}
	r.send(strings.Join(lines, "\n"))
}

// ── helpers ─────────────────────────────────────────────────────────────

func profileHelpText(hostname, note string) string {
	out := []string{fmt.Sprintf("[%s] profile usage:", hostname)}
	if note != "" {
		out = append(out, "  "+note)
	}
	out = append(out,
		"  profile project list",
		"  profile cluster list",
		"  profile project show <name>",
		"  profile project smoke <name>",
		"  profile cluster show <name>",
		"  profile cluster smoke <name>",
		"  (create/update/delete only via UI, API, MCP, CLI — not chat)",
	)
	return strings.Join(out, "\n")
}

func strDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func passOrFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
