// datawatch autonomous deploy show|run
//
// show prints the effective autonomous.deploy settings and the last deploy
// record (GET /api/autonomous/deploy); run starts the deploy sequence for the
// current build now (POST /api/autonomous/deploy/run). The text is rendered by
// internal/deploy view.go, the same code the chat verb uses. The settings are
// changed with: datawatch config set autonomous.deploy.<key> <value>.

package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/deploy"
)

const (
	deployAPI    = "/api/autonomous/deploy"
	deployRunAPI = deployAPI + "/run"
)

func newAutonomousDeployCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "deploy",
		Short: "Show the deploy settings and last record, or run a deploy now",
		Long: `The deploy step turns a shipped build into the running daemon.

  show [--json]   The effective settings and the last deploy record
  run  [--json]   Start the deploy sequence for the current build now

Change a setting with: datawatch config set autonomous.deploy.<key> <value>
`,
	}
	c.AddCommand(newDeployShowCmd(), newDeployRunCmd())
	return c
}

func newDeployShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show",
		Short: "Show the effective deploy settings and the last deploy record",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := mergePipelineSend(http.MethodGet, deployAPI, nil, nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			v, err := deploy.ParseStatusView(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), deploy.RenderStatusText(v))
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}

func newDeployRunCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "run",
		Short: "Start the deploy sequence for the current build now",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			body, err := mergePipelineSend(http.MethodPost, deployRunAPI, nil, nil)
			if err != nil {
				return err
			}
			if asJSON {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			text, err := deploy.RenderRunText(body)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), text)
			return err
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the raw JSON response")
	return c
}
