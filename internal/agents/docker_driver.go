package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	// Cluster Profile doesn't set its own image_registry. Typical
	// value "harbor.dmzs.com/datawatch". Set at construction.
	DefaultImagePrefix string

	// DefaultTag is the image tag to pull. Typically the datawatch
	// release version (e.g. "v2.4.5"). Set at construction.
	DefaultTag string

	// CallbackURL is the URL workers call for bootstrap. Usually the
	// parent's HTTPS endpoint; per-cluster override via
	// ClusterProfile.ParentCallbackURL takes priority.
	CallbackURL string
}

// NewDockerDriver builds a DockerDriver with sane defaults. bin can be
// "" to use "docker"; pass "podman" for rootless deploys. imagePrefix
// + tag are what the driver appends ImagePair.Agent to when forming
// the full image reference (e.g. harbor.dmzs.com/datawatch + agent-claude
// + v2.4.5 → harbor.dmzs.com/datawatch/agent-claude:v2.4.5).
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
		"-e", "DATAWATCH_BOOTSTRAP_URL=" + callback,
		"-e", "DATAWATCH_BOOTSTRAP_TOKEN=" + a.BootstrapToken,
		"-e", "DATAWATCH_AGENT_ID=" + a.ID,
	}

	// Inject per-project env overrides.
	for k, v := range a.project.Env {
		args = append(args, "-e", k+"="+v)
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

// ── helpers ────────────────────────────────────────────────────────────

// imageRef assembles the full image reference from (cluster registry
// override, default prefix) / agent image name : default tag.
//
// agent-claude → harbor.dmzs.com/datawatch/agent-claude:v2.4.5
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
