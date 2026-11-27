// `datawatch delivery`: the delivery check, one list of the work that has not
// been delivered or finished. Same names and arguments as the REST, MCP and
// comm-channel surfaces:
//
//	datawatch delivery status                 the current report; runs no check
//	datawatch delivery check                  run the check; exit 1 when red
//	datawatch delivery ack --key <k>          mark entries as seen
//	datawatch delivery accept --backlog --reason <code> --until <date>
//	datawatch delivery drop --key <k> --reason <code>
//	datawatch delivery test-message
//
// The check reports: it blocks nothing.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/delivery"
)

// deliveryActorCLI labels the operator's own command line.
const deliveryActorCLI = "cli"

// deliverySelectorFlags are the flags that choose entries.
type deliverySelectorFlags struct {
	keys                     []string
	kind, state, matchReason string
	backlog                  bool
}

func (f *deliverySelectorFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.keys, "key", nil, "Entry key, as <kind>:<id>:<state> (repeatable)")
	cmd.Flags().StringVar(&f.kind, "kind", "", "Only entries of this kind (automaton, task, bug, schedule, session, ...)")
	cmd.Flags().StringVar(&f.state, "state", "", "Only entries in this state")
	cmd.Flags().StringVar(&f.matchReason, "match-reason", "", "Only entries listed for this reason code")
	cmd.Flags().BoolVar(&f.backlog, "backlog", false, "Only the backlog: entries already true when the check was first started")
}

// body is the action's request. At least one flag must choose entries; a blank
// key is refused, because leaving it out would widen the choice.
func (f *deliverySelectorFlags) body() (map[string]any, error) {
	body := map[string]any{}
	var keys []string
	for _, k := range f.keys {
		if k = strings.TrimSpace(k); k == "" {
			return nil, errors.New("--key must not be empty")
		}
		keys = append(keys, k)
	}
	if len(keys) > 0 {
		body["keys"] = keys
	}
	for name, v := range map[string]string{"kind": f.kind, "state": f.state, "match_reason": f.matchReason} {
		if v = strings.TrimSpace(v); v != "" {
			body[name] = v
		}
	}
	if f.backlog {
		body["backlog"] = true
	}
	if len(body) == 0 {
		return nil, errors.New("name the entries with --key, --kind, --state, --match-reason or --backlog")
	}
	body["actor"] = deliveryActorCLI
	return body, nil
}

// deliveryPrint writes the daemon's answer: as JSON, or as text made by text.
func deliveryPrint(w io.Writer, asJSON bool, answer []byte, text func([]byte) (string, error)) error {
	if asJSON {
		prettyPrintTo(w, answer)
		return nil
	}
	out, err := text(answer)
	if err != nil {
		return fmt.Errorf("unreadable answer from the daemon: %w", err)
	}
	_, err = fmt.Fprintln(w, out)
	return err
}

func deliveryReportText(answer []byte) (string, error) {
	var r delivery.Report
	if err := json.Unmarshal(answer, &r); err != nil {
		return "", err
	}
	return delivery.RenderReport(r, 0), nil
}

func deliveryTestText(answer []byte) (string, error) {
	var out struct {
		Results []delivery.SendResult `json:"results"`
	}
	if err := json.Unmarshal(answer, &out); err != nil {
		return "", err
	}
	if len(out.Results) == 0 {
		return "no message channel can reach a person", nil
	}
	lines := make([]string, len(out.Results))
	for i, r := range out.Results {
		state := "sent"
		if !r.OK {
			state = "failed (" + r.Code + ")"
		}
		lines[i] = r.Channel + ": " + state
	}
	return strings.Join(lines, "\n"), nil
}

// deliveryActionCmd is ack, accept or drop. done words the count of changed
// entries.
func deliveryActionCmd(use, short string, asJSON *bool, done func(n int) string, extra func(*cobra.Command) func(map[string]any) error) *cobra.Command {
	sel := &deliverySelectorFlags{}
	var addFlags func(map[string]any) error
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := sel.body()
			if err != nil {
				return err
			}
			if addFlags != nil {
				if err := addFlags(body); err != nil {
					return err
				}
			}
			answer, err := daemonJSONBody(http.MethodPost, "/api/delivery/"+use, body)
			if err != nil {
				return err
			}
			return deliveryPrint(cmd.OutOrStdout(), *asJSON, answer, func(b []byte) (string, error) {
				var out struct {
					Changed int `json:"changed"`
				}
				if err := json.Unmarshal(b, &out); err != nil {
					return "", err
				}
				return done(out.Changed), nil
			})
		},
	}
	sel.register(cmd)
	if extra != nil {
		addFlags = extra(cmd)
	}
	return cmd
}

// deliveryRequired puts a required flag's value into the body.
func deliveryRequired(body map[string]any, name, value string) error {
	if value = strings.TrimSpace(value); value == "" {
		return fmt.Errorf("--%s is required", name)
	}
	body[name] = value
	return nil
}

// newDeliveryCmd is "datawatch delivery".
func newDeliveryCmd() *cobra.Command {
	var asJSON bool
	root := &cobra.Command{
		Use:   "delivery",
		Short: "List and check work that has not been delivered or finished",
		Long: `The delivery check keeps one list of the work that has not been delivered or
finished, checks it, and sends what is new to the message channels. It
reports: it blocks nothing and types nothing into a session. Entries hold IDs,
kinds, states, reason codes and times only.`,
		Example: `  datawatch delivery check
  datawatch delivery ack --key task:t1:failed
  datawatch delivery accept --kind automaton --state held --reason parked --until 2026-12-01
  datawatch delivery drop --backlog --reason obsolete`,
	}
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "Print the daemon's answer as JSON")

	status := &cobra.Command{
		Use:   "status",
		Short: "Show the current report without running the check",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			answer, err := daemonGetBody("/api/delivery")
			if err != nil {
				return err
			}
			return deliveryPrint(cmd.OutOrStdout(), asJSON, answer, deliveryReportText)
		},
	}
	check := &cobra.Command{
		Use:   "check",
		Short: "Run the check now and show the report; exit 1 when it is not green",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			answer, err := daemonJSONBody(http.MethodPost, "/api/delivery/check", nil)
			if err != nil {
				return err
			}
			if err := deliveryPrint(cmd.OutOrStdout(), asJSON, answer, deliveryReportText); err != nil {
				return err
			}
			var r delivery.Report
			if err := json.Unmarshal(answer, &r); err != nil {
				return fmt.Errorf("unreadable answer from the daemon: %w", err)
			}
			if r.Result != delivery.ResultGreen {
				return errors.New("the delivery check is red")
			}
			return nil
		},
	}
	ack := deliveryActionCmd("ack", "Mark entries as seen; their messages stop and they stay listed", &asJSON,
		func(n int) string { return fmt.Sprintf("%d marked as seen; they stay listed", n) }, nil)

	var acceptReason, until string
	accept := deliveryActionCmd("accept", "Let entries wait until a review date at most 30 days ahead", &asJSON,
		func(n int) string { return fmt.Sprintf("%d accepted until %s", n, strings.TrimSpace(until)) },
		func(cmd *cobra.Command) func(map[string]any) error {
			cmd.Flags().StringVar(&acceptReason, "reason", "", "Reason code (required): a-z, 0-9, dot, dash and underscore")
			cmd.Flags().StringVar(&until, "until", "", "Review date (required): YYYY-MM-DD or RFC 3339")
			return func(body map[string]any) error {
				if err := deliveryRequired(body, "reason", acceptReason); err != nil {
					return err
				}
				return deliveryRequired(body, "until", until)
			}
		})

	var dropReason string
	drop := deliveryActionCmd("drop", "Close failed or backlog entries", &asJSON,
		func(n int) string { return fmt.Sprintf("%d dropped", n) },
		func(cmd *cobra.Command) func(map[string]any) error {
			cmd.Flags().StringVar(&dropReason, "reason", "", "Reason code (required): a-z, 0-9, dot, dash and underscore")
			return func(body map[string]any) error { return deliveryRequired(body, "reason", dropReason) }
		})

	testMessage := &cobra.Command{
		Use:   "test-message",
		Short: "Send a test message on every message channel and show what each answered",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			answer, err := daemonJSONBody(http.MethodPost, "/api/delivery/test-message", nil)
			if err != nil {
				return err
			}
			return deliveryPrint(cmd.OutOrStdout(), asJSON, answer, deliveryTestText)
		},
	}

	root.AddCommand(status, check, ack, accept, drop, testMessage)
	return root
}
