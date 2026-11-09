// GitLab release creation — shells out to `glab release create`.

package git

import (
	"context"
	"fmt"
	"strings"
)

// CreateRelease shells out to `glab release create <tag>` and returns
// the release URL glab prints on stdout. It is not part of the Provider
// interface; callers hold a *GitLab directly.
func (g *GitLab) CreateRelease(ctx context.Context, opts ReleaseOptions) (string, error) {
	if opts.Repo == "" {
		return "", fmt.Errorf("CreateRelease: Repo required")
	}
	if opts.Tag == "" {
		return "", fmt.Errorf("CreateRelease: Tag required")
	}
	args := []string{
		"release", "create", opts.Tag,
		"--repo", opts.Repo,
		"--name", opts.Title,
		"--notes", opts.Notes,
	}
	stdout, combined, err := g.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("glab release create: %w\n%s", err, strings.TrimSpace(combined))
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://") {
			return line, nil
		}
	}
	return "", fmt.Errorf("glab release create: no release URL in output:\n%s", strings.TrimSpace(stdout))
}
