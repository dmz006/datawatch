// Low-bandwidth list flags for `session list` and `alerts` (BL432 / GH#242).
//
// `session list --summary/--states/--since` map to GET /api/sessions
// ?view=summary&states=&since=; `alerts --limit/--unread` map to the
// paged GET /api/alerts and `alerts --count` to GET /api/alerts/count.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// sessionListQuery builds the GET /api/sessions query from the session
// list flags. Returns an empty set when no filter flag was given.
func sessionListQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}
	for _, f := range []struct{ flag, param string }{
		{"name", "name"},
		{"state", "state"},
		{"backend", "backend"},
		{"alive", "alive"},
		{"states", "states"},
		{"since", "since"},
	} {
		if v, _ := cmd.Flags().GetString(f.flag); v != "" {
			q.Set(f.param, v)
		}
	}
	for _, v := range flagStringArray(cmd, "tag") {
		if v != "" {
			q.Add("tag", v)
		}
	}
	if v, _ := cmd.Flags().GetString("tag-match"); v != "" {
		q.Set("tag_match", v)
	}
	// Axis shortcuts: each non-empty flag adds one "<axis>:<value>" tag so
	// that e.g. --project web --stage task filters by both of those tags
	// (combined with --tag, under the same --tag-match rule).
	for _, axis := range []string{"project", "automaton", "automation", "stage", "kind", "role"} {
		v, _ := cmd.Flags().GetString(axis)
		if v = strings.TrimSpace(v); v != "" {
			q.Add("tag", axis+":"+v)
		}
	}
	if summary, _ := cmd.Flags().GetBool("summary"); summary {
		q.Set("view", "summary")
	}
	return q
}

// flagStringArray reads a --flag that may have been registered as a repeatable
// string-array or a comma-separated string, so both registrations work.
func flagStringArray(cmd *cobra.Command, name string) []string {
	if cmd.Flags().Changed(name) {
		if vs, err := cmd.Flags().GetStringArray(name); err == nil {
			return vs
		}
	}
	if v, _ := cmd.Flags().GetString(name); v != "" {
		return strings.Split(v, ",")
	}
	return nil
}

// sessionsPath appends q to /api/sessions.
func sessionsPath(q url.Values) string {
	if len(q) == 0 {
		return "/api/sessions"
	}
	return "/api/sessions?" + q.Encode()
}

// alertsListQuery builds the GET /api/alerts query. --limit and --unread
// switch the daemon to its paged response; without them the full list is
// returned as before.
func alertsListQuery(cmd *cobra.Command) (url.Values, error) {
	q := url.Values{}
	if system, _ := cmd.Flags().GetBool("system"); system {
		q.Set("source", "system")
	}
	if cmd.Flags().Changed("limit") {
		n, _ := cmd.Flags().GetInt("limit")
		if n < 0 {
			return nil, fmt.Errorf("--limit must be >= 0")
		}
		q.Set("limit", strconv.Itoa(n))
	}
	if unread, _ := cmd.Flags().GetBool("unread"); unread {
		q.Set("unread", "1")
	}
	return q, nil
}

// printAlertsCount fetches GET /api/alerts/count and prints the badge numbers.
func printAlertsCount(baseURL string) error {
	resp, err := http.Get(baseURL + "/count")
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("alerts count failed: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var c struct {
		UnreadCount int `json:"unread_count"`
		Total       int `json:"total"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return fmt.Errorf("decode alerts count: %w", err)
	}
	fmt.Printf("%d unread / %d total\n", c.UnreadCount, c.Total)
	return nil
}
