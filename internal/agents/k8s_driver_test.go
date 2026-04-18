// F10 sprint 4 (S4.1) — K8s driver tests.
//
// Same fake-binary pattern as docker_driver_test.go: drop a shell
// script named "kubectl" into a tempdir, prepend to $PATH, inspect
// captured argv + stdin. No real apiserver required.

package agents

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// newFakeKubectl mirrors newFakeDocker but names the script "kubectl"
// and also captures stdin (used by `apply -f -`) to stdin.log.
func newFakeKubectl(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
# Record full argv, one line per invocation
echo "$@" >> "` + dir + `/invocations.log"
# Capture stdin into a scratch file, then preserve stdin.last only
# when it's non-empty — subsequent no-stdin invocations (get, logs)
# must not truncate the apply's stdin.
cat > "` + dir + `/stdin.tmp"
if [ -s "` + dir + `/stdin.tmp" ]; then
    mv "` + dir + `/stdin.tmp" "` + dir + `/stdin.last"
    cat "` + dir + `/stdin.last" >> "` + dir + `/stdin.log"
    echo "---EOM---" >> "` + dir + `/stdin.log"
else
    rm -f "` + dir + `/stdin.tmp"
fi
# Find the first non-flag arg — that's the kubectl subcommand.
# Loop past every "--flag" and its value so "--context testing get …"
# keys on "get".
sub=""
skip=0
for arg in "$@"; do
    if [ "$skip" = "1" ]; then
        skip=0
        continue
    fi
    case "$arg" in
        --context|--namespace|-n|--kubeconfig)
            skip=1
            continue
            ;;
        -*)
            continue
            ;;
        *)
            sub="$arg"
            break
            ;;
    esac
done
if [ -f "` + dir + `/output.$sub" ]; then
    cat "` + dir + `/output.$sub"
fi
if [ -f "` + dir + `/exitcode.$sub" ]; then
    exit "$(cat "` + dir + `/exitcode.$sub")"
fi
exit 0
`
	path := filepath.Join(dir, "kubectl")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake kubectl: %v", err)
	}
	return dir
}

// k8sTestAgent returns a minimal Agent with a k8s-kind cluster.
func k8sTestAgent(t *testing.T, modCluster func(*profile.ClusterProfile)) *Agent {
	t.Helper()
	p := &profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	}
	c := &profile.ClusterProfile{
		Name:      "c",
		Kind:      profile.ClusterK8s,
		Context:   "testing",
		Namespace: "datawatch",
	}
	if modCluster != nil {
		modCluster(c)
	}
	return &Agent{
		ID:             "agent-xyz",
		project:        p,
		cluster:        c,
		BootstrapToken: "token-123",
		Task:           "echo hi",
	}
}

// Kind returns "k8s" so the Manager's driver map dispatches correctly.
func TestK8sDriver_Kind(t *testing.T) {
	if got := (&K8sDriver{}).Kind(); got != "k8s" {
		t.Errorf("Kind=%q want k8s", got)
	}
}

// NewK8sDriver defaults Bin to "kubectl" when empty.
func TestK8sDriver_NewDefaultsBin(t *testing.T) {
	d := NewK8sDriver("", "prefix", "v1", "http://parent")
	if d.Bin != "kubectl" {
		t.Errorf("Bin=%q want kubectl", d.Bin)
	}
}

// Spawn renders a Pod manifest with every required field populated
// and feeds it to `kubectl apply -f -`.
func TestK8sDriver_Spawn_ManifestContent(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.get"), []byte("10.244.0.5"), 0644)

	d := NewK8sDriver("", "harbor.dmzs.com/datawatch", "v2.4.5", "http://parent:8080")
	a := k8sTestAgent(t, func(c *profile.ClusterProfile) {
		c.ImagePullSecret = "harbor-creds"
	})
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if a.DriverInstance != "datawatch/dw-agent-xyz" {
		t.Errorf("DriverInstance=%q want datawatch/dw-agent-xyz", a.DriverInstance)
	}
	if a.ContainerAddr != "10.244.0.5:8080" {
		t.Errorf("ContainerAddr=%q want 10.244.0.5:8080", a.ContainerAddr)
	}

	manifest, _ := os.ReadFile(filepath.Join(dir, "stdin.last"))
	wants := []string{
		"apiVersion: v1",
		"kind: Pod",
		"name: dw-agent-xyz",
		"namespace: datawatch",
		"datawatch.role: agent-worker",
		`datawatch.agent_id: "agent-xyz"`,
		"imagePullSecrets:",
		"- name: harbor-creds",
		"image: harbor.dmzs.com/datawatch/agent-claude:v2.4.5",
		`value: "http://parent:8080"`, // DATAWATCH_BOOTSTRAP_URL
		`value: "token-123"`,          // DATAWATCH_BOOTSTRAP_TOKEN
		`value: "agent-xyz"`,          // DATAWATCH_AGENT_ID
		`value: "echo hi"`,            // DATAWATCH_TASK
	}
	for _, w := range wants {
		if !bytes.Contains(manifest, []byte(w)) {
			t.Errorf("manifest missing %q\n---manifest---\n%s", w, manifest)
		}
	}

	// --context propagated to kubectl.
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	if !strings.Contains(string(log), "--context testing") {
		t.Errorf("invocation log missing --context testing:\n%s", log)
	}
}

// BootstrapDeadlineSeconds, when non-zero, must appear as an env var.
func TestK8sDriver_Spawn_BootstrapDeadlineEnv(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	d.WorkerBootstrapDeadlineSeconds = 120
	a := k8sTestAgent(t, nil)
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "stdin.last"))
	if !bytes.Contains(manifest, []byte(`value: "120"`)) ||
		!bytes.Contains(manifest, []byte("DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS")) {
		t.Errorf("deadline env missing:\n%s", manifest)
	}
}

// Spawn surfaces kubectl stderr/stdout in the returned error.
func TestK8sDriver_Spawn_ErrorIncludesOutput(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.apply"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.apply"),
		[]byte("error: unable to recognize Pod spec"), 0644)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, nil)
	err := d.Spawn(context.Background(), a)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unable to recognize") {
		t.Errorf("error missing kubectl stdout: %v", err)
	}
}

// Status maps k8s Pod phases to our State enum.
func TestK8sDriver_Status_PhaseMapping(t *testing.T) {
	cases := []struct {
		phase string
		want  State
	}{
		{"Pending", StateStarting},
		{"Running", StateStarting}, // ready-transition is the Manager's job
		{"Succeeded", StateStopped},
		{"Failed", StateStopped},
		{"Unknown", ""},
	}
	for _, c := range cases {
		t.Run(c.phase, func(t *testing.T) {
			dir := newFakeKubectl(t)
			withFakePath(t, dir)
			_ = os.WriteFile(filepath.Join(dir, "output.get"), []byte(c.phase), 0644)

			d := NewK8sDriver("", "p", "v1", "http://parent")
			a := k8sTestAgent(t, nil)
			a.DriverInstance = "datawatch/dw-agent-xyz"
			got, err := d.Status(context.Background(), a)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("phase=%q → %q want %q", c.phase, got, c.want)
			}
		})
	}
}

// A "not found" response from kubectl maps to Stopped (reconciler /
// GC already cleaned it up).
func TestK8sDriver_Status_NotFound(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.get"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.get"),
		[]byte("Error from server (NotFound): pods \"dw-xyz\" not found"), 0644)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, nil)
	a.DriverInstance = "datawatch/dw-agent-xyz"
	got, err := d.Status(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if got != StateStopped {
		t.Errorf("state=%q want %q", got, StateStopped)
	}
}

// Logs wires --tail, --namespace and the container name.
func TestK8sDriver_Logs(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.logs"), []byte("line1\nline2"), 0644)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, nil)
	a.DriverInstance = "datawatch/dw-agent-xyz"
	out, err := d.Logs(context.Background(), a, 42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "line1") {
		t.Errorf("logs=%q", out)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	wants := []string{"logs", "--tail 42", "-n datawatch", "dw-agent-xyz", "-c worker"}
	for _, w := range wants {
		if !strings.Contains(string(log), w) {
			t.Errorf("log missing %q:\n%s", w, log)
		}
	}
}

// Terminate uses --grace-period=0 --force --ignore-not-found.
func TestK8sDriver_Terminate(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, nil)
	a.DriverInstance = "datawatch/dw-agent-xyz"
	if err := d.Terminate(context.Background(), a); err != nil {
		t.Fatalf("Terminate: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	wants := []string{"delete", "pod", "dw-agent-xyz", "--grace-period=0", "--force", "--ignore-not-found"}
	for _, w := range wants {
		if !strings.Contains(string(log), w) {
			t.Errorf("log missing %q:\n%s", w, log)
		}
	}
}

// Terminate swallows "not found" errors.
func TestK8sDriver_Terminate_NotFound(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.delete"), []byte("1"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "output.delete"),
		[]byte("Error from server (NotFound)"), 0644)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, nil)
	a.DriverInstance = "datawatch/dw-agent-xyz"
	if err := d.Terminate(context.Background(), a); err != nil {
		t.Errorf("Terminate should swallow NotFound, got: %v", err)
	}
}

// Namespace defaults to "default" when the cluster profile omits it.
func TestK8sDriver_Spawn_DefaultNamespace(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, func(c *profile.ClusterProfile) { c.Namespace = "" })
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if !strings.HasPrefix(a.DriverInstance, "default/") {
		t.Errorf("DriverInstance=%q want default/…", a.DriverInstance)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "stdin.last"))
	if !bytes.Contains(manifest, []byte("namespace: default")) {
		t.Errorf("manifest missing namespace: default\n%s", manifest)
	}
}

// Resources on the cluster profile render into the Pod spec.
func TestK8sDriver_Spawn_ResourceLimits(t *testing.T) {
	dir := newFakeKubectl(t)
	withFakePath(t, dir)

	d := NewK8sDriver("", "p", "v1", "http://parent")
	a := k8sTestAgent(t, func(c *profile.ClusterProfile) {
		c.DefaultResources = profile.Resources{
			CPURequest: "100m", CPULimit: "500m",
			MemRequest: "128Mi", MemLimit: "512Mi",
		}
	})
	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	manifest, _ := os.ReadFile(filepath.Join(dir, "stdin.last"))
	for _, w := range []string{"resources:", "requests:", "limits:", "cpu: 100m", "memory: 512Mi"} {
		if !bytes.Contains(manifest, []byte(w)) {
			t.Errorf("manifest missing %q:\n%s", w, manifest)
		}
	}
}
