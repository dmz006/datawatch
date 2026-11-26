// A replay is the shadow of one hand merge: datawatch builds the same merge
// with its own merge engine, in shadow mode, from the hand merge's base and
// branch tip, and records what it saw. The record holds facts only; the seven
// results, the verdict and the streak are worked out from them each time they
// are read, so a hand check result recorded later counts at once.
//
// Replays are the only thing the turn-on streak counts. The finish-time plans
// and comparisons stored beside them never count.
package mergeshadow

import "time"

// The states of a replay.
const (
	ReplayQueued  = "queued"
	ReplayRunning = "running"
	ReplayDone    = "done"
	// ReplayError means the shadow could not be run; Error says why.
	ReplayError = "error"
)

// ReplaySelection is the merge selection rule's answer for the commit the hand
// merge merged.
type ReplaySelection = PlanSelection

// HandMerge is what the hand merge did, read from git.
type HandMerge struct {
	// Commit is the commit the shipped record names; Base the main commit the
	// hand merge started from; Tip the branch commit it merged.
	Commit  string      `json:"commit"`
	Base    string      `json:"base,omitempty"`
	Tip     string      `json:"tip,omitempty"`
	Release string      `json:"release,omitempty"`
	At      time.Time   `json:"at"`
	Version string      `json:"version,omitempty"`
	Size    ReleaseSize `json:"size,omitempty"`
	// Tags are the tags on commits the hand merge added to main.
	Tags []string `json:"tags,omitempty"`
	// Conflicts are the files git reports in conflict for Base and Tip.
	Conflicts []string `json:"conflicts"`
}

// ReplayCheck is one lint or test step of the pipeline in the shadow run.
type ReplayCheck struct {
	Name string `json:"name"`
	// Kind is lint or test.
	Kind string `json:"kind"`
	// Result is ok, failed, skipped or not_run.
	Result string `json:"result"`
	// Why is the first line of a failed step's account, such as the limit
	// that ended it.
	Why string `json:"why,omitempty"`
}

// ReplayStepNotRun is a step of the pipeline, other than a lint or test
// step, that the shadow did not run.
type ReplayStepNotRun struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Why  string `json:"why"`
}

// ReplayPushRef is one ref the shadow run would have pushed.
type ReplayPushRef struct {
	Ref     string `json:"ref"`
	New     string `json:"new"`
	Commits int    `json:"commits"`
}

// DaemonMerge is what the shadow run of the merge engine did.
type DaemonMerge struct {
	RunID  string `json:"run_id,omitempty"`
	Result string `json:"result,omitempty"`
	Reason string `json:"reason,omitempty"`
	// StoppedAt is the step that failed, with the first line of its account.
	StoppedAt string          `json:"stopped_at,omitempty"`
	Version   string          `json:"version,omitempty"`
	Size      ReleaseSize     `json:"size,omitempty"`
	Candidate string          `json:"candidate,omitempty"`
	Push      []ReplayPushRef `json:"push,omitempty"`
	Checks    []ReplayCheck   `json:"checks"`
	// NotRun are the steps the shadow did not run that keep the merge from
	// matching: any other command step, any built-in step left out for a
	// missing make target, and a built-in check left out for any reason.
	NotRun    []ReplayStepNotRun `json:"not_run,omitempty"`
	Conflicts []string           `json:"conflicts"`
	// Asked says a conflict would have been put to the operator.
	Asked   bool   `json:"asked,omitempty"`
	AskNote string `json:"ask_note,omitempty"`
}

// ReplayContent compares the two final trees.
type ReplayContent struct {
	Compared bool `json:"compared"`
	// Differences are the files that differ beyond the allowed list.
	Differences []string `json:"differences"`
	// Allowed are the files that differ only in what may differ.
	Allowed []string `json:"allowed,omitempty"`
}

// ReplayLeak is what the push checks found in what the shadow would push.
type ReplayLeak struct {
	Evaluated          bool   `json:"evaluated"`
	ProjectCheckRan    bool   `json:"project_check_ran"`
	ProjectCheckPassed bool   `json:"project_check_passed"`
	ProjectCheckOutput string `json:"project_check_output,omitempty"`
	// PrivatePatterns is how many private patterns the project's check said it
	// loaded; -1 when it did not say.
	PrivatePatterns int    `json:"private_patterns"`
	SecretScanRan   bool   `json:"secret_scan_ran"`
	SecretFindings  int    `json:"secret_findings"`
	Refusal         string `json:"refusal,omitempty"`
}

// ReplaySideEffects is what changed around the shadow run.
type ReplaySideEffects struct {
	Checked bool `json:"checked"`
	// Found are changes the shadow run made, or that cannot be told apart
	// from one it made.
	Found []string `json:"found"`
	// Elsewhere are changes to the real repository or remote made by someone
	// else while the shadow ran, listed apart.
	Elsewhere []string `json:"elsewhere,omitempty"`
}

// Replay is the stored shadow of one hand merge.
type Replay struct {
	Key         string    `json:"key"`
	AutomatonID string    `json:"automaton_id"`
	Name        string    `json:"name,omitempty"`
	Kind        string    `json:"kind,omitempty"`
	Project     string    `json:"project"`
	Branch      string    `json:"branch"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	State       string    `json:"state"`
	Error       string    `json:"error,omitempty"`
	// SettingsHash covers the pipeline and merge settings the shadow ran with;
	// ChecksHash the check files of the merge it built.
	SettingsHash string `json:"settings_hash,omitempty"`
	// CommandLimit is the memory limit the shadow's commands ran under.
	CommandLimit string   `json:"command_limit,omitempty"`
	ChecksHash   string   `json:"checks_hash,omitempty"`
	CheckFiles   []string `json:"check_files,omitempty"`

	Selection   *ReplaySelection  `json:"selection,omitempty"`
	Hand        HandMerge         `json:"hand"`
	Daemon      DaemonMerge       `json:"daemon"`
	Content     ReplayContent     `json:"content"`
	Leak        ReplayLeak        `json:"leak"`
	SideEffects ReplaySideEffects `json:"side_effects"`

	// HandChecks are the hand merge's recorded check results, stored apart and
	// filled in when the replay is read.
	HandChecks map[string]string `json:"hand_checks,omitempty"`
}
