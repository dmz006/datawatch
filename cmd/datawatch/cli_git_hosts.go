package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/spf13/cobra"
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

// newGitHostsListCmd is `datawatch git-hosts`: the operator-facing list of
// every configured git host with its readiness, read from the daemon's
// GET /api/git/hosts (the daemon is the source of truth for the applied
// config; readiness is computed there and never carries a token).
func newGitHostsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "git-hosts",
		Short: "List configured git hosts with their readiness",
		Long: "Reads GET /api/git/hosts from the running daemon and prints every git host\n" +
			"with its credential source and readiness result (ready account, or the\n" +
			"problem and the fix command). Set one with `datawatch config set\n" +
			"git.hosts.<name>.<field> <value>`, which prints the readiness after a\n" +
			"change to the credential source.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runGitHostsListCmd(cfg)
		},
	}
}

// runGitHostsListCmd is the `datawatch git-hosts` body against one daemon:
// it lists every configured host with its readiness on out (os.Stdout).
func runGitHostsListCmd(cfg *config.Config) error {
	return runGitHostsList(os.Stdout, loopbackBaseURL(cfg), cfg.Server.Token)
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

// runGitHostsStats prints the git host counters from GET /api/git/hosts/stats.
func runGitHostsStats(out io.Writer, baseURL, token string) error {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/git/hosts/stats", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("git host stats: HTTP %d", resp.StatusCode)
	}
	var st struct {
		Configured    int            `json:"configured"`
		WindowSeconds int64          `json:"window_seconds"`
		Operations    map[string]int `json:"operations"`
		Failed        int            `json:"failed"`
		LastSuccessAt string         `json:"last_success_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return fmt.Errorf("decode git host stats: %w", err)
	}
	fmt.Fprintf(out, "configured hosts: %d\nwindow: %ds\nfailed: %d\n", st.Configured, st.WindowSeconds, st.Failed)
	last := st.LastSuccessAt
	if last == "" || strings.HasPrefix(last, "0001-") {
		last = "never"
	}
	fmt.Fprintf(out, "last success: %s\n", last)
	kinds := make([]string, 0, len(st.Operations))
	for k := range st.Operations {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(out, "  %s: %d\n", k, st.Operations[k])
	}
	return nil
}

// newGitCmd is `datawatch git hosts stats`: a read of the git host counters.
func newGitCmd() *cobra.Command {
	git := &cobra.Command{Use: "git", Short: "Git host commands"}
	hosts := &cobra.Command{Use: "hosts", Short: "Git host reads"}
	hosts.AddCommand(&cobra.Command{
		Use:   "stats",
		Short: "Show the git host operation counters",
		Long:  "Reads GET /api/git/hosts/stats from the running daemon: configured hosts, operations by kind over the window, failures and the last success.",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return runGitHostsStats(os.Stdout, loopbackBaseURL(cfg), cfg.Server.Token)
		},
	})
	git.AddCommand(hosts)
	return git
}
