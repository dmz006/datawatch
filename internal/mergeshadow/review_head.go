package mergeshadow

import (
	"context"
	"fmt"
	"strings"
)

// ReviewStaleCode is the finding code for a review verdict that does not name
// the head of the Automaton branch. The review examined some other commit, so
// its verdict says nothing about the work the merge would land.
const ReviewStaleCode = "review_stale"

// ReviewVerdict is a review's verdict together with the commit it reviewed.
// The commit is what makes the verdict checkable: a verdict that names no
// commit, or a commit other than the branch head, is stale.
type ReviewVerdict struct {
	// Commit is the commit the review examined, as a SHA or any ref git can
	// resolve to a commit.
	Commit string
}

// CheckReviewHead compares the commit a review named with the head of the
// Automaton branch branch in repoPath. It returns one blocking review_stale
// finding when the two differ, when the review names no commit, or when the
// named commit does not exist in the repository. A review of the current head
// produces no finding.
//
// Nothing is changed in the repository. A git failure other than an unknown
// commit or a missing branch is an error.
func CheckReviewHead(ctx context.Context, repoPath, branch, reviewed string) ([]Finding, error) {
	if repoPath == "" || branch == "" {
		return nil, fmt.Errorf("mergeshadow: repo path and branch are required")
	}
	if strings.HasPrefix(branch, "-") {
		return nil, fmt.Errorf("mergeshadow: invalid branch name %q", branch)
	}
	reviewed = strings.TrimSpace(reviewed)
	if reviewed == "" {
		return []Finding{{
			Code:     ReviewStaleCode,
			Message:  "review names no commit; the verdict cannot be tied to the branch head",
			Blocking: true,
		}}, nil
	}
	if strings.HasPrefix(reviewed, "-") {
		return nil, fmt.Errorf("mergeshadow: invalid review commit %q", reviewed)
	}

	head, err := runGit(ctx, repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("mergeshadow: resolve head of %s: %w", branch, err)
	}
	head = strings.TrimSpace(head)

	named, err := runGit(ctx, repoPath, "rev-parse", "--verify", "--quiet", reviewed+"^{commit}")
	if err != nil {
		return []Finding{{
			Code:     ReviewStaleCode,
			Message:  "review names commit " + reviewed + ", which is not in the repository",
			Blocking: true,
		}}, nil
	}
	named = strings.TrimSpace(named)

	if named == head {
		return nil, nil
	}
	return []Finding{{
		Code: ReviewStaleCode,
		Message: "review named commit " + named + " but " + branch +
			" is at " + head + "; the review predates the current head",
		Blocking: true,
	}}, nil
}
