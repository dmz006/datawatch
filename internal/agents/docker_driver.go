package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// DockerDriver spawns workers on the local (or DOCKER_HOST-pointed)
// docker daemon by shelling out to the `docker` CLI.
//
// Rationale for CLI over the github.com/docker/docker Go client:
//   * ~1000 fewer transitive dependencies
//   * zero vendor risk (docker client's API surface changes often)
//   * trivial to debug — every call is reproducible at a shell prompt
//   * matches the existing datawatch pattern (rtk, signal-cli, etc.)
//
// Trade-offs:
//   * every call forks `docker` once (~10ms overhead)
//   * log streaming uses `docker logs -f` which means goroutine-per-stream
//     — acceptable at small N, revisit with the Go client if we ever
//     run 50+ concurrent workers per host.
//
// The driver labels every container it creates with
// `datawatch.role=agent-worker` + `datawatch.agent_id=<id>` so the
// reconciler (sprint 7) can find them by label even if our in-memory
// state was lost to a restart.
type DockerDriver struct {
	// Bin is the docker CLI to invoke. Defaults to "docker"; override
	// via NewDockerDriver for podman compatibility or custom paths.
	Bin string

	// DefaultImagePrefix is prepended to ImagePair.Agent when the
	// Cluster Profile doesn't set its own image_registry. Examples:
	// "ghcr.io/your-org/datawatch", "harbor.example.com/datawatch",
	// "localhost:5000/datawatch". Set at construction; configurable
	// via agents.image_prefix in config.yaml. See
	// docs/registry-and-secrets.md.
	DefaultImagePrefix string

	// DefaultTag is the image tag to pull. Typically the datawatch
	// release version (e.g. "v2.4.5"). Set at construction.
	DefaultTag string

	// CallbackURL is the URL workers call for bootstrap. Usually the
	// parent's HTTPS endpoint; per-cluster override via
	// ClusterProfile.ParentCallbackURL takes priority.
	CallbackURL string

	// WorkerBootstrapDeadlineSeconds is injected into spawned
	// containers as DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS so the
	// worker uses the operator-tuned deadline rather than its
	// compiled-in default. 0 means "let worker use its default".
	WorkerBootstrapDeadlineSeconds int

	// ParentCertFingerprint, when non-empty, is injected into the
	// spawned container as DATAWATCH_PARENT_CERT_FINGERPRINT. The
	// worker's bootstrap client pins to this fingerprint instead of
	// InsecureSkipVerify (F10 S4.3). Hex-encoded SHA-256 of the
	// parent's leaf cert, lower-case, no colons.
	ParentCertFingerprint string
}

// NewDockerDriver builds a DockerDriver with sane defaults. bin can be
// "" to use "docker"; pass "podman" for rootless deploys. imagePrefix
// + tag are what the driver appends ImagePair.Agent to when forming
// the full image reference (e.g. ghcr.io/your-org/datawatch + agent-claude
// + v2.4.5 → ghcr.io/your-org/datawatch/agent-claude:v2.4.5).
func NewDockerDriver(bin, imagePrefix, tag, callbackURL string) *DockerDriver {
	if bin == "" {
		bin = "docker"
	}
	return &DockerDriver{
		Bin:                bin,
		DefaultImagePrefix: imagePrefix,
		DefaultTag:         tag,
		CallbackURL:        callbackURL,
	}
}

// Kind implements Driver.
func (d *DockerDriver) Kind() string { return "docker" }

// Spawn pulls the agent image and creates + starts a container.
// The agent's sidecar is NOT spawned here — Sprint 3 ships agent-only
// spawns; Sprint 4 adds the two-container Pod pattern for k8s, and
// compose-style multi-container for docker will follow.
func (d *DockerDriver) Spawn(ctx context.Context, a *Agent) error {
	if a.project == nil || a.cluster == nil {
		return fmt.Errorf("agent %q missing profile references", a.ID)
	}
	image := d.imageRef(a)

	// Skip explicit pull — `docker run` will pull on miss. Saves a
	// round-trip when the image is already local.

	callback := d.callbackURL(a)
	name := "dw-" + a.ID

	args := []string{
		"run", "-d",
		"--name", name,
		"--label", "datawatch.role=agent-worker",
		"--label", "datawatch.agent_id=" + a.ID,
		"--label", "datawatch.project_profile=" + a.project.Name,
		"--label", "datawatch.cluster_profile=" + a.cluster.Name,
		// BL112 — extra labels the reconciler reads back to
		// reconstruct Agent records after a parent restart.
		"--label", "datawatch.branch=" + a.Branch,
		"--label", "datawatch.parent_agent_id=" + a.ParentAgentID,
		"-e", "DATAWATCH_BOOTSTRAP_URL=" + callback,
		"-e", "DATAWATCH_BOOTSTRAP_TOKEN=" + a.BootstrapToken,
		"-e", "DATAWATCH_AGENT_ID=" + a.ID,
	}

	// Operator-tunable bootstrap deadline; only injected when set so
	// the worker falls back to its compiled-in default otherwise.
	if d.WorkerBootstrapDeadlineSeconds > 0 {
		args = append(args, "-e",
			fmt.Sprintf("DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS=%d", d.WorkerBootstrapDeadlineSeconds))
	}

	// S4.3 parent cert pinning — only when the parent has a TLS cert.
	if d.ParentCertFingerprint != "" {
		args = append(args, "-e",
			"DATAWATCH_PARENT_CERT_FINGERPRINT="+d.ParentCertFingerprint)
	}

	// BL95 — PQC bootstrap material. Only injected when the Agent
	// record has keys (i.e. AgentsConfig.PQCBootstrap is on); the
	// worker uses these to construct the bootstrap envelope instead
	// of (or alongside) the UUID token above.
	if a.PQCKeys != nil {
		args = append(args,
			"-e", "DATAWATCH_PQC_MODE=ml-kem-768+ml-dsa-65",
			"-e", "DATAWATCH_PQC_KEM_PRIV="+a.PQCKeys.KEMPrivateB64,
			"-e", "DATAWATCH_PQC_KEM_PUB="+a.PQCKeys.KEMPublicB64,
			"-e", "DATAWATCH_PQC_SIGN_PRIV="+a.PQCKeys.SignPrivateB64,
		)
	}

	// Inject per-project env overrides. EnvOverride is set when
	// BL242 Phase 4 resolved ${secret:name} refs at spawn time.
	envMap := a.project.Env
	if a.EnvOverride != nil {
		envMap = a.EnvOverride
	}
	for k, v := range envMap {
		args = append(args, "-e", k+"="+v)
	}

	// BL114 — cluster-shared volumes. Docker only honours HostPath
	// directly. NFS + PVC sources are silently skipped on Docker —
	// the operator pre-mounts the NFS share at whatever host path
	// they choose and supplies it via HostPath in a separate
	// SharedVolume entry; PVC is k8s-only.
	//
	// No path is inferred to avoid hard-coding an "/mnt/..." prefix —
	// per AGENT.md "Configuration Accessibility Rule" every value
	// must be explicit on the operator's profile.
	for _, v := range a.cluster.SharedVolumes {
		if v.HostPath == "" {
			continue
		}
		mount := v.HostPath + ":" + v.MountPath
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	// Task, for now, is visible to the worker via env. Actually invoking
	// the task lives in the session start flow (Sprint 6+).
	if a.Task != "" {
		args = append(args, "-e", "DATAWATCH_TASK="+a.Task)
	}

	args = append(args, image)

	out, err := d.runDocker(ctx, args...)
	if err != nil {
		return fmt.Errorf("docker run %s: %w\n%s", image, err, out)
	}
	cid := strings.TrimSpace(out)
	a.DriverInstance = cid

	// Resolve the container's bridge IP so the reverse-proxy in S3.5
	// has a target. Docker may return an empty IP for host-network or
	// not-yet-scheduled containers; we capture best-effort and let
	// Status re-poll later.
	if ip, err := d.containerIP(ctx, cid); err == nil && ip != "" {
		a.ContainerAddr = ip + ":8080"
	}

	return nil
}

// Status polls docker inspect for the container's high-level state and
// maps it back to our State enum. Used by Manager's reconciler loop.
func (d *DockerDriver) Status(ctx context.Context, a *Agent) (State, error) {
	if a.DriverInstance == "" {
		return "", nil
	}
	out, err := d.runDocker(ctx, "inspect", "-f", "{{.State.Status}}", a.DriverInstance)
	if err != nil {
		// Container may have been removed; don't promote to error.
		if strings.Contains(err.Error(), "No such") ||
			strings.Contains(string(out), "No such") {
			return StateStopped, nil
		}
		return "", err
	}
	switch strings.TrimSpace(out) {
	case "created":
		return StateStarting, nil
	case "running":
		// Driver can't tell if the worker has bootstrapped. The
		// Manager transitions ready when the bootstrap call arrives.
		return StateStarting, nil
	case "paused", "restarting":
		return StateStarting, nil
	case "exited", "dead", "removing":
		return StateStopped, nil
	default:
		return "", nil
	}
}

// Logs fetches the last N lines from the container's stdout/stderr.
// `docker logs --tail N <id>` returns both streams merged.
func (d *DockerDriver) Logs(ctx context.Context, a *Agent, lines int) (string, error) {
	if a.DriverInstance == "" {
		return "", fmt.Errorf("agent has no container")
	}
	if lines <= 0 {
		lines = 200
	}
	out, err := d.runDocker(ctx, "logs", "--tail", fmt.Sprintf("%d", lines), a.DriverInstance)
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// Terminate forcefully removes the container. Using `rm -f` rather
// than `stop` + `rm` because workers are meant to be ephemeral; a
// graceful SIGTERM isn't worth the extra round-trip.
func (d *DockerDriver) Terminate(ctx context.Context, a *Agent) error {
	if a.DriverInstance == "" {
		return nil // nothing to do
	}
	out, err := d.runDocker(ctx, "rm", "-f", a.DriverInstance)
	if err != nil {
		// "No such container" is fine — someone else cleaned it up.
		if strings.Contains(err.Error(), "No such") ||
			strings.Contains(string(out), "No such") {
			return nil
		}
		return fmt.Errorf("docker rm -f %s: %w\n%s", a.DriverInstance, err, out)
	}
	return nil
}

// pushUnpushedScript is run inside the worker container via
// `docker exec <cid> sh -c <script>`. It locates the cloned repo
// (workers clone into /workspace/<repo>), reports the current branch
// and the number of commits on HEAD that are not on any remote ref,
// then pushes the branch to origin with upstream tracking. The push
// is a plain `git push --set-upstream` — fast-forward only, never a
// force. Output lines the driver parses:
//
//	NO_REPO_FOUND | NO_CURRENT_BRANCH | BRANCH=<b> | UNPUSHED=<n> | PUSH_OK | PUSH_FAILED
func pushUnpushedScript(repoName string) string {
	if repoName != "" {
		repoName = shellQuote(repoName)
	} else {
		repoName = `""`
	}
	return `repo=` + repoName + `
r=""
for c in "$repo" /workspace/* /workspace .; do
  if git -C "$c" rev-parse --git-dir >/dev/null 2>&1; then r="$c"; break; fi
done
if [ -z "$r" ]; then echo NO_REPO_FOUND; exit 1; fi
b=$(git -C "$r" symbolic-ref --short HEAD 2>/dev/null)
if [ -z "$b" ]; then echo NO_CURRENT_BRANCH; exit 1; fi
echo "BRANCH=$b"
n=$(git -C "$r" rev-list --count HEAD --not --remotes=origin 2>/dev/null)
case "$n" in ''|*[!0-9]*) n=0;; esac
echo "UNPUSHED=$n"
out=$(git -C "$r" push --set-upstream origin "$b" 2>&1)
status=$?
echo "$out"
if [ "$status" -eq 0 ]; then echo PUSH_OK; else echo PUSH_FAILED; exit "$status"; fi
`
}

func shellQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// PushUnpushed implements TeardownPusher: inside the worker container
// it counts the commits not yet on the remote and pushes the current
// branch with upstream tracking (fast-forward only, never force), so
// the container can be destroyed without losing the worker's work.
// Always returns a report; errors are folded into the report's
// Reason rather than returned, so termination is never blocked.
func (d *DockerDriver) PushUnpushed(ctx context.Context, a *Agent) (TeardownPushReport, error) {
	var rep TeardownPushReport
	if a.DriverInstance == "" {
		rep.Attempted = false
		rep.Reason = "agent has no container to exec into"
		return rep, nil
	}
	repoName := repoNameFromGitURL(agentGitURL(a))
	out, err := d.runDocker(ctx, "exec", a.DriverInstance, "sh", "-c", pushUnpushedScript(repoName))
	rep.Attempted = true
	if branch := pushScriptField(out, "BRANCH="); branch != "" {
		rep.Branch = branch
	}
	if count, ok := pushScriptCount(out); ok {
		rep.Count = count
	}
	switch {
	case strings.Contains(out, "NO_REPO_FOUND"):
		rep.Reason = "no git repo found in worker container"
	case strings.Contains(out, "NO_CURRENT_BRANCH"):
		rep.Reason = "worker on a detached HEAD (no current branch)"
	case strings.Contains(out, "PUSH_OK"):
		rep.Pushed = true
	default:
		tail := strings.TrimSpace(outputTail(out, 3))
		if err != nil {
			reason := "docker exec failed: " + err.Error()
			if tail != "" {
				reason += "; " + tail
			}
			rep.Reason = reason
		} else if tail != "" {
			rep.Reason = tail + " (fast-forward only; never force)"
		}
	}
	return rep, nil
}

func outputTail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
}

// pushScriptField extracts the last "KEY=value" line emitted by
// pushUnpushedScript (e.g. "BRANCH=worker-42").
func pushScriptField(out, key string) string {
	val := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, key); ok {
			val = strings.TrimSpace(v)
		}
	}
	return val
}

func pushScriptCount(out string) (int, bool) {
	raw := pushScriptField(out, "UNPUSHED=")
	if raw == "" {
		return 0, false
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// repoNameFromGitURL extracts the bare repository name from a clone
// URL (https://host/owner/repo(.git) or git@host:owner/repo(.git)).
// Empty input yields "".
func repoNameFromGitURL(u string) string {
	u = strings.TrimSuffix(u, ".git")
	if i := strings.LastIndex(u, ":"); i > 0 && !strings.Contains(u[:i], "/") {
		u = u[i+1:]
	}
	u = strings.TrimSuffix(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	return u
}

// agentGitURL best-effort resolves the worker's repository URL from
// its Project Profile (via the manager's cache); empty when unknown.
func agentGitURL(a *Agent) string {
	if a == nil {
		return ""
	}
	if a.project != nil {
		return a.project.Git.URL
	}
	return ""
}

// ── helpers ────────────────────────────────────────────────────────────

// imageRef assembles the full image reference from (cluster registry
// override, default prefix) / agent image name : default tag.
//
// agent-claude → ghcr.io/your-org/datawatch/agent-claude:v2.4.5
//
// When a ClusterProfile overrides image_registry we honour it so the
// same profile can target harbor in prod and localhost:5000 in dev.
func (d *DockerDriver) imageRef(a *Agent) string {
	prefix := d.DefaultImagePrefix
	if a.cluster.ImageRegistry != "" {
		prefix = a.cluster.ImageRegistry
	}
	tag := d.DefaultTag
	if tag == "" {
		tag = "latest"
	}
	agentImg := a.project.ImagePair.Agent
	if prefix == "" {
		return agentImg + ":" + tag
	}
	return prefix + "/" + agentImg + ":" + tag
}

// callbackURL resolves the URL workers should hit for bootstrap.
// Per-cluster override wins; then the driver default.
func (d *DockerDriver) callbackURL(a *Agent) string {
	if a.cluster.ParentCallbackURL != "" {
		return a.cluster.ParentCallbackURL
	}
	return d.CallbackURL
}

// runDocker is a thin wrapper around exec.CommandContext that captures
// combined stdout+stderr. 30s hard timeout guards against hung docker
// calls (transient daemon issues).
func (d *DockerDriver) runDocker(ctx context.Context, args ...string) (string, error) {
	// Respect caller cancellation but bound total wait to 30s.
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, d.Bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// ListLabelled implements Discovery (BL112). Shells out to
// `docker ps --filter label=K=V --format {{json .}}` for each
// selector entry and intersects the resulting container ID sets so
// AND semantics are preserved without a separate inspect.
func (d *DockerDriver) ListLabelled(ctx context.Context, _ *profile.ClusterProfile, selector map[string]string) ([]DiscoveredInstance, error) {
	if len(selector) == 0 {
		return nil, nil
	}
	args := []string{"ps", "--no-trunc", "--format", "{{.ID}}"}
	for k, v := range selector {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", k, v))
	}
	out, err := d.runDocker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, out)
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Inspect each ID once to fetch the labels we need + IP.
	out, err = d.runDocker(ctx,
		append([]string{"inspect", "-f",
			`{{.Id}}|{{json .Config.Labels}}|{{json .NetworkSettings.Networks}}`}, ids...)...)
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w\n%s", err, out)
	}
	var insts []DiscoveredInstance
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 {
			continue
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(parts[1]), &labels); err != nil {
			continue
		}
		var nets map[string]struct {
			IPAddress string `json:"IPAddress"`
		}
		_ = json.Unmarshal([]byte(parts[2]), &nets)
		var addr string
		for _, n := range nets {
			if n.IPAddress != "" {
				addr = n.IPAddress + ":8080"
				break
			}
		}
		insts = append(insts, DiscoveredInstance{
			DriverInstance: parts[0],
			AgentID:        labels["datawatch.agent_id"],
			ProjectProfile: labels["datawatch.project_profile"],
			ClusterProfile: labels["datawatch.cluster_profile"],
			Branch:         labels["datawatch.branch"],
			ParentAgentID:  labels["datawatch.parent_agent_id"],
			Addr:           addr,
		})
	}
	return insts, nil
}

// containerIP returns the container's first bridge-network IP, or
// empty string if it has none (host network, or not yet scheduled).
// Used to populate Agent.ContainerAddr for the S3.5 reverse proxy.
func (d *DockerDriver) containerIP(ctx context.Context, cid string) (string, error) {
	// docker inspect -f '{{json .NetworkSettings.Networks}}' returns a
	// map keyed by network name; we take the first non-empty IP.
	out, err := d.runDocker(ctx, "inspect", "-f", "{{json .NetworkSettings.Networks}}", cid)
	if err != nil {
		return "", err
	}
	var nets map[string]struct {
		IPAddress string `json:"IPAddress"`
	}
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(out)), &nets); jsonErr != nil {
		return "", jsonErr
	}
	for _, net := range nets {
		if net.IPAddress != "" {
			return net.IPAddress, nil
		}
	}
	return "", nil
}
