// `datawatch schedule-results` — CLI parity for the scheduled-job result
// panel (docs/plans/2026-10-09-gh204-result-panel.md, CLI surface).
//
//	datawatch schedule-results list                        → GET  /api/schedule-results
//	datawatch schedule-results history <schedule_name>     → GET  /api/schedule-results/{schedule_name}
//	datawatch schedule-results post --session <id> ...     → POST /api/sessions/{id}/result
//
// The `post` verb is written for scheduled shell jobs: when a subprocess
// session is spawned, the daemon injects DATAWATCH_SESSION_ID,
// DATAWATCH_BASE_URL and a scoped session credential (DATAWATCH_RESULT_TOKEN)
// into the child's environment, so a wrapper script can finish with a bare
//
//	datawatch schedule-results post --title "run complete" --status ok \
//	    --summary "40 rules, 2 messages actioned"
//
// and the session id + token come from the environment automatically.
// Outside a spawned session, --session must be given and auth falls back
// to the usual daemon token (DATAWATCH_TOKEN / config server.token).

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// ----- command tree ---------------------------------------------------------

// newScheduleResultsCmd returns the "schedule-results" cobra subtree.
func newScheduleResultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule-results",
		Short: "Post, list and inspect structured results from scheduled jobs",
		Long: `Work with the structured result panel for scheduled jobs.

Each command talks to the running daemon over REST:

  list                            latest result per schedule (GET /api/schedule-results)
  history <schedule_name>         full retained history for one schedule
  post --session <id> --title T   post a result for a session
                                  (POST /api/sessions/{id}/result)

Inside a spawned subprocess session, post picks up the session id from
DATAWATCH_SESSION_ID and the scoped credential from DATAWATCH_RESULT_TOKEN
(both injected by the daemon at spawn time), so no flags beyond the
payload are needed.`,
	}
	cmd.AddCommand(
		newScheduleResultsListCmd(),
		newScheduleResultsHistoryCmd(),
		newScheduleResultsPostCmd(),
	)
	return cmd
}

func newScheduleResultsListCmd() *cobra.Command {
	var rawJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the latest posted result per schedule name",
		RunE: func(*cobra.Command, []string) error {
			body, err := scheduleResultsHTTP(http.MethodGet, "/api/schedule-results", nil)
			if err != nil {
				return err
			}
			if rawJSON {
				prettyPrint(body)
				return nil
			}
			out, err := formatScheduleResultsList(body)
			if err != nil {
				prettyPrint(body)
				return nil
			}
			fmt.Print(out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&rawJSON, "json", false, "Print the raw JSON response")
	return cmd
}

func newScheduleResultsHistoryCmd() *cobra.Command {
	var rawJSON bool
	cmd := &cobra.Command{
		Use:   "history <schedule_name>",
		Short: "Show the full retained result history for one schedule name",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := scheduleResultsHTTP(http.MethodGet, scheduleResultsHistoryPath(args[0]), nil)
			if err != nil {
				return err
			}
			if rawJSON {
				prettyPrint(body)
				return nil
			}
			out, err := formatScheduleResultsHistory(body)
			if err != nil {
				prettyPrint(body)
				return nil
			}
			fmt.Print(out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&rawJSON, "json", false, "Print the raw JSON response")
	return cmd
}

// scheduleResultsPostFlags holds every `post` input (excluding the target
// session, which is not part of the payload). It is passed whole to
// buildScheduleResultsPayload so the builder stays unit-testable.
type scheduleResultsPostFlags struct {
	Title    string
	Status   string
	Summary  string
	Metrics  string
	Tables   []string
	Markdown string
	File     string
}

func newScheduleResultsPostCmd() *cobra.Command {
	var session string
	var f scheduleResultsPostFlags
	cmd := &cobra.Command{
		Use:   "post --session <id> --title <title>",
		Short: "Post a structured result for a session",
		Long: `Post a structured result for a session. A spawned subprocess session
posts for itself (session id and credential from the environment); any
other session requires a credential that may post for it.

Payload sources (later wins):
  --file <payload.json>   a full payload object as a JSON file
  --title / --status ...  individual flags, which override --file fields
  --table <json>          repeatable; each value is one table object

Every field except a title is optional (a title from --file suffices).`,
		RunE: func(*cobra.Command, []string) error {
			sessID, err := scheduleResultsPostSession(session)
			if err != nil {
				return err
			}
			payload, err := buildScheduleResultsPayload(f)
			if err != nil {
				return err
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("encode payload: %w", err)
			}
			resp, err := scheduleResultsHTTP(
				http.MethodPost,
				"/api/sessions/"+url.PathEscape(sessID)+"/result",
				body,
			)
			if err != nil {
				return err
			}
			prettyPrint(resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session id to post for (default: $DATAWATCH_SESSION_ID)")
	cmd.Flags().StringVar(&f.Title, "title", "", `result title (required, or set inside --file)`)
	cmd.Flags().StringVar(&f.Status, "status", "", "result status: ok, warn or error (default: unset)")
	cmd.Flags().StringVar(&f.Summary, "summary", "", "one-line human summary")
	cmd.Flags().StringVar(&f.Metrics, "metrics", "", `metrics as a JSON object, e.g. {"rules": 40}`)
	cmd.Flags().StringArrayVar(&f.Tables, "table", nil, `one table object as JSON ({"title":...,"columns":[...],"rows":[[...]]}); repeatable`)
	cmd.Flags().StringVar(&f.Markdown, "markdown", "", "optional short markdown notes")
	cmd.Flags().StringVar(&f.File, "file", "", "read the full payload from a JSON file (flags override its fields)")
	return cmd
}

// ----- pure helpers (unit-testable) -----------------------------------------

// scheduleResultsPostSession resolves the target session id for `post`:
// an explicit --session wins, then the DATAWATCH_SESSION_ID environment
// (injected into spawned subprocess sessions).
func scheduleResultsPostSession(flagVal string) (string, error) {
	if flagVal != "" {
		return strings.TrimSpace(flagVal), nil
	}
	if env := strings.TrimSpace(os.Getenv("DATAWATCH_SESSION_ID")); env != "" {
		return env, nil
	}
	return "", errors.New("--session is required (or run inside a spawned session with DATAWATCH_SESSION_ID set)")
}

// scheduleResultsPostToken returns the bearer token `post` sends: the
// scoped session credential when present (DATAWATCH_RESULT_TOKEN, injected
// into spawned subprocess sessions), otherwise the usual daemon token.
func scheduleResultsPostToken() string {
	if tok := strings.TrimSpace(os.Getenv("DATAWATCH_RESULT_TOKEN")); tok != "" {
		return tok
	}
	return daemonToken()
}

// buildScheduleResultsPayload assembles the POST /api/sessions/{id}/result
// payload from the post flags. It is pure (the --file read aside) so the
// flag→payload mapping is directly unit-testable.
//
// Semantics:
//   - --file (if set) must be a JSON object; it seeds the payload.
//   - Each non-empty flag overrides the corresponding file field.
//   - --table values accumulate in flag order (they replace a file's tables).
//   - a title is required, from either source.
//   - status is restricted to ""|ok|warn|error (mirrors the server).
//   - --metrics and each --table must be a JSON object.
func buildScheduleResultsPayload(f scheduleResultsPostFlags) (map[string]any, error) {
	out := map[string]any{}
	if f.File != "" {
		raw, err := os.ReadFile(f.File)
		if err != nil {
			return nil, fmt.Errorf("read --file: %w", err)
		}
		seed, err := filePayloadObject(raw)
		if err != nil {
			return nil, err
		}
		out = seed
	}
	if f.Title != "" {
		out["title"] = f.Title
	}
	if f.Status != "" {
		if !scheduleResultStatusOK(f.Status) {
			return nil, fmt.Errorf("invalid --status %q (want ok, warn or error)", f.Status)
		}
		out["status"] = f.Status
	}
	if f.Summary != "" {
		out["summary"] = f.Summary
	}
	if f.Markdown != "" {
		out["markdown"] = f.Markdown
	}
	if f.Metrics != "" {
		m, err := metricsObject(f.Metrics)
		if err != nil {
			return nil, err
		}
		out["metrics"] = m
	}
	if len(f.Tables) > 0 {
		tables, err := tableObjects(f.Tables)
		if err != nil {
			return nil, err
		}
		out["tables"] = tables
	}
	t, _ := out["title"].(string)
	if strings.TrimSpace(t) == "" {
		return nil, errors.New("a title is required (set --title or \"title\" inside --file)")
	}
	return out, nil
}

// scheduleResultStatusOK reports whether s is an accepted result status
// (empty = unset, matching the server's closed set).
func scheduleResultStatusOK(s string) bool {
	switch s {
	case "", "ok", "warn", "error":
		return true
	}
	return false
}

// filePayloadObject parses a --file value that must be a JSON object.
func filePayloadObject(raw []byte) (map[string]any, error) {
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("--file is not a JSON object: %w", err)
	}
	return v, nil
}

// metricsObject parses a --metrics value that must be a JSON object.
func metricsObject(raw string) (map[string]any, error) {
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, fmt.Errorf("--metrics is not a JSON object: %w", err)
	}
	return v, nil
}

// tableObjects parses each --table value (each must be a JSON object) and
// returns them in flag order.
func tableObjects(raws []string) ([]any, error) {
	out := make([]any, 0, len(raws))
	for i, raw := range raws {
		var v map[string]any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("--table[%d] is not a JSON object: %w", i, err)
		}
		out = append(out, v)
	}
	return out, nil
}

// scheduleResultsHistoryPath builds the escaped /api/schedule-results/{name}
// path for GET.
func scheduleResultsHistoryPath(name string) string {
	return "/api/schedule-results/" + url.PathEscape(strings.TrimSpace(name))
}

// ----- response shapes (server-side structs, read-only) ----------------------

// scheduleResultPayload is the shape of the server's ResultPayload as the
// CLI renders it.
type scheduleResultPayload struct {
	Title    string           `json:"title"`
	Status   string           `json:"status"`
	Summary  string           `json:"summary"`
	Metrics  map[string]any   `json:"metrics"`
	Markdown string           `json:"markdown"`
	Tables   []map[string]any `json:"tables"`
}

// scheduleResultEntry is one posted result as stored by the daemon.
type scheduleResultEntry struct {
	PostedAt     time.Time             `json:"posted_at"`
	SessionID    string                `json:"session_id"`
	ScheduleName string                `json:"schedule_name"`
	Payload      scheduleResultPayload `json:"payload"`
}

// scheduleResultSummary is one row of GET /api/schedule-results.
type scheduleResultSummary struct {
	ScheduleName   string              `json:"schedule_name"`
	Latest         scheduleResultEntry `json:"latest"`
	Count          int                 `json:"count"`
	RecentStatuses []string            `json:"recent_statuses"`
}

// ----- response formatting ---------------------------------------------------

// clipForCLI shortens a string for table columns, marking the cut.
func clipForCLI(s string) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= 60 {
		return string(r)
	}
	return string(r[:59]) + "…"
}

// formatScheduleResultsList renders the GET /api/schedule-results body as a
// table: one row per schedule with its latest result.
func formatScheduleResultsList(body []byte) (string, error) {
	var rows []scheduleResultSummary
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No scheduled-job results have been posted yet.\n", nil
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SCHEDULE\tSTATUS\tTITLE\tSUMMARY\tPOSTED\tRETAINED\tRECENT")
	for _, r := range rows {
		status := strings.TrimSpace(r.Latest.Payload.Status)
		if status == "" {
			status = "-"
		}
		recent := strings.Join(r.RecentStatuses, ",")
		if recent == "" {
			recent = "-"
		}
		posted := "-"
		if !r.Latest.PostedAt.IsZero() {
			posted = r.Latest.PostedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			clipForCLI(r.ScheduleName),
			status,
			clipForCLI(r.Latest.Payload.Title),
			clipForCLI(r.Latest.Payload.Summary),
			posted,
			r.Count,
			clipForCLI(recent),
		)
	}
	if err := w.Flush(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// formatScheduleResultsHistory renders the GET /api/schedule-results/{name}
// body as one block per entry, oldest first (the server's order).
func formatScheduleResultsHistory(body []byte) (string, error) {
	var entries []scheduleResultEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "No results recorded for this schedule.\n", nil
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n")
		}
		postTime := "-"
		if !e.PostedAt.IsZero() {
			postTime = e.PostedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		status := strings.TrimSpace(e.Payload.Status)
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(&b, "%s  %-5s  %s\n", postTime, status, e.Payload.Title)
		if s := strings.TrimSpace(e.Payload.Summary); s != "" {
			b.WriteString("  summary: " + clipForCLI(s) + "\n")
		}
		if e.SessionID != "" {
			b.WriteString("  session: " + e.SessionID + "\n")
		}
		if len(e.Payload.Metrics) > 0 {
			if mb, err := json.Marshal(e.Payload.Metrics); err == nil {
				b.WriteString("  metrics: " + string(mb) + "\n")
			}
		}
		if tc := len(e.Payload.Tables); tc > 0 {
			fmt.Fprintf(&b, "  tables: %d table(s)\n", tc)
		}
		if m := strings.TrimSpace(e.Payload.Markdown); m != "" {
			b.WriteString("  markdown: " + clipForCLI(m) + "\n")
		}
	}
	return b.String(), nil
}

// ----- HTTP plumbing ---------------------------------------------------------

// scheduleResultsHTTP sends a request to the daemon (via daemonURL()/
// daemonClient(), the same base-URL and TLS handling as the other REST-proxy
// verbs) and returns the raw 2xx body. The POST carries the scoped
// schedule-result credential (DATAWATCH_RESULT_TOKEN) when present; read
// endpoints use the usual daemon token.
func scheduleResultsHTTP(method, path string, body []byte) ([]byte, error) {
	client := daemonClient()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, daemonURL()+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	token := daemonToken()
	if method == http.MethodPost {
		token = scheduleResultsPostToken()
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}
