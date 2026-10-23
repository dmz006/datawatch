// CLI for fingerprint match records: list the matches the daemon detected
// while reconciling resource fingerprints across federation peers, and
// confirm or reject them. A confirmation is a suggestion until the operator
// decides: confirming merges the two resources into one shared pool,
// rejecting keeps (or makes) them separate again.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

// fingerprintMatch mirrors the daemon's /api/federation/fingerprint-matches
// record (the fields the operator needs to identify the pair).
type fingerprintMatch struct {
	ID        string `json:"id"`
	Peer      string `json:"peer"`
	Kind      string `json:"kind"`
	LocalName string `json:"local_name"`
	PeerName  string `json:"peer_name"`
	State     string `json:"state"`
}

// fingerprintLine is one table row: id, state, kind, local resource name,
// peer name, peer's resource name.
func fingerprintLine(m fingerprintMatch) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s",
		m.ID, m.State, m.Kind, m.LocalName, m.Peer, m.PeerName)
}

// fingerprintDecision calls {id}/confirm or {id}/reject and returns the
// updated record the daemon echoes back.
func fingerprintDecision(id, action string) (fingerprintMatch, error) {
	body, err := daemonJSONBody(http.MethodPost, "/api/federation/fingerprint-matches/"+id+"/"+action, nil)
	if err != nil {
		return fingerprintMatch{}, err
	}
	var m fingerprintMatch
	err = json.Unmarshal(body, &m)
	return m, err
}

func newFingerprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fingerprint",
		Short: "List, confirm or reject resource-fingerprint matches across federation peers",
	}

	listCmd := &cobra.Command{
		Use:   "list [--state pending|confirmed|rejected]",
		Short: "List fingerprint match records",
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, _ := cmd.Flags().GetString("state")
			if state != "" &&
				state != "pending" && state != "confirmed" && state != "rejected" {
				return fmt.Errorf("invalid --state %q: expected pending, confirmed or rejected", state)
			}
			path := "/api/federation/fingerprint-matches"
			if state != "" {
				path += "?state=" + state
			}
			body, err := daemonJSONBody(http.MethodGet, path, nil)
			if err != nil {
				return err
			}
			var out struct {
				Matches []fingerprintMatch `json:"matches"`
			}
			if err := json.Unmarshal(body, &out); err != nil {
				return err
			}
			if len(out.Matches) == 0 {
				fmt.Println("no fingerprint matches")
				return nil
			}
			fmt.Println("ID\tSTATE\tKIND\tLOCAL\tPEER\tPEER_NAME")
			for _, m := range out.Matches {
				fmt.Println(fingerprintLine(m))
			}
			fmt.Println("NOTE: confirming a match makes the two resources one shared pool; rejecting keeps them separate.")
			return nil
		},
	}
	listCmd.Flags().String("state", "", "filter by state: pending, confirmed or rejected (default: all)")
	cmd.AddCommand(listCmd)

	confirmCmd := &cobra.Command{
		Use:   "confirm <id>",
		Short: "Confirm a fingerprint match — the two resources become one shared pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			m, err := fingerprintDecision(args[0], "confirm")
			if err != nil {
				return err
			}
			fmt.Printf("Confirmed %s: %s and %s/%s are now one shared pool.\n",
				m.ID, m.LocalName, m.Peer, m.PeerName)
			return nil
		},
	}
	cmd.AddCommand(confirmCmd)

	rejectCmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "Reject a fingerprint match — the two resources stay separate (any confirm is undone)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			m, err := fingerprintDecision(args[0], "reject")
			if err != nil {
				return err
			}
			fmt.Printf("Rejected %s: %s and %s/%s are separate resources again (confirm undone if it had been confirmed).\n",
				m.ID, m.LocalName, m.Peer, m.PeerName)
			return nil
		},
	}
	cmd.AddCommand(rejectCmd)

	return cmd
}
