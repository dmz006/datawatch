package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// CLI side of git.hosts. `datawatch config set git.hosts.<name>.<field> <value>`
// reaches the daemon through the generic config set command; the daemon applies
// the key with config.SetGitHostField (internal/config/git_hosts.go) and refuses
// a bad value with its message. This file adds what the generic command does not:
// a set that also prints the readiness result when the credential source changed,
// and a list of every configured host with its readiness.

// gitHostReadiness mirrors the readiness part of one GET /api/git/hosts row.
// It never carries a token.
type gitHostReadiness struct {
	LoggedIn   bool   `json:"logged_in"`
	Account    string `json:"account"`
	FixCommand string `json:"fix_command"`
	Problem    string `json:"problem"`
}

// gitHostRow is one row of GET /api/git/hosts.
type gitHostRow struct {
	Name             string           `json:"name"`
	Kind             string           `json:"kind"`
	BaseURL          string           `json:"base_url"`
	CredentialSource string           `json:"credential_source"`
	VaultSecret      string           `json:"vault_secret"`
	Readiness        gitHostReadiness `json:"readiness"`
}

// isGitHostKey reports whether key is a dotted git host key
// (git.hosts.<name>.<field>) that the daemon routes to config.SetGitHostField.
func isGitHostKey(key string) bool {
	rest, ok := strings.CutPrefix(key, "git.hosts.")
	if !ok {
		return false
	}
	name, field, ok := strings.Cut(rest, ".")
	return ok && name != "" && field != ""
}

// gitHostKeyParts splits git.hosts.<name>.<field>.
func gitHostKeyParts(key string) (name, field string) {
	rest := strings.TrimPrefix(key, "git.hosts.")
	name, field, _ = strings.Cut(rest, ".")
	return name, field
}

// isGitHostCredentialKey reports whether a set of key changes the credential
// source, so the readiness result is worth printing afterwards.
func isGitHostCredentialKey(key string) bool {
	if !isGitHostKey(key) {
		return false
	}
	_, field := gitHostKeyParts(key)
	return field == "credential_source" || field == "vault_secret"
}

// gitHostConfigBody builds the PUT /api/config body for one dotted key.
// sign_tags is sent as a JSON boolean; every other field as a string.
func gitHostConfigBody(key, value string) ([]byte, error) {
	_, field := gitHostKeyParts(key)
	var v any = value
	if field == "sign_tags" {
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("%s must be true or false, got %q", key, value)
		}
		v = b
	}
	return json.Marshal(map[string]any{key: v})
}

// runGitHostsConfigSet sets git.hosts.<name>.<field> through the daemon. After
// a set of the credential source (credential_source or vault_secret) it prints
// the readiness result for that host.
func runGitHostsConfigSet(out io.Writer, baseURL, token, key, value string) error {
	if !isGitHostKey(key) {
		return fmt.Errorf("config set: %q is not a git host key (want git.hosts.<name>.<field>)", key)
	}
	body, err := gitHostConfigBody(key, value)
	if err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	if err := putConfig(baseURL+"/api/config", token, string(body)); err != nil {
		return fmt.Errorf("config set: %w", err)
	}
	fmt.Fprintf(out, "Set %s = %s\n", key, value)
	if !isGitHostCredentialKey(key) {
		return nil
	}
	name, _ := gitHostKeyParts(key)
	rows, err := fetchGitHosts(baseURL, token)
	if err != nil {
		return fmt.Errorf("set applied, readiness check failed: %w", err)
	}
	for _, r := range rows {
		if r.Name == name {
			printGitHostRow(out, r)
			return nil
		}
	}
	return fmt.Errorf("set applied, but host %q is not in GET /api/git/hosts", name)
}

// runGitHostsList prints every configured git host with its readiness.
func runGitHostsList(out io.Writer, baseURL, token string) error {
	rows, err := fetchGitHosts(baseURL, token)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(out, "No git hosts configured. Add one with PUT /api/config (git.hosts).")
		return nil
	}
	for _, r := range rows {
		printGitHostRow(out, r)
	}
	return nil
}

// fetchGitHosts reads GET /api/git/hosts.
func fetchGitHosts(baseURL, token string) ([]gitHostRow, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/git/hosts", nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("git hosts: HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Hosts []gitHostRow `json:"hosts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode git hosts: %w", err)
	}
	return doc.Hosts, nil
}

// printGitHostRow writes one host and its readiness in plain language.
func printGitHostRow(out io.Writer, r gitHostRow) {
	src := r.CredentialSource
	if src == "" {
		src = config.GitCredentialHostLogin
	}
	line := fmt.Sprintf("%s (%s) credential_source=%s", r.Name, r.Kind, src)
	if r.VaultSecret != "" {
		line += " vault_secret=" + r.VaultSecret
	}
	if r.BaseURL != "" {
		if u, err := url.Parse(r.BaseURL); err == nil && u.Host != "" {
			line += " base_url=" + u.Host
		}
	}
	fmt.Fprintln(out, line)
	if r.Readiness.LoggedIn {
		acct := r.Readiness.Account
		if acct == "" {
			acct = "unknown account"
		}
		fmt.Fprintf(out, "  ready: yes (%s)\n", acct)
		return
	}
	fmt.Fprintln(out, "  ready: no")
	if r.Readiness.Problem != "" {
		fmt.Fprintf(out, "  problem: %s\n", r.Readiness.Problem)
	}
	if r.Readiness.FixCommand != "" {
		fmt.Fprintf(out, "  fix: %s\n", r.Readiness.FixCommand)
	}
}
