// Command outgoing-check runs, by hand, the checks datawatch runs before it
// pushes: what the push would send is scanned for secrets and handed to the
// project's check-outgoing target from the base commit. It sends nothing.
//
// Run it with `make outgoing-check [REF=HEAD] [DEST=refs/heads/x] [REMOTE=origin]`.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dmz006/datawatch/internal/git"
)

func main() {
	ref := flag.String("ref", "HEAD", "local revision that would be pushed")
	dest := flag.String("dest", "", "destination ref on the remote (default: the current branch)")
	remote := flag.String("remote", "origin", "remote to compare with")
	flag.Parse()
	ctx := context.Background()
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "outgoing-check:", err)
		os.Exit(2)
	}
	if *dest == "" {
		branch, err := git.CurrentBranch(ctx, dir)
		if err != nil || branch == "" {
			fmt.Fprintln(os.Stderr, "outgoing-check: detached HEAD; name the destination with DEST=refs/heads/<branch>")
			os.Exit(2)
		}
		*dest = branch
	}
	if !strings.HasPrefix(*dest, "refs/") {
		*dest = "refs/heads/" + *dest
	}
	res := git.CheckOutgoing(ctx, dir, *remote, []git.OutgoingRef{{Src: *ref, Dst: *dest}}, git.SecretScanPolicy{Mode: git.ScanEnforce})
	if res.Kind != "" {
		fmt.Printf("REFUSED (%s): %s\n", res.Kind, res.Reason)
		os.Exit(1)
	}
	if res.NothingToPush {
		fmt.Printf("nothing would leave: %s on %s already has %s\n", *dest, *remote, res.SHA)
		return
	}
	fmt.Printf("passed: %s (%s) may go to %s %s\n", *ref, res.SHA, *remote, *dest)
}
