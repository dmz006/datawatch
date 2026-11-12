package autonomous

import "fmt"

// releasePushRefusal reports whether an unattended push to the default
// branch must be refused. The daemon may push the default branch on its
// own only once git.secret_scan.mode is "enforce"; with "shadow" or "off"
// an unattended push of the default branch is refused. Attended pushes
// and pushes to any other branch are not affected by this rule.
//
// It returns nil when the push is allowed and a descriptive error when it
// is refused. This function is not wired into any push path here; the
// direct push task calls it.
func releasePushRefusal(secretScanMode string, branch string, defaultBranch string, unattended bool) error {
	if !unattended || branch != defaultBranch {
		return nil
	}
	if secretScanMode == "enforce" {
		return nil
	}
	return fmt.Errorf(
		"refusing unattended push of default branch %q: the daemon refuses to push the default branch on its own until git.secret_scan.mode is enforce (current mode %q)",
		defaultBranch, secretScanMode,
	)
}
