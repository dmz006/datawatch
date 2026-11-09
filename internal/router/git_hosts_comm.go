// Package router — comm-channel surface for the configured git hosts
// (git.hosts, internal/config/git_hosts.go). Handles two verbs under the
// existing `configure` command:
//
//	configure git.hosts list
//	    List every configured git host with its kind and credential source.
//	    A vault-sourced host shows the vault ENTRY NAME (vault_secret) — the
//	    secret value is never held by the daemon config, so it can never be
//	    printed here.
//	configure git.hosts.<name>.<field>=<value>
//	    Set one field of an existing host (kind, base_url, credential_source,
//	    vault_secret, sign_tags, signing_key). The write goes through the
//	    daemon's configure surface (PUT /api/config), which applies the whole
//	    git.hosts list atomically: a value that would leave the host list
//	    inconsistent is rejected as a whole and the config file is left
//	    unchanged.
//
// The verbs never print a credential: at most the vault entry name, never a
// token value.
package router

import (
	"encoding/json"
	"fmt"
	"strings"
)

// gitHostsCommLabel tags every response this verb sends, matching the
// `[<hostname>] <label>` reply convention.
const gitHostsCommLabel = "git-hosts"

// gitHostsPath is the list endpoint behind `configure git.hosts list`
// (internal/server/git_hosts_api.go).
const gitHostsPath = "/api/git/hosts"

// gitHostsPrefix fronts every dotted per-field key handled here.
const gitHostsPrefix = "git.hosts."

// GitHostsCommVerbs returns the comm verbs this file installs.
func GitHostsCommVerbs() []string { return []string{"git-hosts"} }

// GitHostsConfigureKeys returns the configure keys this file installs: the
// list verb and the dotted per-field key.
func GitHostsConfigureKeys() []string {
	return []string{"git.hosts list", "git.hosts.<name>.<field>=<value>"}
}

// gitHostRow is one row of GET /api/git/hosts as the comm list verb renders
// it. The row deliberately carries only display fields — kind and credential
// source (and, for a vault-sourced host, the vault entry NAME) — so there is
// no field in which a secret value could ride along into the reply.
type gitHostRow struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	CredentialSource string `json:"credential_source"`
	VaultSecret      string `json:"vault_secret"`
}

// handleGitHostsDispatch routes the `git.hosts …` configure text: a bare verb
// (nothing after `git.hosts`) or `git.hosts list` lists the hosts; anything
// else under the dotted prefix is a per-field set. Registered in
// handleConfigure (router.go) next to the other configure handlers.
func (r *Router) handleGitHostsDispatch(text string) {
	rest := strings.TrimSpace(text)
	if rest == "" || rest == "list" || rest == "git.hosts" || rest == "git.hosts list" {
		r.handleGitHostsList()
		return
	}
	r.handleGitHostsSet(rest)
}

// replyGitHosts sends a tagged response like every other comm verb.
func (r *Router) replyGitHosts(body string) {
	r.reply(gitHostsCommLabel, body)
}

// handleGitHostsList lists every configured git host with its kind and
// credential source via the existing /api/git/hosts endpoint. A vault-sourced
// host shows the vault entry name; a secret value is never printed.
func (r *Router) handleGitHostsList() {
	body, err := r.commGet(gitHostsPath, nil)
	if err != nil {
		r.replyGitHosts("list failed: " + err.Error())
		return
	}
	var envelope struct {
		Hosts []gitHostRow `json:"hosts"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		r.replyGitHosts("list failed: host list is not valid JSON (" + err.Error() + ")")
		return
	}
	rows := envelope.Hosts
	if len(rows) == 0 {
		r.replyGitHosts("no git hosts configured; set one with " +
			gitHostsPrefix + "<name>.<field>=<value>")
		return
	}
	// Render one line per host: name, kind, credential source. The vault
	// entry name (never a secret value) disambiguates vault-sourced hosts.
	var sb strings.Builder
	for _, h := range rows {
		line := fmt.Sprintf("%-16s kind=%-9s credential_source=%s", h.Name, h.Kind, h.CredentialSource)
		if h.CredentialSource == "vault" && h.VaultSecret != "" {
			line += " vault_secret=" + h.VaultSecret
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	r.replyGitHosts(strings.TrimRight(sb.String(), "\n"))
}

// handleGitHostsSet handles `configure git.hosts.<name>.<field>=<value>`.
// The key is cut at the first '=' (a value such as a base_url can itself
// contain '='), then applied through the daemon's configure surface so the
// whole git.hosts list is validated and rewritten atomically. The reply
// echoes only the key — never the value — so a secret is never mirrored into
// the chat channel even when the daemon's error message quotes input.
func (r *Router) handleGitHostsSet(arg string) {
	key, value, ok := strings.Cut(arg, "=")
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if !ok || key == gitHostsPrefix || key == "" || !strings.HasPrefix(key, gitHostsPrefix) {
		r.replyGitHosts("invalid: set needs " + gitHostsPrefix + "<name>.<field>=<value>")
		return
	}
	if value == "" {
		r.replyGitHosts("invalid: " + key + " needs a value")
		return
	}
	if r.configureFn == nil {
		r.replyGitHosts("configure is not wired in this build")
		return
	}
	if err := r.configureFn(key, value); err != nil {
		r.replyGitHosts("rejected " + key + ": " + err.Error())
		return
	}
	r.replyGitHosts("configured " + key)
}
