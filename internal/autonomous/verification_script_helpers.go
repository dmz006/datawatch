// Shared helper contract for generated verification scripts. The text is
// static and size-bounded so the planning prompt and its tests share one
// copy. It never names a project, home folder or tracker id, and carries
// no fixed release number.
package autonomous

// verificationScriptHelperMaxBytes bounds the helper text so the planning
// prompt stays small whatever the repo holds.
const verificationScriptHelperMaxBytes = 2000

// verificationScriptHelperText is the helper every generated script must
// follow. It is written as numbered rules continuing after rule 5 of the
// verification-scripts block (numbering starts at 6).
const verificationScriptHelperText = "6. Docs sync first. A script that checks docs, the embedded docs copy" +
	" (internal/server/web/docs) or how-to coverage must run `make sync-docs`" +
	" itself as its first step, so the embedded copy matches the source docs.\n" +
	"7. Cumulative allowed files. The allowed-file list is cumulative from" +
	" the start: it allows every file any story of the Automaton may touch," +
	" the standard tasks' generated files (changelog draft, parity issue" +
	" draft and its hash file, docs, the registry plan files), gofmt targets" +
	" and docs files. Never build it from one task's own file list.\n" +
	"8. Merge-base comparison. Every comparison is against the merge base:" +
	" `git diff --name-only $(git merge-base HEAD main)` plus the untracked" +
	" files — never against the working tree, the current main, or one" +
	" task's own file list.\n" +
	"9. Never run gofmt over a whole package or directory; run it only on the" +
	" listed changed Go files.\n"

// VerificationScriptHelperText returns the shared helper rules text.
func VerificationScriptHelperText() string {
	return verificationScriptHelperText
}
