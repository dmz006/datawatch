package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// judgeVerdictWithSession returns a copy of v stamped with the one-shot judge
// session that produced it and the judge output file that backed it. It is
// pure: v is passed by value, so the caller's verdict is never mutated. An
// empty sessionID leaves JudgeSessionID empty, which is how a text-only ask
// verdict (no session) reports itself.
func judgeVerdictWithSession(v autonomouspkg.GuardrailVerdict, sessionID, outputPath string) autonomouspkg.GuardrailVerdict {
	v.JudgeSessionID = sessionID
	v.JudgeOutputPath = outputPath
	return v
}
