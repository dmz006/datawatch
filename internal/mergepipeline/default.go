package mergepipeline

import "time"

// DefaultDefinition returns the built-in pipeline. Its steps reproduce the
// current merge flow in the same order:
//
//  1. rebase-onto-main: fetch the main branch when the project has a remote,
//     then rebase each branch onto the local main in a throwaway worktree. A
//     branch already based on main is left alone; a conflicting branch stops
//     the pipeline with that branch untouched.
//  2. check-branch-versions: before anything is merged, every branch must pass
//     the version check (the branch either left the version alone or already
//     carries the expected next one). Any failure stops the pipeline with
//     nothing merged.
//  3. check-internal-refs: lint the checked-out main tree for internal
//     reference leaks, only where the project defines that make target
//     (the step's RequiresMakeTarget; the runner skips it otherwise). A
//     failure stops the pipeline before any merge.
//  4. merge-branches: merge each branch in order with --no-ff; a conflicting
//     merge is aborted and stops the pipeline.
//  5. set-release-version: set the next patch version, one above main's, in
//     both version-carrying files.
//  6. insert-changelog-heading: add the release heading under the unreleased
//     heading of the changelog, unless it is already there.
//  7. set-readme-release-line: rewrite the version and date on the README's
//     Current release: line to the release version and today's date. A line
//     that already names the version is left alone. A README with no such
//     line stops the pipeline.
//  8. commit-release-bump: stage only the two version files, the changelog
//     and the README, and commit once, if anything is staged.
//  9. tag-release: tag the bump commit with the new version.
//  10. validate-release-bump: before anything leaves the machine, check the
//     release bump (version files, changelog heading, one step above the
//     newest tag, tag free on the remote, README line). Any failing check
//     stops the pipeline, so push-release never runs.
//
// 11. push-release: push main and the tag.
//
// 12. deploy-latest: restart the local daemon on the new build.
//
// Steps 4 to 6 are one script run today; they are separate steps so a project
// can replace any of them.
//
// When merge-branches finds every branch already in main, nothing new is
// released: steps 5 to 10 are recorded as skipped and nothing is pushed. The
// deploy step still runs.
func DefaultDefinition() Definition {
	return Definition{Steps: []Step{
		{Name: "rebase-onto-main", Kind: KindCustom, Action: ActionRebaseOntoMain, OnFailure: OnFailureStop, Timeout: 5 * time.Minute},
		{Name: "check-branch-versions", Kind: KindCustom, Action: ActionCheckBranchVersions, OnFailure: OnFailureStop, Timeout: 2 * time.Minute},
		{Name: "check-internal-refs", Kind: KindLint, Command: "make check-internal-refs", RequiresMakeTarget: "check-internal-refs", OnFailure: OnFailureStop, Timeout: 10 * time.Minute},
		{Name: "merge-branches", Kind: KindShip, Action: ActionMergeBranches, OnFailure: OnFailureStop, Timeout: 5 * time.Minute},
		{Name: "set-release-version", Kind: KindVersionBump, Action: ActionSetReleaseVersion, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "insert-changelog-heading", Kind: KindChangelog, Action: ActionInsertChangelog, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "set-readme-release-line", Kind: KindVersionBump, Action: ActionSetReadmeReleaseLine, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "commit-release-bump", Kind: KindShip, Action: ActionCommitReleaseBump, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "tag-release", Kind: KindTag, Action: ActionTagRelease, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "validate-release-bump", Kind: KindCustom, Action: ActionValidateReleaseBump, OnFailure: OnFailureStop, Timeout: time.Minute},
		{Name: "push-release", Kind: KindShip, Action: ActionPushRelease, OnFailure: OnFailureStop, Timeout: 5 * time.Minute},
		{Name: "deploy-latest", Kind: KindDeploy, Action: ActionDeployLatest, OnFailure: OnFailureContinue, Timeout: 10 * time.Minute},
	}}
}
