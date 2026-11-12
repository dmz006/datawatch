// Backend version probe for ComputeNodes.
//
// After the reachability probe passes (see probe.go), the operator
// also wants to know WHICH backend version the node is actually
// running — Ollama reports it at GET /api/version and the Docker
// daemon at `docker info --format {{.ServerVersion}}` (already used
// by probeDocker). This file captures that version at save time and
// records it (with the probe timestamp) on the node record so the
// node detail payload (GET /api/compute/nodes/{name}) carries it.
//
// All I/O goes through injectable interfaces (VersionHTTPClient,
// VersionRunner) so tests run against fakes — no daemon, no real
// Ollama, no model pulls, no docker daemon required.

package compute

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrNoVersionProbe is returned when a node kind has no registered
// version source (e.g. openai-compat backends expose no standard
// version endpoint). It is NOT an error condition for the caller —
// save-time wiring treats it as "nothing to record".
var ErrNoVersionProbe = errors.New("no version probe for this backend")

// VersionHTTPClient is the injectable HTTP surface the version probe
// uses. *http.Client does not satisfy it directly; the adapter below
// wraps one. Tests supply a fake.
type VersionHTTPClient interface {
	// Get performs a GET and returns the status code + full body
	// (capped). err is non-nil only on transport-level failure
	// (connection refused / timeout / DNS).
	Get(ctx context.Context, url string) (status int, body []byte, err error)
}

// VersionRunner is the injectable command surface (docker CLI and
// friends). Tests supply a fake that never touches the daemon.
type VersionRunner interface {
	Run(ctx context.Context, name string, args ...string) (output string, err error)
}

// defaultVersionClient adapts *http.Client to VersionHTTPClient.
// Matches the existing probe pattern: operator-declared nodes are
// often self-signed, so verification is skipped (the node's own
// TLS* fields govern dispatch-time trust, not save-time probing).
type defaultVersionClient struct {
	client *http.Client
}

// DefaultVersionClient returns the production VersionHTTPClient:
// 5s timeout, self-signed tolerant, 1MB body cap.
func DefaultVersionClient() VersionHTTPClient {
	return &defaultVersionClient{
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 — save-time probe only, mirrors probeHTTP
			},
		},
	}
}

func (c *defaultVersionClient) Get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("version probe: build request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if rerr != nil {
		return resp.StatusCode, nil, fmt.Errorf("version probe: read body: %w", rerr)
	}
	return resp.StatusCode, body, nil
}

// defaultVersionRunner shells out via os/exec with the caller's ctx.
type defaultVersionRunner struct{}

// DefaultVersionRunner returns the production VersionRunner (exec).
func DefaultVersionRunner() VersionRunner { return &defaultVersionRunner{} }

func (defaultVersionRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// VersionProbe bundles the injectable I/O plus a clock (for tests).
// Zero value is usable: missing pieces fall back to the production
// defaults via withDefaults().
type VersionProbe struct {
	Client VersionHTTPClient
	Runner VersionRunner
	Now    func() time.Time
}

// NewVersionProbe returns a probe with production defaults.
func NewVersionProbe() *VersionProbe {
	return &VersionProbe{
		Client: DefaultVersionClient(),
		Runner: DefaultVersionRunner(),
		Now:    func() time.Time { return time.Now().UTC() },
	}
}

func (vp *VersionProbe) withDefaults() *VersionProbe {
	out := *vp
	if out.Client == nil {
		out.Client = DefaultVersionClient()
	}
	if out.Runner == nil {
		out.Runner = DefaultVersionRunner()
	}
	if out.Now == nil {
		out.Now = func() time.Time { return time.Now().UTC() }
	}
	return &out
}

// VersionResult is one successful version observation.
type VersionResult struct {
	Backend    string    // which backend reported ("ollama" | "docker")
	Raw        string    // verbatim version string from the backend
	Version    string    // normalized semver core (e.g. "0.5.14"), "" when unparseable
	ObservedAt time.Time // probe time (UTC)
}

// ProbeVersion discovers the node's backend version. Returns
// ErrNoVersionProbe when the kind has no known version source.
// Transport/parse failures carry diagnostic context.
func (vp *VersionProbe) ProbeVersion(ctx context.Context, n *Node) (VersionResult, error) {
	v := vp.withDefaults()
	if n == nil {
		return VersionResult{}, errors.New("version probe: nil node")
	}
	switch {
	case n.Routing == RoutingDockerNetwork || n.Kind == KindDocker:
		return v.dockerVersion(ctx, n)
	case n.Kind == KindOllama:
		return v.ollamaVersion(ctx, n)
	default:
		return VersionResult{}, fmt.Errorf("%w (kind %q)", ErrNoVersionProbe, n.Kind)
	}
}

// ollamaVersion GETs <address>/api/version and reads the "version"
// field: {"version":"0.5.14","packages":{"ollama":"0.5.14",...}}.
func (v *VersionProbe) ollamaVersion(ctx context.Context, n *Node) (VersionResult, error) {
	if strings.TrimSpace(n.Address) == "" {
		return VersionResult{}, errors.New("ollama version probe: node has no address")
	}
	url := strings.TrimRight(normalizeAddress(n.Address), "/") + "/api/version"
	status, body, err := v.Client.Get(ctx, url)
	if err != nil {
		return VersionResult{}, fmt.Errorf("ollama version probe: %s unreachable: %w", url, err)
	}
	if status != http.StatusOK {
		return VersionResult{}, fmt.Errorf("ollama version probe: HTTP %d from %s", status, url)
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return VersionResult{}, fmt.Errorf("ollama version probe: malformed response from %s: %v", url, err)
	}
	if strings.TrimSpace(doc.Version) == "" {
		return VersionResult{}, fmt.Errorf("ollama version probe: response from %s has no \"version\" field", url)
	}
	return VersionResult{
		Backend:    "ollama",
		Raw:        strings.TrimSpace(doc.Version),
		Version:    ParseBackendVersion(doc.Version),
		ObservedAt: v.Now(),
	}, nil
}

// dockerVersion reads the Docker daemon's ServerVersion via the same
// CLI call probeDocker already validates (`docker info`).
func (v *VersionProbe) dockerVersion(ctx context.Context, n *Node) (VersionResult, error) {
	args := []string{}
	if n.Routing == RoutingDockerNetwork && n.RoutingDockerNetwork != nil && n.RoutingDockerNetwork.DockerEndpoint != "" {
		args = append(args, "--host", n.RoutingDockerNetwork.DockerEndpoint)
	} else if n.Docker != nil && n.Docker.Endpoint != "" {
		args = append(args, "--host", n.Docker.Endpoint)
	}
	args = append(args, "info", "--format", "{{.ServerVersion}}")
	out, err := v.Runner.Run(ctx, "docker", args...)
	if err != nil {
		return VersionResult{}, fmt.Errorf("docker version probe: %v — %s", err, strings.TrimSpace(string(out)))
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return VersionResult{}, errors.New("docker version probe: empty server version response")
	}
	return VersionResult{
		Backend:    "docker",
		Raw:        raw,
		Version:    ParseBackendVersion(raw),
		ObservedAt: v.Now(),
	}, nil
}

// semverCoreRE matches the first semver-like core in a string:
// MAJOR.MINOR or MAJOR.MINOR.PATCH, digits only. It is deliberately
// tolerant of surrounding noise — version banners ("Docker version
// 27.1.1, build 537b5d6"), "v" prefixes ("v0.5.14"), and
// pre-release / build suffixes ("0.5.14-beta3", "26.1.2-rc1+bullseye")
// all yield the clean "X.Y.Z" core.
var semverCoreRE = regexp.MustCompile(`(?m)(\d+(?:\.\d+){1,2})`)

// ParseBackendVersion extracts the semver core from an arbitrary
// version string and returns it as "X.Y" or "X.Y.Z". Returns "" when
// the input carries no recognizable version (malformed / N/A / empty).
func ParseBackendVersion(s string) string {
	m := semverCoreRE.FindString(strings.TrimSpace(s))
	if m == "" {
		return ""
	}
	// Trim any trailing dot-run so "1.2.3.4" yields "1.2.3", not "1.2".
	return m
}

// ParseBackendVersionParts is ParseBackendVersion with the numeric
// components exposed — useful for comparisons and tests.
type BackendVersionParts struct {
	V     string // normalized core "X.Y" or "X.Y.Z"
	Major int
	Minor int
	Patch int
	OK    bool
}

func ParseBackendVersionParts(s string) BackendVersionParts {
	core := ParseBackendVersion(s)
	if core == "" {
		return BackendVersionParts{}
	}
	var major, minor, patch int
	seg := strings.Split(core, ".")
	major = atoiSilent(seg[0])
	if len(seg) > 1 {
		minor = atoiSilent(seg[1])
	}
	if len(seg) > 2 {
		patch = atoiSilent(seg[2])
	}
	return BackendVersionParts{V: core, Major: major, Minor: minor, Patch: patch, OK: true}
}

func atoiSilent(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// RecordBackendVersion stamps the probe result onto the node record.
// Called with the probe's observed time so the detail payload can
// show "version as of <time>".
func (n *Node) RecordBackendVersion(vr VersionResult) {
	if vr.Backend == "" && vr.Version == "" && vr.Raw == "" {
		return
	}
	n.VersionBackend = vr.Backend
	n.BackendVersion = vr.Version
	if n.BackendVersion == "" {
		n.BackendVersion = vr.Raw // unparseable but non-empty: keep verbatim
	}
	n.VersionProbedAt = vr.ObservedAt
}
