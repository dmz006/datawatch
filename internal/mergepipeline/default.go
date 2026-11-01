package mergepipeline

import "time"

// DefaultDefinition returns the built-in pipeline. Its steps reproduce the
// current merge flow in the same order:
//
//  1. check-branch-versions: before anything is merged, every branch must pass
//     the version check (the branch either left the version alone or already
//     carries the expected next one). Any failure stops the pipeline with
//     nothing merged.
//  2. check-internal-refs: lint the checked-out main tree for internal
//     reference leaks, only where the project defines that make target. A
//     failure stops the pipeline before any merge.
//  3. merge-branches: merge each branch in order with --no-ff; a conflicting
//     merge is aborted and stops the pipeline.
//  4. set-release-version: set the next patch version, one above main's, in
//     both version-carrying files.
//  5. insert-changelog-heading: add the release heading under the unreleased
//     heading of the changelog, unless it is already there.
//  6. commit-release-bump: stage only the two version files and the changelog
//     and commit once, if anything is staged.
//  7. tag-release: tag the bump commit with the new version.
//  8. push-release: push main and the tag.
//  9. deploy-latest: restart the local daemon on the new build.
//
// Steps 4 to 6 are one script run today; they are separate steps so a project
// can replace any of them.
func DefaultDefinition() Definition {
	return Definition{Steps: []Step{
		{Name: "check-branch-versions", Kind: KindCustom, Action: ActionCheckBranchVersions, OnFailure: OnFailureStop, Timeout: 2 * time.Minute},
		{Name: "check-internal-refs", Kind: KindLint, Command: "make check-internal-refs", OnFailure: OnFailureStop, Timeout: 10 * time.Minute},
		{Name: "merge-branches", Kind: KindShip, Action: ActionMergeBranches, OnFailure: OnFailureStop, Timeout: 5 * time.Minute},
		{Name: "set-release-version", Kind: KindVersionBump, Action: ActionSetReleaseVersion, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "insert-changelog-heading", Kind: KindChangelog, Action: ActionInsertChangelog, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "commit-release-bump", Kind: KindShip, Action: ActionCommitReleaseBump, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "tag-release", Kind: KindTag, Action: ActionTagRelease, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "push-release", Kind: KindShip, Action: ActionPushRelease, OnFailure: OnFailureStop, Timeout: 5 * time.Minute},
		{Name: "deploy-latest", Kind: KindDeploy, Action: ActionDeployLatest, OnFailure: OnFailureContinue, Timeout: 10 * time.Minute},
	}}
}
