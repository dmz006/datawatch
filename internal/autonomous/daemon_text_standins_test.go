package autonomous

// The rules-file fixtures in these tests use short stand-ins for the daemon's
// sections. The protected-path check clears only text the daemon writes, so a
// rig that uses a stand-in declares it, through the same hook the daemon wires.

// gateStandIns are the sections of daemonSectionsBlock.
var gateStandIns = []string{
	"<!-- datawatch:platform-context v1 -->\nThis session runs on datawatch.\n<!-- /datawatch:platform-context -->",
	"<!-- rtk-instructions -->\nAlways prefix commands with rtk.\n<!-- /rtk-instructions -->",
	"<!-- cross-session-communication-rule -->\nUse datawatch tools for durable facts.\n<!-- /cross-session-communication-rule -->",
	"# Memory & Knowledge (datawatch)\n\nUse memory_recall before answering project questions.",
	"# Session Guardrails\n\nNever stop running sessions.",
}

// dscStandIns are the sections of dscDaemonOnlyRules.
var dscStandIns = []string{
	"# Memory & Knowledge (datawatch)\n\nUse the memory tools first.",
	"<!-- rtk-instructions -->\nalways prefix with rtk\n<!-- /rtk-instructions -->",
}

// dsmStandIns are the sections of dsmDaemon.
var dsmStandIns = []string{
	"<!-- rtk-instructions -->\nAlways prefix commands with rtk.\n<!-- /rtk-instructions -->",
	"# Memory & Knowledge (datawatch)\n\n- Use the memory system proactively.",
}

func standInDaemonText(blocks []string) func(string) []string {
	return func(string) []string { return blocks }
}
