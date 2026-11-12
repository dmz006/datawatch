// datawatch automation onboarding-report [--format text|json]
//
// Shows the onboarding report: what is configured, what is missing and what to
// try next. The text is rendered by the onboardingreport package, the same
// renderer the REST route, the MCP tool and the comm verb use; --format json
// prints the REST body untouched.

package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dmz006/datawatch/internal/onboardingreport"
)

func newAutomationOnboardingReportCmd() *cobra.Command {
	var format string
	c := &cobra.Command{
		Use:   "onboarding-report",
		Short: "Show what is configured, what is missing and what to try next",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := strings.ToLower(strings.TrimSpace(format))
			if f != "text" && f != "json" {
				return fmt.Errorf("--format must be text or json, got %q", format)
			}
			body, err := progressGet("/api/automation/onboarding-report", url.Values{"format": {"json"}})
			if err != nil {
				return err
			}
			if f == "json" {
				_, err = cmd.OutOrStdout().Write(body)
				return err
			}
			var rep onboardingreport.Report
			if err := json.Unmarshal(body, &rep); err != nil {
				return fmt.Errorf("decode report: %w", err)
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), rep.Text())
			return err
		},
	}
	c.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return c
}
