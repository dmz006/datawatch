// The merge run and deploy run request switch: its key and the refusal every
// surface shows while it is off.

package config

// MergeDeployRunKey is the setting that must be true for a merge run request
// or a deploy run request to act.
const MergeDeployRunKey = "autonomous.merge_deploy_run_enabled"

// MergeDeployRunRefusal is the answer to a merge run or deploy run request
// while the setting is off. REST sends it as the body; the CLI, the MCP tool,
// the chat verb and the web app show that body.
const MergeDeployRunRefusal = "merge run and deploy run are switched off, so nothing was run: the setting " +
	MergeDeployRunKey + " is false. To allow them, set it to true (datawatch config set " + MergeDeployRunKey + " true)."
