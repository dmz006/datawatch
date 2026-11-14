// Shared helper contract for generated verification scripts. The text is
// static and size-bounded so the planning prompt and its tests share one
// copy. It never names a project, home folder or tracker id, and carries
// no fixed release number.
package autonomous

// verificationScriptHelperMaxBytes bounds the helper text so the planning
// prompt stays small whatever the repo holds.
const verificationScriptHelperMaxBytes = 3200

// verificationScriptHelperText is the helper every generated script must
// follow. It is written as numbered rules continuing after rule 5 of the
// verification-scripts block (numbering starts at 6).
const verificationScriptHelperText = "6. Docs sync first. A script that checks docs, the embedded docs copy" +
	" (internal/server/web/docs) or how-to coverage must run `make sync-docs`" +
	" itself as its first step, so the embedded copy matches the source docs.\n" +
	"7. One shared, cumulative allowed-file list. Build it once, from the" +
	" start: the task's own files plus the changelog, README, docs, locales" +
	" and tests, and it is cumulative across stories: it allows every file" +
	" any story of the Automaton may touch, the standard tasks' generated" +
	" files (changelog draft, parity issue draft and its hash file, docs," +
	" the registry plan files) and gofmt targets. Never build it from one" +
	" task's own file list.\n" +
	"8. Merge-base comparison. Every comparison is against the merge base:" +
	" `git diff --name-only $(git merge-base HEAD main)` plus the untracked" +
	" files — never against the working tree, the current main, or one" +
	" task's own file list.\n" +
	"9. Never run gofmt over a whole package or directory; run it only on the" +
	" listed changed Go files.\n" +
	"10. Shell. Start every script with `#!/usr/bin/env bash`; it is run with" +
	" bash, so write bash, not POSIX sh.\n" +
	"11. Tracker-id patterns never match CSS hex colours (#fff, #1a2b3c):" +
	" a letter-plus-digits pattern also matches a hex colour, so require the" +
	" match not to be preceded by `#` (or drop hex colours first).\n" +
	"12. Never pipe into `grep -q`: it exits at the first match and the" +
	" writer dies with SIGPIPE, which fails a pipefail script. Capture the" +
	" output in a variable or file first, then run `grep -q` on the file or" +
	" a here-string.\n" +
	"13. A story's gate script runs only that story's own scripts, never" +
	" another story's; only the single final_gate runs the checks of the" +
	" whole Automaton.\n"

// VerificationScriptHelperText returns the shared helper rules text.
func VerificationScriptHelperText() string {
	return verificationScriptHelperText
}
