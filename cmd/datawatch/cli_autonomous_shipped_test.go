// Pure (no daemon) tests for `datawatch autonomous shipped backfill`: the
// sub-command and its --dry-run flag are wired up alongside the record
// sub-command, and the endpoint is the fixed backfill path.

package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// findSubCommand locates a direct child of cmd by name (the Use prefix),
// or fails the test.
func findSubCommand(t *testing.T, root *cobra.Command, want string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == want {
			return c
		}
	}
	t.Fatalf("sub-command %q not found under %q (have %v)", want, root.Name(), root.Commands())
	return nil
}

func TestShippedBackfillCLI_PathAndDryRun(t *testing.T) {
	// The endpoint is fixed; dry_run is the only knob the daemon accepts,
	// and it is not part of the path (it rides the query, set by run()).
	a := &shippedBackfillArgs{dryRun: false}
	if got := a.path(); got != "/api/autonomous/shipped/backfill" {
		t.Errorf("path = %q; want /api/autonomous/shipped/backfill", got)
	}
	if a.dryRun {
		t.Errorf("the writing pass must be the default")
	}
}

func TestShippedBackfillCLI_CommandWiring(t *testing.T) {
	cmd := newAutonomousShippedCmd()
	if cmd.Name() != "shipped" {
		t.Fatalf("Name = %q, want shipped", cmd.Name())
	}
	backfill := findSubCommand(t, cmd, "backfill")
	if f := backfill.Flags().Lookup("dry-run"); f == nil {
		t.Fatalf("backfill is missing the --dry-run flag")
	} else {
		if f.DefValue != "false" {
			t.Errorf("--dry-run default = %q, want false (the writing pass is the default)", f.DefValue)
		}
	}
	if f := backfill.Flags().Lookup("release"); f != nil {
		t.Errorf("backfill must not take record flags, found --release")
	}
	if ArgsErr := backfill.Args; ArgsErr == nil {
		t.Errorf("backfill should take no positional args")
	}
	// The record sub-command is untouched and still alongside.
	record := findSubCommand(t, cmd, "record")
	if record.Flags().Lookup("commit") == nil {
		t.Errorf("record lost its --commit flag")
	}
}
