// F10 sprint 4 (S4.1) — Kubernetes driver.
//
// Mirrors DockerDriver's design decision of shelling out to the
// platform CLI (kubectl) rather than pulling in k8s.io/client-go.
// Rationale (same as DockerDriver):
//   * ~1000 fewer transitive dependencies
//   * zero vendor risk (client-go moves fast, has deprecation churn)
//   * trivial to debug — every call reproducible at a shell prompt
//   * matches the existing datawatch pattern (rtk, signal-cli, docker)
//
// Trade-offs:
//   * every call forks kubectl once (~30-80ms on a local apiserver)
//   * no incremental watches — we poll Status on demand
//   * YAML round-trip through text/template — less type-safe than
//     unstructured.Unstructured, but YAML diffs are human-readable
//
// Each Pod is created with the labels `datawatch.role=agent-worker`
// + `datawatch.agent_id=<id>` so the Sprint 7 reconciler can find
// workers by label after a parent restart.
//
// K8s-specific features deferred to later S4 stories:
//   * S4.3 — TrustedCAs ConfigMap + SSL_CERT_DIR wiring
//   * S4.2 — parent_callback_url auto-discovery from node network
//   * Owner references + reap-on-terminate (Sprint 7 reconciler)

package agents

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// K8sDriver spawns workers as Pods in the configured kubectl context.
type K8sDriver struct {
	// Bin is the kubectl binary name. Defaults to "kubectl" when empty.
	// Set to "oc" for OpenShift or a custom path for vendored binaries.
	Bin string

	// DefaultImagePrefix is prepended to ImagePair.Agent when the
	// Cluster Profile doesn't set its own image_registry. Typical
	// "harbor.dmzs.com/datawatch".
	DefaultImagePrefix string

	// DefaultTag — datawatch release version, e.g. "v2.4.5".
	DefaultTag string

	// CallbackURL is the URL workers call home to for bootstrap.
	// Per-cluster override via ClusterProfile.ParentCallbackURL wins.
	CallbackURL string

	// WorkerBootstrapDeadlineSeconds is injected into spawned Pods as
	// DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS. 0 = use worker default.
	WorkerBootstrapDeadlineSeconds int
}

// NewK8sDriver mirrors NewDockerDriver's constructor shape.
func NewK8sDriver(bin, imagePrefix, tag, callbackURL string) *K8sDriver {
	if bin == "" {
		bin = "kubectl"
	}
	return &K8sDriver{
		Bin:                bin,
		DefaultImagePrefix: imagePrefix,
		DefaultTag:         tag,
		CallbackURL:        callbackURL,
	}
}

// Kind implements Driver.
func (d *K8sDriver) Kind() string { return "k8s" }

// podManifest is the YAML we feed to `kubectl apply -f -`. Kept
// deliberately minimal — Sprint 4 additions (TrustedCAs ConfigMap,
// resource limits, network policy) will grow it.
const podManifest = `apiVersion: v1
kind: Pod
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
  labels:
    datawatch.role: agent-worker
    datawatch.agent_id: "{{.AgentID}}"
    datawatch.project_profile: "{{.ProjectProfile}}"
    datawatch.cluster_profile: "{{.ClusterProfile}}"
spec:
  restartPolicy: Never
{{- if .ImagePullSecret }}
  imagePullSecrets:
    - name: {{.ImagePullSecret}}
{{- end }}
  containers:
    - name: worker
      image: {{.Image}}
      env:
        - name: DATAWATCH_BOOTSTRAP_URL
          value: "{{.CallbackURL}}"
        - name: DATAWATCH_BOOTSTRAP_TOKEN
          value: "{{.BootstrapToken}}"
        - name: DATAWATCH_AGENT_ID
          value: "{{.AgentID}}"
{{- if .Task }}
        - name: DATAWATCH_TASK
          value: {{printf "%q" .Task}}
{{- end }}
{{- if .BootstrapDeadlineSeconds }}
        - name: DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS
          value: "{{.BootstrapDeadlineSeconds}}"
{{- end }}
{{- range $k, $v := .ProjectEnv }}
        - name: {{$k}}
          value: {{printf "%q" $v}}
{{- end }}
{{- if or .Resources.CPURequest .Resources.MemRequest .Resources.CPULimit .Resources.MemLimit }}
      resources:
{{- if or .Resources.CPURequest .Resources.MemRequest }}
        requests:
{{- if .Resources.CPURequest }}
          cpu: {{.Resources.CPURequest}}
{{- end }}
{{- if .Resources.MemRequest }}
          memory: {{.Resources.MemRequest}}
{{- end }}
{{- end }}
{{- if or .Resources.CPULimit .Resources.MemLimit }}
        limits:
{{- if .Resources.CPULimit }}
          cpu: {{.Resources.CPULimit}}
{{- end }}
{{- if .Resources.MemLimit }}
          memory: {{.Resources.MemLimit}}
{{- end }}
{{- end }}
{{- end }}
`

type podTemplateData struct {
	Name                     string
	Namespace                string
	AgentID                  string
	ProjectProfile           string
	ClusterProfile           string
	Image                    string
	ImagePullSecret          string
	CallbackURL              string
	BootstrapToken           string
	Task                     string
	BootstrapDeadlineSeconds int
	ProjectEnv               map[string]string
	Resources                profile.Resources
}

var podTmpl = template.Must(template.New("pod").Parse(podManifest))

// Spawn renders a Pod manifest and applies it via kubectl stdin.
// Blocks only until the apiserver accepts the Pod — scheduling +
// readiness are observed via Status polling, matching the Docker
// driver's semantics.
func (d *K8sDriver) Spawn(ctx context.Context, a *Agent) error {
	if a.project == nil || a.cluster == nil {
		return fmt.Errorf("agent %q missing profile references", a.ID)
	}

	data := podTemplateData{
		Name:                     "dw-" + a.ID,
		Namespace:                a.cluster.EffectiveNamespace(),
		AgentID:                  a.ID,
		ProjectProfile:           a.project.Name,
		ClusterProfile:           a.cluster.Name,
		Image:                    d.imageRef(a),
		ImagePullSecret:          a.cluster.ImagePullSecret,
		CallbackURL:              d.callbackURL(a),
		BootstrapToken:           a.BootstrapToken,
		Task:                     a.Task,
		BootstrapDeadlineSeconds: d.WorkerBootstrapDeadlineSeconds,
		ProjectEnv:               a.project.Env,
		Resources:                a.cluster.DefaultResources,
	}

	var buf bytes.Buffer
	if err := podTmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("render pod manifest: %w", err)
	}

	out, err := d.runKubectlStdin(ctx, buf.Bytes(),
		d.contextArgs(a.cluster, "apply", "-f", "-")...)
	if err != nil {
		return fmt.Errorf("kubectl apply: %w\n%s", err, out)
	}
	a.DriverInstance = data.Namespace + "/" + data.Name

	// Best-effort: capture the Pod IP for the S3.5 reverse proxy.
	// May be empty if the Pod hasn't been scheduled yet; Status will
	// re-resolve later.
	if ip, err := d.podIP(ctx, a); err == nil && ip != "" {
		a.ContainerAddr = ip + ":8080"
	}
	return nil
}

// Status polls the Pod phase and maps it to our State enum.
func (d *K8sDriver) Status(ctx context.Context, a *Agent) (State, error) {
	if a.DriverInstance == "" {
		return "", nil
	}
	out, err := d.runKubectl(ctx,
		d.contextArgs(a.cluster, "get", "pod", d.podName(a),
			"-n", a.cluster.EffectiveNamespace(),
			"-o", "jsonpath={.status.phase}")...)
	if err != nil {
		// A missing Pod isn't fatal — reconciler / cleanup may have
		// removed it already. Surface as Stopped rather than error.
		if isNotFound(err.Error(), out) {
			return StateStopped, nil
		}
		return "", err
	}
	phase := strings.TrimSpace(out)
	switch phase {
	case "Pending":
		return StateStarting, nil
	case "Running":
		// Driver can't tell if the worker has bootstrapped. The
		// Manager transitions ready when the bootstrap call arrives.
		return StateStarting, nil
	case "Succeeded", "Failed":
		return StateStopped, nil
	case "Unknown", "":
		return "", nil
	default:
		return "", nil
	}
}

// Logs fetches the tail of the worker container's stdout/stderr.
func (d *K8sDriver) Logs(ctx context.Context, a *Agent, lines int) (string, error) {
	if a.DriverInstance == "" {
		return "", fmt.Errorf("agent has no pod")
	}
	if lines <= 0 {
		lines = 200
	}
	return d.runKubectl(ctx,
		d.contextArgs(a.cluster, "logs",
			"--tail", fmt.Sprintf("%d", lines),
			"-n", a.cluster.EffectiveNamespace(),
			d.podName(a),
			"-c", "worker")...)
}

// Terminate deletes the Pod. Uses --grace-period=0 --force to match
// the Docker driver's "ephemeral — no graceful shutdown needed"
// semantics. Missing-Pod errors are swallowed (already cleaned up).
func (d *K8sDriver) Terminate(ctx context.Context, a *Agent) error {
	if a.DriverInstance == "" {
		return nil
	}
	out, err := d.runKubectl(ctx,
		d.contextArgs(a.cluster, "delete", "pod",
			"-n", a.cluster.EffectiveNamespace(),
			d.podName(a),
			"--grace-period=0",
			"--force",
			"--ignore-not-found")...)
	if err != nil {
		if isNotFound(err.Error(), out) {
			return nil
		}
		return fmt.Errorf("kubectl delete pod: %w\n%s", err, out)
	}
	return nil
}

// ── helpers ────────────────────────────────────────────────────────────

// imageRef mirrors DockerDriver.imageRef — keeps the compose logic
// consistent so operators can swap cluster kinds without editing
// profiles.
func (d *K8sDriver) imageRef(a *Agent) string {
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

func (d *K8sDriver) callbackURL(a *Agent) string {
	if a.cluster.ParentCallbackURL != "" {
		return a.cluster.ParentCallbackURL
	}
	return d.CallbackURL
}

func (d *K8sDriver) podName(a *Agent) string {
	// DriverInstance is "ns/name"; grab the name.
	if i := strings.IndexByte(a.DriverInstance, '/'); i >= 0 {
		return a.DriverInstance[i+1:]
	}
	return a.DriverInstance
}

// contextArgs prepends --context (and --namespace, handled per-call
// since `apply` vs `get pod` pass it differently) when the profile
// specifies a kubectl context.
func (d *K8sDriver) contextArgs(c *profile.ClusterProfile, args ...string) []string {
	out := make([]string, 0, len(args)+2)
	if c.Context != "" {
		out = append(out, "--context", c.Context)
	}
	return append(out, args...)
}

// runKubectl runs `kubectl <args...>` with a 30s cap.
func (d *K8sDriver) runKubectl(ctx context.Context, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, d.Bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// runKubectlStdin is runKubectl but pipes `input` to kubectl's stdin.
// Used by `apply -f -`.
func (d *K8sDriver) runKubectlStdin(ctx context.Context, input []byte, args ...string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(callCtx, d.Bin, args...)
	cmd.Stdin = bytes.NewReader(input)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// podIP resolves the Pod's cluster IP for the S3.5 reverse proxy.
// Empty string when the Pod has no IP yet (Pending phase).
func (d *K8sDriver) podIP(ctx context.Context, a *Agent) (string, error) {
	out, err := d.runKubectl(ctx,
		d.contextArgs(a.cluster, "get", "pod", "dw-"+a.ID,
			"-n", a.cluster.EffectiveNamespace(),
			"-o", "jsonpath={.status.podIP}")...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// isNotFound heuristic — kubectl's not-found wording is stable across
// versions. We grep both the error string and the output because
// `kubectl delete --ignore-not-found` writes to stdout, not stderr.
func isNotFound(errStr, out string) bool {
	for _, s := range []string{errStr, out} {
		if strings.Contains(s, "NotFound") ||
			strings.Contains(s, "not found") {
			return true
		}
	}
	return false
}
