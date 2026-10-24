// datawatch autonomous progress [--json] [--automation ID] [--tag ID] [--since DATE] [--until DATE]
// datawatch autonomous progress-trend [--days N] [--json]
//
// Read the Automata progress metrics and the saved trend from the daemon. The
// text is the one renderer the comm channels use; --json prints the REST body
// untouched.

package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// progressGet reads one progress route and returns the body untouched.
func progressGet(path string, q url.Values) ([]byte, error) {
	u := daemonURL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func newAutonomousProgressCmd() *cobra.Command {
	var asJSON bool
	var automation, tag, since, until string
	c := &cobra.Command{
		Use:   "progress",
		Short: "Show Automata progress: counts, rates, work left and the completion range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			for k, v := range map[string]string{"automation_id": automation, "tag_id": tag, "since": since, "until": until} {
				if v != "" {
					q.Set(k, v)
				}
			}
			body, err := progressGet("/api/autonomous/progress", q)
			if err != nil {
				return err
			}
			return renderProgress(cmd.OutOrStdout(), body, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().StringVar(&automation, "automation", "", "count only the Automata of this Automation")
	c.Flags().StringVar(&tag, "tag", "", "count only the Automata carrying this tag")
	c.Flags().StringVar(&since, "since", "", "first day counted, YYYY-MM-DD")
	c.Flags().StringVar(&until, "until", "", "last day counted, YYYY-MM-DD (inclusive)")
	return c
}

func newAutonomousProgressTrendCmd() *cobra.Command {
	var asJSON bool
	var days int
	c := &cobra.Command{
		Use:   "progress-trend",
		Short: "Show how the completion range moved over the saved daily snapshots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q := url.Values{}
			if days != 0 {
				q.Set("days", strconv.Itoa(days))
			}
			body, err := progressGet("/api/autonomous/progress/trend", q)
			if err != nil {
				return err
			}
			return renderProgressTrend(cmd.OutOrStdout(), body, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	c.Flags().IntVar(&days, "days", 0, "how many days back to read, 1 to 365 (default 30)")
	return c
}

func renderProgress(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	m, err := autonomous.ParseProgressMetrics(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderProgressText(m))
	return err
}

func renderProgressTrend(w io.Writer, body []byte, asJSON bool) error {
	if asJSON {
		_, err := w.Write(body)
		return err
	}
	t, err := autonomous.ParseProgressTrend(body)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, autonomous.RenderTrendReply(t.Days, t.Direction, t.Points))
	return err
}
