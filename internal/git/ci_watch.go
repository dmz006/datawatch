package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ciWatchTimeout bounds each CI query, so a hung CLI cannot stall the watcher.
const ciWatchTimeout = 15 * time.Second

// CIRun is one CI run or pipeline of a repository ref, as reported by the host.
type CIRun struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	SHA        string `json:"sha"`
	URL        string `json:"url"`
}

// LatestRuns returns the most recent CI runs for repo on ref. GitHub hosts are
// queried with gh run list; GitLab hosts with glab api against the project's
// pipelines. The call is bounded by a 15 second deadline derived from ctx.
// It never returns a token.
func LatestRuns(ctx context.Context, host GitHostConfig, repo, ref string) (runs []CIRun, err error) {
	defer func() { RecordHostOp(HostOpCIRead, err == nil) }()
	repo = strings.TrimSpace(repo)
	ref = strings.TrimSpace(ref)
	if repo == "" {
		return nil, errors.New("ci watch: repo is required")
	}
	if ref == "" {
		return nil, errors.New("ci watch: ref is required")
	}
	cctx, cancel := context.WithTimeout(ctx, ciWatchTimeout)
	defer cancel()

	switch host.Kind {
	case "github":
		out, err := runCICmd(cctx, "gh", "run", "list",
			"--repo", repo,
			"--branch", ref,
			"--json", "databaseId,name,status,conclusion,headSha,url")
		if err != nil {
			return nil, ciCmdError("gh run list", err)
		}
		return parseGitHubRuns(out)
	case "gitlab":
		path := "projects/" + url.PathEscape(repo) + "/pipelines?ref=" + url.QueryEscape(ref)
		out, err := runCICmd(cctx, "glab", "api", path)
		if err != nil {
			return nil, ciCmdError("glab api pipelines", err)
		}
		return parseGitLabPipelines(out)
	default:
		return nil, fmt.Errorf("ci watch: host kind %q has no CI watch", host.Kind)
	}
}

// runCICmd runs name with args under ctx and returns stdout. Stderr is kept
// out of the result so a CLI error message cannot leak into the caller's data.
func runCICmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...) // #nosec G204 -- fixed binary names, args built from validated config
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, err
}

func ciCmdError(what string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("ci watch: %s is not installed", what)
	}
	return fmt.Errorf("ci watch: %s failed: %w", what, err)
}

type ghRunJSON struct {
	DatabaseID int64  `json:"databaseId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HeadSHA    string `json:"headSha"`
	URL        string `json:"url"`
}

func parseGitHubRuns(out []byte) ([]CIRun, error) {
	var raw []ghRunJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ci watch: decode gh run list: %w", err)
	}
	runs := make([]CIRun, 0, len(raw))
	for _, r := range raw {
		runs = append(runs, CIRun{
			ID:         strconv.FormatInt(r.DatabaseID, 10),
			Name:       r.Name,
			Status:     r.Status,
			Conclusion: r.Conclusion,
			SHA:        r.HeadSHA,
			URL:        r.URL,
		})
	}
	return runs, nil
}

type glPipelineJSON struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
	SHA    string `json:"sha"`
	WebURL string `json:"web_url"`
}

func parseGitLabPipelines(out []byte) ([]CIRun, error) {
	var raw []glPipelineJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("ci watch: decode glab pipelines: %w", err)
	}
	runs := make([]CIRun, 0, len(raw))
	for _, p := range raw {
		runs = append(runs, CIRun{
			ID:     strconv.FormatInt(p.ID, 10),
			Status: p.Status,
			SHA:    p.SHA,
			URL:    p.WebURL,
		})
	}
	return runs, nil
}
