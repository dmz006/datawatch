package autonomous

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
)

// snapshotAddChunk caps how many paths one git add invocation receives, so a
// long candidate list cannot exceed the OS argument limit.
const snapshotAddChunk = 1000

// snapshotCandidates lists the repo-relative paths the attempt may have
// changed, read from the real index and HEAD rather than the temporary index:
// modified tracked files, staged files, and untracked files that .gitignore
// does not exclude. Rename detection is off, so a moved file contributes both
// its old path (deleted) and its new path (added). Output is NUL-separated, so
// names with spaces, quotes or non-ASCII bytes arrive unquoted.
func snapshotCandidates(ctx context.Context, top string) ([]string, error) {
	queries := [][]string{
		{"diff", "--name-only", "-z", "--no-renames", "HEAD"},
		{"diff", "--cached", "--name-only", "-z", "--no-renames"},
		{"ls-files", "-z", "--others", "--exclude-standard"},
	}
	seen := make(map[string]bool)
	var candidates []string
	for _, args := range queries {
		out, err := snapshotGitRaw(ctx, top, os.Environ(), args...)
		if err != nil {
			return nil, err
		}
		for _, f := range splitNUL(out) {
			if !seen[f] {
				seen[f] = true
				candidates = append(candidates, f)
			}
		}
	}
	return candidates, nil
}

// snapshotDropUnmatched removes candidates that are missing from the working
// tree and absent from the temporary index. git add rejects such a pathspec
// ("did not match any files"), and the snapshot tree already lacks the path,
// so there is nothing to record for it. Candidates missing from disk that
// HEAD tracks stay, because git add stages their deletion. The index listing
// is read only when some candidate is missing, so the common case costs no
// extra git call.
func snapshotDropUnmatched(ctx context.Context, top string, env []string, paths []string) ([]string, error) {
	missing := false
	for _, f := range paths {
		if _, err := os.Lstat(filepath.Join(top, f)); err != nil {
			missing = true
			break
		}
	}
	if !missing {
		return paths, nil
	}
	out, err := snapshotGitRaw(ctx, top, env, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	inIndex := make(map[string]bool)
	for _, f := range splitNUL(out) {
		inIndex[f] = true
	}
	kept := make([]string, 0, len(paths))
	for _, f := range paths {
		if _, err := os.Lstat(filepath.Join(top, f)); err == nil || inIndex[f] {
			kept = append(kept, f)
		}
	}
	return kept, nil
}

// snapshotAdd stages paths into the temporary index with git add, sharing one
// deadline of the policy's AddTimeout across all chunks (zero means no
// deadline). If an add fails or times out, the interrupted git process may
// leave temporary pack files behind, so snapshotAdd removes them with
// gitguard.CleanTempPacks and returns the error. The caller then creates no ref.
func snapshotAdd(ctx context.Context, top string, env []string, p gitguard.Policy, paths []string) error {
	if p.AddTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.AddTimeout)
		defer cancel()
	}
	for start := 0; start < len(paths); start += snapshotAddChunk {
		end := min(start+snapshotAddChunk, len(paths))
		args := append([]string{"add", "--"}, paths[start:end]...)
		if _, err := snapshotGit(ctx, top, env, args...); err != nil {
			cause := err
			if ctx.Err() != nil {
				cause = fmt.Errorf("timed out after %s: %w", p.AddTimeout, ctx.Err())
			}
			if removed, cerr := gitguard.CleanTempPacks(top); cerr != nil {
				fmt.Printf("[autonomous] warning: cleaning interrupted pack files in %s: %v\n", top, cerr)
			} else if len(removed) > 0 {
				fmt.Printf("[autonomous] removed %d interrupted pack file(s) in %s\n", len(removed), top)
			}
			return fmt.Errorf("attempt snapshot add in %s: %w", top, cause)
		}
	}
	return nil
}

// snapshotSkipSummary counts skipped candidates by reason, for example
// "ignore-dir=3, size=1". Reasons are sorted so the log line is stable.
func snapshotSkipSummary(skipped []gitguard.Skip) string {
	counts := make(map[string]int)
	for _, s := range skipped {
		counts[s.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		parts = append(parts, fmt.Sprintf("%s=%d", r, counts[r]))
	}
	return strings.Join(parts, ", ")
}

// splitNUL splits NUL-terminated git output into its non-empty entries.
func splitNUL(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
