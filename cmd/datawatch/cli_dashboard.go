// cli_dashboard.go — #57/#58 CLI surface for dashboard card layout.
//
//	datawatch dashboard ls                     list cards
//	datawatch dashboard card get <id>          get one card
//	datawatch dashboard card set <id> <cs>     set column span (add if missing)
//	datawatch dashboard card add <id> <cs>     append a card
//	datawatch dashboard card rm <id>           remove a card
//	datawatch dashboard layout get             get raw layout JSON
//	datawatch dashboard layout set             set raw layout JSON from stdin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Manage dashboard card layout (#57)",
	}
	cmd.AddCommand(newDashboardLsCmd())
	cmd.AddCommand(newDashboardCardCmd())
	cmd.AddCommand(newDashboardLayoutCmd())
	return cmd
}

func newDashboardLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List dashboard cards",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/dashboard/cards") },
	}
}

func newDashboardCardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "card",
		Short: "Card sub-commands (get/set/add/rm)",
	}
	cmd.AddCommand(newDashboardCardGetCmd())
	cmd.AddCommand(newDashboardCardSetCmd())
	cmd.AddCommand(newDashboardCardAddCmd())
	cmd.AddCommand(newDashboardCardRmCmd())
	return cmd
}

func newDashboardCardGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get one dashboard card by id",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return daemonGet("/api/dashboard/cards/" + args[0]) },
	}
}

func newDashboardCardSetCmd() *cobra.Command {
	var rs int
	cmd := &cobra.Command{
		Use:   "set <id> <cs>",
		Short: "Set column span for a card; adds the card if not present",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cs, err := strconv.Atoi(args[1])
			if err != nil || cs < 1 || cs > 12 {
				return fmt.Errorf("cs must be 1-12")
			}
			body := map[string]any{"id": args[0], "cs": cs}
			if rs > 0 {
				body["rs"] = rs
			}
			return daemonJSON(http.MethodPut, "/api/dashboard/cards/"+args[0], body)
		},
	}
	cmd.Flags().IntVar(&rs, "rs", 0, "row span (optional)")
	return cmd
}

func newDashboardCardAddCmd() *cobra.Command {
	var rs int
	cmd := &cobra.Command{
		Use:   "add <id> <cs>",
		Short: "Append a card to the dashboard layout",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			cs, err := strconv.Atoi(args[1])
			if err != nil || cs < 1 || cs > 12 {
				return fmt.Errorf("cs must be 1-12")
			}
			body := map[string]any{"id": args[0], "cs": cs}
			if rs > 0 {
				body["rs"] = rs
			}
			return daemonJSON(http.MethodPost, "/api/dashboard/cards", body)
		},
	}
	cmd.Flags().IntVar(&rs, "rs", 0, "row span (optional)")
	return cmd
}

func newDashboardCardRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Remove a card from the dashboard layout",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/dashboard/cards/"+args[0], nil)
		},
	}
}

func newDashboardLayoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "layout",
		Short: "Raw dashboard layout (get/set)",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get raw dashboard layout JSON",
		RunE:  func(*cobra.Command, []string) error { return daemonGet("/api/dashboard/layout") },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Set raw dashboard layout JSON (reads from stdin)",
		RunE: func(_ *cobra.Command, _ []string) error {
			var body json.RawMessage
			if err := json.NewDecoder(os.Stdin).Decode(&body); err != nil {
				return fmt.Errorf("invalid JSON: %w", err)
			}
			return daemonJSON(http.MethodPut, "/api/dashboard/layout", body)
		},
	})
	return cmd
}
