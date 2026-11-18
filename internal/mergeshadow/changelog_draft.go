package mergeshadow

import (
	"context"
)

// ChangelogSourceDraft and ChangelogSourceBranch are the changelog_source
// values used when the entry came from the branch rather than an entry task:
// the generated changelog_draft artefact, or the new top entry of CHANGELOG.md.
const (
	ChangelogSourceDraft  = "draft"
	ChangelogSourceBranch = "branch"
)

// readBranchChangelogEntry is the fallback used when no entry task output is
// usable. It reads the generated changelog draft at draftPath on the branch
// and, if that holds no well-formed entry, the top entry CHANGELOG.md gains on
// the branch relative to base. Nothing is invented: when neither yields an
// entry the result is the missing entry. Files are read with git show, never
// from the working tree.
func readBranchChangelogEntry(ctx context.Context, repoPath, base, branch, draftPath string) (ChangelogEntry, error) {
	missing := ChangelogEntry{Source: ChangelogSourceMissing}
	if repoPath == "" || branch == "" {
		return missing, nil
	}
	if draftPath != "" {
		text, ok, err := refFile(ctx, repoPath, branch, draftPath)
		if err != nil {
			return missing, err
		}
		if ok {
			if h, b, good := parseChangelogEntry(text); good {
				return ChangelogEntry{Source: ChangelogSourceDraft, Heading: h, Body: b}, nil
			}
		}
	}
	text, ok, err := refFile(ctx, repoPath, branch, DefaultChangelogFile)
	if err != nil || !ok {
		return missing, err
	}
	h, b, good := parseChangelogEntry(text)
	if !good {
		return missing, nil
	}
	if baseText, bok, berr := refFile(ctx, repoPath, base, DefaultChangelogFile); berr == nil && bok {
		if findHeading(baseText).Value == h {
			return missing, nil // the branch adds no new top entry
		}
	}
	return ChangelogEntry{Source: ChangelogSourceBranch, Heading: h, Body: b}, nil
}
