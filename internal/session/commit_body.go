package session

import (
	"fmt"
	"strings"
)

// commitBodyMaxFiles caps how many file lines commitBody lists before it
// summarises the rest as "...and N more".
const commitBodyMaxFiles = 50

// commitBody builds the body of an automatic task commit: the changed files
// (one per line, capped) followed by the verification result. It is pure
// string work and makes no git calls.
func commitBody(files []string, verification string) string {
	var kept []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		kept = append(kept, f)
	}

	var b strings.Builder
	if len(kept) == 0 {
		b.WriteString("Files changed: none")
	} else {
		b.WriteString("Files changed:")
		for i, f := range kept {
			if i == commitBodyMaxFiles {
				break
			}
			b.WriteString("\n- " + f)
		}
		if extra := len(kept) - commitBodyMaxFiles; extra > 0 {
			fmt.Fprintf(&b, "\n- ...and %d more", extra)
		}
	}

	b.WriteString("\n\n")
	if v := strings.TrimSpace(verification); v != "" {
		b.WriteString("Verification: " + v)
	} else {
		b.WriteString("Verification: not run before this commit")
	}
	return b.String()
}
