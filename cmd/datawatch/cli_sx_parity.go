// Sprint Sx (v3.7.2) — CLI subcommand parity for v3.5.0–v3.7.0
// REST endpoints. Each command is a thin REST proxy: locates the
// running daemon's port from the active config, calls the endpoint,
// pretty-prints the JSON response.

package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// daemonURL returns the base URL for the running local daemon, honoring
// TLS settings (https://127.0.0.1:<tls_port> when tls_enabled, else
// http://127.0.0.1:<port>). Falls back to http://127.0.0.1:8080 when no
// config is loadable.
// When --url/-u is given, that value takes priority over config.
func daemonURL() string {
	if serverURL != "" {
		return strings.TrimRight(serverURL, "/")
	}
	cfg, err := loadConfigSecure()
	if err != nil || cfg == nil {
		return "http://127.0.0.1:8080"
	}
	if cfg.Server.TLSEnabled {
		port := cfg.Server.TLSPort
		if port == 0 {
			port = 8443
		}
		return fmt.Sprintf("https://127.0.0.1:%d", port)
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}

// daemonClient returns an *http.Client configured for the local daemon.
// When TLS is enabled, it loads the daemon's own cert (auto-generated or
// operator-supplied) and trusts it as a root CA — so localhost CLI calls
// verify properly without InsecureSkipVerify. If the cert file isn't
// readable, falls back to InsecureSkipVerify so the CLI keeps working
// (with a stderr warning).
//
// Fix BL: "datawatch should trust its own cert" (operator 2026-05-07) —
// before this, every CLI subcommand against an HTTPS-enabled daemon
// failed with x509: unknown authority.
func daemonClient() *http.Client {
	c := &http.Client{Timeout: 60 * time.Second}
	cfg, err := loadConfigSecure()
	if err != nil || cfg == nil || !cfg.Server.TLSEnabled {
		return c
	}
	certPath := cfg.Server.TLSCert
	if certPath == "" {
		// Auto-generated default path. ensureSelfSigned uses
		// <DataDir>/tls/<Name>/cert.pem and the main daemon names this
		// "server" — see internal/tlsutil/tls.go ensureSelfSigned + the
		// Build call in cmd/datawatch/main.go.
		dataDir := cfg.DataDir
		if dataDir == "" {
			home, _ := os.UserHomeDir()
			dataDir = filepath.Join(home, ".datawatch")
		}
		certPath = filepath.Join(dataDir, "tls", "server", "cert.pem")
	}
	pem, err := os.ReadFile(certPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[warn] cannot read daemon cert at %s: %v — falling back to InsecureSkipVerify\n", certPath, err)
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}} // #nosec G402 -- localhost fallback when own cert unreadable
		return c
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		fmt.Fprintf(os.Stderr, "[warn] daemon cert at %s did not parse — falling back to InsecureSkipVerify\n", certPath)
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13}} // #nosec G402 -- localhost fallback when own cert unparseable
		return c
	}
	c.Transport = &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    pool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS13,
	}}
	return c
}

// daemonToken returns the Bearer token to use for daemon API calls.
// Prefers DATAWATCH_TOKEN env var; falls back to server.token from config.
func daemonToken() string {
	if tok := os.Getenv("DATAWATCH_TOKEN"); tok != "" {
		return tok
	}
	cfg, err := loadConfigSecure()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.Server.Token
}

// daemonGet calls GET <daemonURL><path> and prints the response body.
// Returns an error if the daemon isn't reachable or returns non-2xx.
func daemonGet(path string) error {
	req, err := http.NewRequest(http.MethodGet, daemonURL()+path, nil)
	if err != nil {
		return err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	prettyPrint(body)
	return nil
}

// daemonJSON sends method+body to <daemonURL><path>.
func daemonJSON(method, path string, body any) error {
	client := daemonClient()
	var rdr io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, daemonURL()+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	if len(respBody) > 0 {
		prettyPrint(respBody)
	} else {
		fmt.Println("ok")
	}
	return nil
}

// prettyPrint emits indented JSON when body parses as JSON, raw otherwise.
func prettyPrint(body []byte) {
	var v any
	if err := json.Unmarshal(body, &v); err == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return
	}
	_, _ = os.Stdout.Write(body)
}

// ----- BL34 ask ------------------------------------------------------------

func newAskCmd() *cobra.Command {
	var backend, model string
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Single-shot LLM ask — no session, no tmux",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := joinArgs(args)
			return daemonJSON(http.MethodPost, "/api/ask", map[string]any{
				"question": question, "backend": backend, "model": model,
			})
		},
	}
	cmd.Flags().StringVar(&backend, "backend", "ollama", "ollama or openwebui")
	cmd.Flags().StringVar(&model, "model", "", "Override model")
	return cmd
}

// ----- BL35 project summary -----------------------------------------------

func newProjectSummaryCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "project-summary",
		Short: "Project overview: git status + sessions + stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				dir, _ = os.Getwd()
			}
			return daemonGet("/api/project/summary?dir=" + dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Absolute project dir (default: current dir)")
	return cmd
}

// ----- BL5 templates -------------------------------------------------------

func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Manage session-start templates",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List templates",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/templates") },
		},
		&cobra.Command{
			Use:   "get <name>",
			Short: "Show one template",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/templates/" + args[0])
			},
		},
		newTemplateUpsertCmd(),
		&cobra.Command{
			Use:   "delete <name>",
			Short: "Delete a template by name",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/templates/"+args[0], nil)
			},
		},
	)
	return cmd
}

func newTemplateUpsertCmd() *cobra.Command {
	var projectDir, backend, effort, profile, description string
	cmd := &cobra.Command{
		Use:   "upsert <name>",
		Short: "Create or update a template",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/templates", map[string]any{
				"name": args[0], "project_dir": projectDir, "backend": backend,
				"effort": effort, "profile": profile, "description": description,
			})
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Default project_dir")
	cmd.Flags().StringVar(&backend, "backend", "", "Default backend")
	cmd.Flags().StringVar(&effort, "effort", "", "quick / normal / thorough")
	cmd.Flags().StringVar(&profile, "profile", "", "Profile to use")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	return cmd
}

// ----- BL27 projects -------------------------------------------------------

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage project aliases",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List project aliases",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/projects") },
		},
		&cobra.Command{
			Use:   "get <name>",
			Short: "Show one project alias",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonGet("/api/projects/" + args[0])
			},
		},
		newProjectsUpsertCmd(),
		&cobra.Command{
			Use:   "delete <name>",
			Short: "Delete a project alias (does not touch the directory)",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/projects/"+args[0], nil)
			},
		},
	)
	return cmd
}

func newProjectsUpsertCmd() *cobra.Command {
	var dir, backend, description string
	cmd := &cobra.Command{
		Use:   "upsert <name>",
		Short: "Register or update a project alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/projects", map[string]any{
				"name": args[0], "dir": dir, "default_backend": backend,
				"description": description,
			})
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Absolute directory (required)")
	cmd.Flags().StringVar(&backend, "backend", "", "Default LLM backend")
	cmd.Flags().StringVar(&description, "description", "", "Description")
	return cmd
}

// ----- BL29 rollback -------------------------------------------------------

func newRollbackCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rollback <session-id>",
		Short: "Roll back a session's project_dir to the pre-session checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/sessions/"+args[0]+"/rollback",
				map[string]any{"force": force})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Discard uncommitted changes")
	return cmd
}

// ----- BL30 cooldown -------------------------------------------------------

func newCooldownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cooldown",
		Short: "Manage global rate-limit cooldown",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show current cooldown state",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/cooldown") },
		},
		newCooldownSetCmd(),
		&cobra.Command{
			Use:   "clear",
			Short: "Clear active cooldown",
			RunE: func(*cobra.Command, []string) error {
				return daemonJSON(http.MethodDelete, "/api/cooldown", nil)
			},
		},
	)
	return cmd
}

func newCooldownSetCmd() *cobra.Command {
	var seconds int
	var reason string
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Activate cooldown for the next N seconds",
		RunE: func(*cobra.Command, []string) error {
			if seconds <= 0 {
				return fmt.Errorf("--seconds must be > 0")
			}
			until := time.Now().Add(time.Duration(seconds) * time.Second).UnixMilli()
			return daemonJSON(http.MethodPost, "/api/cooldown",
				map[string]any{"until_unix_ms": until, "reason": reason})
		},
	}
	cmd.Flags().IntVar(&seconds, "seconds", 0, "Cooldown duration in seconds")
	cmd.Flags().StringVar(&reason, "reason", "", "Operator-readable reason")
	return cmd
}

// ----- BL40 stale ----------------------------------------------------------

func newStaleCmd() *cobra.Command {
	var seconds int
	cmd := &cobra.Command{
		Use:   "stale",
		Short: "List running sessions stuck longer than threshold",
		RunE: func(*cobra.Command, []string) error {
			path := "/api/sessions/stale"
			if seconds > 0 {
				path += "?seconds=" + strconv.Itoa(seconds)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().IntVar(&seconds, "seconds", 0, "Override stale threshold in seconds")
	return cmd
}

// ----- BL6 cost ------------------------------------------------------------

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Token + USD cost rollup",
	}
	var session string
	summary := &cobra.Command{
		Use:   "summary",
		Short: "Aggregate or per-session token + cost",
		RunE: func(*cobra.Command, []string) error {
			path := "/api/cost"
			if session != "" {
				path += "?session=" + session
			}
			return daemonGet(path)
		},
	}
	summary.Flags().StringVar(&session, "session", "", "full_id for per-session breakdown")
	cmd.AddCommand(summary, newCostUsageCmd(), newCostRatesCmd())
	return cmd
}

func newCostUsageCmd() *cobra.Command {
	var sess string
	var in, out int
	var inPerK, outPerK float64
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Add usage to a session's running counters",
		RunE: func(*cobra.Command, []string) error {
			if sess == "" {
				return fmt.Errorf("--session required")
			}
			return daemonJSON(http.MethodPost, "/api/cost/usage", map[string]any{
				"session": sess, "tokens_in": in, "tokens_out": out,
				"in_per_k": inPerK, "out_per_k": outPerK,
			})
		},
	}
	cmd.Flags().StringVar(&sess, "session", "", "Session full_id")
	cmd.Flags().IntVar(&in, "in", 0, "Input tokens")
	cmd.Flags().IntVar(&out, "out", 0, "Output tokens")
	cmd.Flags().Float64Var(&inPerK, "in-per-k", 0, "USD per 1K input (override)")
	cmd.Flags().Float64Var(&outPerK, "out-per-k", 0, "USD per 1K output (override)")
	return cmd
}

func newCostRatesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rates",
		Short: "Show effective per-backend rate table",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/cost/rates") },
	}
}

// ----- BL9 audit -----------------------------------------------------------

func newAuditCmd() *cobra.Command {
	var actor, action, sessID, since, until string
	var limit int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query operator audit log",
		RunE: func(*cobra.Command, []string) error {
			path := "/api/audit?"
			add := func(k, v string) {
				if v != "" {
					path += k + "=" + v + "&"
				}
			}
			add("actor", actor)
			add("action", action)
			add("session_id", sessID)
			add("since", since)
			add("until", until)
			if limit > 0 {
				path += "limit=" + strconv.Itoa(limit)
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "", "Filter by actor")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action")
	cmd.Flags().StringVar(&sessID, "session-id", "", "Filter by session")
	cmd.Flags().StringVar(&since, "since", "", "RFC3339 lower bound")
	cmd.Flags().StringVar(&until, "until", "", "RFC3339 upper bound")
	cmd.Flags().IntVar(&limit, "limit", 100, "Max entries")
	return cmd
}

// ----- S4 (v3.8.0) ---------------------------------------------------------

// BL42 assist
func newAssistCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "assist <question>",
		Short: "Quick-response assistant — uses configured assistant backend",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/assist",
				map[string]any{"question": joinArgs(args)})
		},
	}
}

// BL31 device aliases
func newDeviceAliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device-alias",
		Short: "Manage device aliases for `new: @<alias>:` routing",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List device aliases",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/device-aliases") },
		},
		newDeviceAliasUpsertCmd(),
		&cobra.Command{
			Use:   "delete <alias>",
			Short: "Delete a device alias",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodDelete, "/api/device-aliases/"+args[0], nil)
			},
		},
	)
	return cmd
}

func newDeviceAliasUpsertCmd() *cobra.Command {
	var server string
	cmd := &cobra.Command{
		Use:   "upsert <alias>",
		Short: "Create or update a device alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/device-aliases",
				map[string]any{"alias": args[0], "server": server})
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Remote server name (required)")
	return cmd
}

// BL69 splash info
func newSplashInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "splash-info",
		Short: "Show splash render context (logo, tagline, version)",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/splash/info") },
	}
}

// ----- S5 (v3.9.0) ---------------------------------------------------------

// BL20 routing rules
func newRoutingRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing-rules",
		Short: "Backend auto-selection routing rules",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: "List routing rules",
			RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/routing-rules") },
		},
		&cobra.Command{
			Use:   "test <task>",
			Short: "Test which backend a task would route to",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return daemonJSON(http.MethodPost, "/api/routing-rules/test",
					map[string]any{"task": joinArgs(args)})
			},
		},
	)
	return cmd
}

// ----- helpers -------------------------------------------------------------

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
