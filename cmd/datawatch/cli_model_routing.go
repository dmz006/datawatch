// Model routing commands:
//
//	datawatch compute node served-models [name] [--model M] [--backend B] [--refresh] [--json]
//	datawatch autonomous prd-set-task-node  <automaton-id> --task  <task-id>  --node <node>
//	datawatch autonomous prd-set-story-node <automaton-id> --story <story-id> --node <node>
//
// served-models reads GET /api/compute/served-models and prints one line per
// node. The text is built by renderServedModelsText so a test can check it
// without a daemon; --json prints the REST body untouched. The two pin commands
// post to the set_task_node and set_story_node actions; an empty --node clears
// the pin.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// servedModelsAnswer is the part of the served-models body the text view reads.
type servedModelsAnswer struct {
	CacheSeconds int                  `json:"cache_seconds"`
	RefusalMode  string               `json:"refusal_mode"`
	Nodes        []servedModelsEntry  `json:"nodes"`
	Check        *servedModelsVerdict `json:"check"`
}

type servedModelsEntry struct {
	Node       string   `json:"node"`
	Kind       string   `json:"kind"`
	Models     []string `json:"models"`
	Known      bool     `json:"known"`
	Cached     bool     `json:"cached"`
	AgeSeconds int      `json:"age_seconds"`
	Error      string   `json:"error"`
	Disabled   bool     `json:"disabled"`
}

type servedModelsVerdict struct {
	Model       string   `json:"model"`
	Applicable  bool     `json:"applicable"`
	Refused     bool     `json:"refused"`
	NodeUnknown bool     `json:"node_unknown"`
	ServedBy    []string `json:"served_by"`
	Checked     []struct {
		Node   string `json:"node"`
		State  string `json:"state"`
		Reason string `json:"reason"`
	} `json:"checked"`
	Message string `json:"message"`
}

// renderServedModelsText turns a served-models body into the operator text:
// one line per node (name, kind, model count, cached or fresh with its age,
// then the models), and, when the answer carries a model check, the check
// result with its message.
func renderServedModelsText(body []byte) (string, error) {
	var a servedModelsAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return "", fmt.Errorf("served models answer is not valid JSON: %w", err)
	}
	var b strings.Builder
	if len(a.Nodes) == 0 {
		b.WriteString("No compute nodes are registered.\n")
	}
	for _, n := range a.Nodes {
		kind := n.Kind
		if kind == "" {
			kind = "unknown kind"
		}
		switch {
		case n.Disabled:
			fmt.Fprintf(&b, "%s (%s): disabled, not read\n", n.Node, kind)
		case !n.Known:
			reason := n.Error
			if reason == "" {
				reason = "no answer from the node"
			}
			fmt.Fprintf(&b, "%s (%s): models unknown (%s)\n", n.Node, kind, reason)
		default:
			source := "fresh"
			if n.Cached {
				source = fmt.Sprintf("cached, %ds old", n.AgeSeconds)
			}
			noun := "models"
			if len(n.Models) == 1 {
				noun = "model"
			}
			list := "none"
			if len(n.Models) > 0 {
				list = strings.Join(n.Models, ", ")
			}
			fmt.Fprintf(&b, "%s (%s): %d %s, %s: %s\n", n.Node, kind, len(n.Models), noun, source, list)
		}
	}
	if c := a.Check; c != nil {
		b.WriteString("Model check for " + c.Model + ": " + servedModelsCheckResult(c) + "\n")
		if c.Message != "" {
			b.WriteString(c.Message + "\n")
		}
		if len(c.ServedBy) > 0 {
			b.WriteString("Served by: " + strings.Join(c.ServedBy, ", ") + "\n")
		}
		for _, s := range c.Checked {
			line := "  " + s.Node + ": " + s.State
			if s.Reason != "" {
				line += " (" + s.Reason + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String(), nil
}

// servedModelsCheckResult names the outcome of a model check in one phrase.
func servedModelsCheckResult(c *servedModelsVerdict) string {
	switch {
	case !c.Applicable:
		return "does not apply"
	case c.Refused:
		return "refused"
	case c.NodeUnknown:
		return "allowed, node models unknown"
	default:
		return "allowed"
	}
}

func newComputeNodeServedModelsCmd() *cobra.Command {
	var model, backend string
	var refresh, asJSON bool
	c := &cobra.Command{
		Use:   "served-models [name]",
		Short: "Show which models each compute node serves, and optionally check one model",
		Long: `One line per node: name, kind, model count, whether the list came from the
cache or fresh from the node (with its age), then the models.

--model also prints the result of the pin-time check for that model, with its
message: the same check that refuses a pin to a node that does not serve it.
--backend narrows that check to one backend. --refresh reads from the node
instead of the cache. --json prints the response untouched.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if len(args) == 1 {
				if name := strings.TrimSpace(args[0]); name != "" {
					q.Set("node", name)
				}
			}
			if m := strings.TrimSpace(model); m != "" {
				q.Set("model", m)
			}
			if bk := strings.TrimSpace(backend); bk != "" {
				q.Set("backend", bk)
			}
			if refresh {
				q.Set("refresh", "true")
			}
			body, err := progressGet("/api/compute/served-models", q)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			text, err := renderServedModelsText(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().StringVar(&model, "model", "", "also check whether this model is served, and print the check result and message")
	c.Flags().StringVar(&backend, "backend", "", "backend the model check is for (default: every enabled node)")
	c.Flags().BoolVar(&refresh, "refresh", false, "read from the nodes now instead of the cache")
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

// nodePinBody builds the body of a node pin request. idKey is task_id or
// story_id; an empty node clears the pin.
func nodePinBody(idKey, id, node string) []byte {
	body, _ := json.Marshal(map[string]string{idKey: id, "node": node, "actor": "operator"})
	return body
}

func newAutonomousPRDSetTaskNodeCmd() *cobra.Command {
	var taskID, node string
	cmd := &cobra.Command{
		Use:   "prd-set-task-node <automaton-id> --task <task-id> --node <node>",
		Short: "Pin one task of an Automaton to a compute node. An empty --node clears the pin.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_task_node", nodePinBody("task_id", taskID, node))
		},
	}
	cmd.Flags().StringVar(&taskID, "task", "", "task ID to pin (required)")
	cmd.Flags().StringVar(&node, "node", "", "compute node name; empty clears the pin")
	_ = cmd.MarkFlagRequired("task")
	return cmd
}

func newAutonomousPRDSetStoryNodeCmd() *cobra.Command {
	var storyID, node string
	cmd := &cobra.Command{
		Use:   "prd-set-story-node <automaton-id> --story <story-id> --node <node>",
		Short: "Pin every task of one story of an Automaton to a compute node. An empty --node clears the pin.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodPost, "/api/autonomous/prds/"+args[0]+"/set_story_node", nodePinBody("story_id", storyID, node))
		},
	}
	cmd.Flags().StringVar(&storyID, "story", "", "story ID to pin (required)")
	cmd.Flags().StringVar(&node, "node", "", "compute node name; empty clears the pin")
	_ = cmd.MarkFlagRequired("story")
	return cmd
}
