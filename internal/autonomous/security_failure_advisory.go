// Remote advisory lookup for a security-failure run. One advisory
// identifier (GHSA-… or CVE-…) is resolved to the fields an acceptance
// judgment needs: the advisory's published date and the first patched
// version.
//
// The production implementation (defaultSecurityFailureAdvisoryLookup) is a
// read-only `gh api` GET, resolved by name from PATH:
//
//   - a GHSA id uses the advisories endpoint keyed by that id:
//     `gh api advisories/GHSA-…`
//   - a CVE id uses the advisories listing filtered by cve_id:
//     `gh api advisories?cve_id=CVE-…`
//
// Only the advisory identifier is placed on the command line — the
// caller's repository path, package, version, and any other local state
// never becomes an argument, so no local data leaves the host either via
// arguments or via the request.
//
// Every failure mode — gh not on PATH, the process failing, an empty
// response, or a non-JSON response — yields an unknown result: empty
// PublishedDate and FixedVersion with the error text kept in ErrText.
// The lookup never returns an error to the caller and never invents a
// date or version it did not receive.
//
// The seam, like the repo-CI-watch seams in repo_ci_watch_actions.go, is a
// function variable: nil by default (production implementation) and a
// test-injectable implementation when set; callers go through
// LookupSecurityAdvisory and never call the implementation directly.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// SecurityFailureAdvisoryResult is one advisory lookup's answer. An unknown
// result — gh absent, the process failed, or the response was empty or
// non-JSON — leaves PublishedDate and FixedVersion empty and keeps the
// reason in ErrText.
type SecurityFailureAdvisoryResult struct {
	PublishedDate string
	FixedVersion  string
	ErrText       string
}

// securityFailureAdvisoryFn is the nil-seam (same pattern as the repo
// CI-watch seams): nil means "use the production implementation"; a
// non-nil value is consulted first, and that is what tests inject.
var (
	securityFailureAdvisoryMu sync.RWMutex
	securityFailureAdvisoryFn func(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult
)

// setSecurityFailureAdvisoryFn points the seam at fn for the duration of
// the caller and returns a restore function that puts the previous
// implementation back.
func setSecurityFailureAdvisoryFn(fn func(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult) (restore func()) {
	securityFailureAdvisoryMu.Lock()
	old := securityFailureAdvisoryFn
	securityFailureAdvisoryFn = fn
	securityFailureAdvisoryMu.Unlock()
	return func() {
		securityFailureAdvisoryMu.Lock()
		securityFailureAdvisoryFn = old
		securityFailureAdvisoryMu.Unlock()
	}
}

// LookupSecurityAdvisory resolves advisoryID to its published date and the
// first patched version. It never fails the caller: every error is folded
// into an unknown SecurityFailureAdvisoryResult (empty date/version, error
// text kept).
func LookupSecurityAdvisory(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult {
	securityFailureAdvisoryMu.RLock()
	fn := securityFailureAdvisoryFn
	securityFailureAdvisoryMu.RUnlock()
	if fn != nil {
		// #nosec G602 -- the seam is a trusted in-process test double;
		// production always takes the default below.
		return fn(ctx, advisoryID)
	}
	return defaultSecurityFailureAdvisoryLookup(ctx, advisoryID)
}

var (
	ghsaIDPat = regexp.MustCompile(`^GHSA-[0-9a-z]{4}-[0-9a-z]{4}-[0-9a-z]{4}$`)
	cveIDPat  = regexp.MustCompile(`^CVE-\d{4}-\d{4,7}$`)
)

// advisoryFields is the slice of the advisories API object this lookup
// needs, in both the single-advisory ("advisories/<ghsa>") and listing
// ("advisories?cve_id=<cve>") response shapes.
type advisoryFields struct {
	PublishedAt   string `json:"published_at"`
	Vulnerability []struct {
		FirstPatchedVersion string `json:"first_patched_version"`
	} `json:"vulnerabilities"`
}

// defaultSecurityFailureAdvisoryLookup is the production implementation: a
// GET-only `gh api` whose command line carries only the validated advisory
// identifier.
func defaultSecurityFailureAdvisoryLookup(ctx context.Context, advisoryID string) SecurityFailureAdvisoryResult {
	id := strings.TrimSpace(advisoryID)
	var apiPath string
	switch {
	case ghsaIDPat.MatchString(id):
		apiPath = "advisories/" + id
	case cveIDPat.MatchString(id):
		apiPath = "advisories?cve_id=" + id
	default:
		return unknownAdvisoryResult(fmt.Sprintf("advisory id %q is neither a GHSA nor a CVE id", advisoryID))
	}
	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return unknownAdvisoryResult("gh not found on PATH: " + err.Error())
	}
	cmd := exec.CommandContext(ctx, ghBin, "api", apiPath) // #nosec G204 -- apiPath is the validated identifier, see above
	out, err := cmd.Output()
	if err != nil {
		return unknownAdvisoryResult(ghErrorText(cmd, err))
	}
	if strings.TrimSpace(string(out)) == "" {
		return unknownAdvisoryResult("empty gh response")
	}
	advs, parseErr := advisoryFieldsFromResponse(out)
	if parseErr != nil {
		return unknownAdvisoryResult("non-JSON gh response: " + parseErr.Error())
	}
	if len(advs) == 0 {
		return unknownAdvisoryResult("advisories listing returned no advisories")
	}
	first := advs[0]
	return SecurityFailureAdvisoryResult{
		PublishedDate: first.PublishedAt,
		FixedVersion:  firstAdvisoryFixedVersion(first),
	}
}

func unknownAdvisoryResult(reason string) SecurityFailureAdvisoryResult {
	return SecurityFailureAdvisoryResult{ErrText: reason}
}

func ghErrorText(cmd *exec.Cmd, err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return fmt.Sprintf("%s failed: %s", cmd.Path, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return fmt.Sprintf("%s failed: %v", cmd.Path, err)
}

// advisoryFieldsFromResponse accepts either response shape: the single
// advisory object of "advisories/<ghsa>" or the (possibly empty) list of
// "advisories?cve_id=<cve>". Anything that is neither is an error.
func advisoryFieldsFromResponse(body []byte) ([]advisoryFields, error) {
	var single advisoryFields
	if err := json.Unmarshal(body, &single); err == nil {
		return []advisoryFields{single}, nil
	}
	var listing []advisoryFields
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}
	return listing, nil
}

func firstAdvisoryFixedVersion(a advisoryFields) string {
	// The listing is ordered per the advisories API; the first entry with
	// a patched version is the first patched version.
	for _, v := range a.Vulnerability {
		if v.FirstPatchedVersion != "" {
			return v.FirstPatchedVersion
		}
	}
	return ""
}
