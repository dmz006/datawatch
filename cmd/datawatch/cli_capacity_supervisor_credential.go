// datawatch capacity supervisor-credential mint|list|revoke — the thin,
// removable credential that makes an external supervisor's REST session start
// the supervisor admission class. Removed together with the credential when the
// daemon runs the detector itself.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/spf13/cobra"
)

func newCapacitySupervisorCredentialCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "supervisor-credential",
		Short: "Mint, list or revoke the supervisor credential (a REST start with it is the supervisor class)",
		Long: `The supervisor credential lets an external supervisor (the detector timer) start sessions in the
supervisor admission class over REST: it may use the reserved supervisor buffer on a pool and never goes past it,
and every start is audited with the credential's name. Only the operator can mint, list or revoke it (a session
credential or a federation peer cannot). The token is shown once at mint. Present it as the bearer token of the
start call only; use the normal token for everything else. Setting autonomous.capacity_supervisor_credential_enabled
false makes every credential do nothing without revoking it.`,
	}
	c.AddCommand(supervisorCredentialMintCmd(), supervisorCredentialListCmd(), supervisorCredentialRevokeCmd())
	return c
}

func supervisorCredentialDo(method, query string, body []byte) ([]byte, error) {
	req, err := http.NewRequest(method, daemonURL()+"/api/capacity/supervisor-credential"+query, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon not reachable (%s): %w", daemonURL(), err)
	}
	defer resp.Body.Close() //nolint:errcheck
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(out))
	}
	return out, nil
}

func supervisorCredentialMintCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "mint --name NAME",
		Short: "Mint a supervisor credential; the token is shown once (minting the same name replaces it)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			body, _ := json.Marshal(map[string]string{"name": name})
			out, err := supervisorCredentialDo(http.MethodPost, "", body)
			if err != nil {
				return err
			}
			var v struct{ Name, Token string }
			if err := json.Unmarshal(out, &v); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Credential %s minted. Token (shown once):\n%s\n", v.Name, v.Token)
			return err
		},
	}
	c.Flags().StringVar(&name, "name", "", "credential name (a-z, 0-9, dot, dash, underscore)")
	return c
}

func supervisorCredentialListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the supervisor credentials by name (never the token)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := supervisorCredentialDo(http.MethodGet, "", nil)
			if err != nil {
				return err
			}
			return renderSupervisorCredentials(cmd.OutOrStdout(), out)
		},
	}
}

func renderSupervisorCredentials(w io.Writer, body []byte) error {
	var v struct {
		Credentials []string `json:"credentials"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return err
	}
	if len(v.Credentials) == 0 {
		_, err := fmt.Fprintln(w, "No supervisor credentials.")
		return err
	}
	for _, n := range v.Credentials {
		if _, err := fmt.Fprintln(w, n); err != nil {
			return err
		}
	}
	return nil
}

func supervisorCredentialRevokeCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "revoke --name NAME",
		Short: "Revoke a supervisor credential; it stops working at once",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if name == "" {
				return fmt.Errorf("--name is required")
			}
			if _, err := supervisorCredentialDo(http.MethodDelete, "?name="+url.QueryEscape(name), nil); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Credential %s revoked.\n", name)
			return err
		},
	}
	c.Flags().StringVar(&name, "name", "", "credential name")
	return c
}
