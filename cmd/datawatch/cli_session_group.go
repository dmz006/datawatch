// `session list --group-by` — the grouped text view of the session list.
//
// The grouping itself is the single pure read behind GET
// /api/sessions?group_by=...&tag_prefix=...; the CLI only sends the query
// (reusing the sessions list command wiring) and renders the grouped
// response as a text view: a group header with its count and per-state mix,
// then the group's sessions indented underneath. On the automaton axis the
// story blocks and the block's progress count come with the response.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// sessionGroupsQuery builds the GET /api/sessions query for the grouped
// view from the session list flags.
func sessionGroupsQuery(cmd *cobra.Command) url.Values {
	q := url.Values{}
	if groupBy, _ := cmd.Flags().GetString("group-by"); groupBy != "" {
		q.Set("group_by", groupBy)
	}
	if prefix, _ := cmd.Flags().GetString("tag-prefix"); prefix != "" {
		q.Set("tag_prefix", prefix)
	}
	return q
}

// sessionGroupMember mirrors one REST grouped-response member.
type sessionGroupMember struct {
	ID      string `json:"id"`
	FullID  string `json:"full_id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	StoryID string `json:"story_id"`
	TaskID  string `json:"task_id"`
}

// sessionGroupStory mirrors one story block of the automaton axis.
type sessionGroupStory struct {
	Key      string               `json:"key"`
	Count    int                  `json:"count"`
	StateMix map[string]int       `json:"state_mix"`
	Sessions []sessionGroupMember `json:"sessions"`
}

// sessionGroupEntry mirrors one group of the grouped response.
type sessionGroupEntry struct {
	Key      string               `json:"key"`
	Count    int                  `json:"count"`
	StateMix map[string]int       `json:"state_mix"`
	Sessions []sessionGroupMember `json:"sessions"`
	Stories  []sessionGroupStory  `json:"stories"`
	Progress *struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
}

// sessionGroupsBody is the GET /api/sessions?group_by=... response.
type sessionGroupsBody struct {
	GroupBy     string              `json:"group_by"`
	TagPrefix   string              `json:"tag_prefix"`
	Groups      []sessionGroupEntry `json:"groups"`
	StateTotals map[string]int      `json:"state_totals"`
	Total       int                 `json:"total"`
}

// stateMixText renders a per-state mix as "state×n, state×m" in stable order.
func stateMixText(mix map[string]int) string {
	keys := make([]string, 0, len(mix))
	for k := range mix {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, mix[k]))
	}
	return strings.Join(parts, ", ")
}

// renderSessionGroups renders the grouped response as the text view:
// one header line per group (count + state mix, plus the progress count on
// the automaton axis) with the group's sessions indented underneath.
func renderSessionGroups(body []byte) (string, error) {
	var g sessionGroupsBody
	if err := json.Unmarshal(body, &g); err != nil {
		return "", fmt.Errorf("decode grouped sessions: %w", err)
	}

	var sb strings.Builder
	if g.TagPrefix != "" {
		fmt.Fprintf(&sb, "Sessions grouped by %s (tag prefix %q), %d total:\n", g.GroupBy, g.TagPrefix, g.Total)
	} else {
		fmt.Fprintf(&sb, "Sessions grouped by %s, %d total:\n", g.GroupBy, g.Total)
	}

	writeMember := func(indent string, m sessionGroupMember) {
		name := m.Name
		if name == "" {
			name = "(no task)"
		}
		if m.TaskID != "" {
			fmt.Fprintf(&sb, "%s[%s] %s | %s%s", indent, m.ID, name, m.State, " · task "+m.TaskID)
		} else {
			fmt.Fprintf(&sb, "%s[%s] %s | %s", indent, m.ID, name, m.State)
		}
		sb.WriteByte('\n')
	}

	for _, grp := range g.Groups {
		mix := stateMixText(grp.StateMix)
		if grp.Progress != nil {
			fmt.Fprintf(&sb, "  %s  %d (%s) — done %d/%d\n", grp.Key, grp.Count, mix, grp.Progress.Done, grp.Progress.Total)
		} else {
			fmt.Fprintf(&sb, "  %s  %d (%s)\n", grp.Key, grp.Count, mix)
		}
		if len(grp.Stories) > 0 {
			for _, story := range grp.Stories {
				if story.Key == "root" {
					sb.WriteString("    (root)  ")
				} else {
					fmt.Fprintf(&sb, "    story %s:  ", story.Key)
				}
				fmt.Fprintf(&sb, "%d (%s)\n", story.Count, stateMixText(story.StateMix))
				for _, m := range story.Sessions {
					writeMember("      ", m)
				}
			}
		} else {
			for _, m := range grp.Sessions {
				writeMember("    ", m)
			}
		}
	}

	sb.WriteString("\nPer-state totals: ")
	sb.WriteString(stateMixText(g.StateTotals) + "\n")
	return strings.TrimRight(sb.String(), "\n") + "\n", nil
}

// daemonGetSessionGroups runs the grouped read over the daemon API and
// prints the grouped text view.
func daemonGetSessionGroups(cmd *cobra.Command) error {
	q := sessionGroupsQuery(cmd)
	req, err := http.NewRequest(http.MethodGet, daemonURL()+sessionsPath(q), nil)
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
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	text, err := renderSessionGroups(body)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}
