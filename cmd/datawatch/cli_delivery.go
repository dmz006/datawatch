// CLI for the delivery check.

package main

import "github.com/spf13/cobra"

// newDeliveryCmd is "datawatch delivery".
func newDeliveryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delivery",
		Short: "List and check work that has not been delivered or finished",
	}
}
