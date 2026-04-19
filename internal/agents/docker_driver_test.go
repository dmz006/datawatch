package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/profile"
)

// The DockerDriver shells out to the `docker` CLI. These tests don't
// touch a real docker daemon — instead we drop a tiny script named
// "docker" into a temp directory, prepend that dir to $PATH, and
// inspect what the driver would have run.
//
// Alternative would have been to pull in `go-docker`'s test helpers
// but those require a working socket anyway.

// newFakeDocker writes a minimal shell script to $dir/docker that:
//   * records its full argv to $dir/invocations.log
//   * emits the content of $dir/output.<subcommand> to stdout
//   * exits 0 or the code in $dir/exitcode.<subcommand> if present
//
// Returns the dir so tests can read the log + seed subcommand outputs.
func newFakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
# Record full argv, one line per invocation
echo "$@" >> "` + dir + `/invocations.log"
# Route by first arg — the docker subcommand
sub="$1"
if [ -f "` + dir + `/output.$sub" ]; then
    cat "` + dir + `/output.$sub"
fi
if [ -f "` + dir + `/exitcode.$sub" ]; then
    exit "$(cat "` + dir + `/exitcode.$sub")"
fi
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	return dir
}

// withFakePath prepends dir to $PATH so exec.Command("docker") finds
// the fake first. Restores on t.Cleanup.
func withFakePath(t *testing.T, dir string) {
	t.Helper()
	prev := os.Getenv("PATH")
	_ = os.Setenv("PATH", dir+":"+prev)
	t.Cleanup(func() { _ = os.Setenv("PATH", prev) })
}

// testAgent returns a minimal Agent ready for driver calls.
func testAgent(t *testing.T, modProj func(*profile.ProjectProfile), modCluster func(*profile.ClusterProfile)) *Agent {
	t.Helper()
	p := &profile.ProjectProfile{
		Name:      "p",
		Git:       profile.GitSpec{URL: "https://github.com/x/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	}
	if modProj != nil {
		modProj(p)
	}
	c := &profile.ClusterProfile{
		Name:    "c",
		Kind:    profile.ClusterDocker,
		Context: "local",
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

// ── imageRef ────────────────────────────────────────────────────────────

func TestDockerDriver_ImageRef_Defaults(t *testing.T) {
	d := NewDockerDriver("", "harbor.dmzs.com/datawatch", "v2.4.5", "http://parent:8080")
	a := testAgent(t, nil, nil)
	got := d.imageRef(a)
	want := "harbor.dmzs.com/datawatch/agent-claude:v2.4.5"
	if got != want {
		t.Errorf("imageRef=%q want %q", got, want)
	}
}

func TestDockerDriver_ImageRef_ClusterOverride(t *testing.T) {
	d := NewDockerDriver("", "harbor.dmzs.com/datawatch", "v2.4.5", "")
	a := testAgent(t, nil, func(c *profile.ClusterProfile) {
		c.ImageRegistry = "localhost:5000/datawatch"
	})
	got := d.imageRef(a)
	want := "localhost:5000/datawatch/agent-claude:v2.4.5"
	if got != want {
		t.Errorf("cluster override imageRef=%q want %q", got, want)
	}
}

func TestDockerDriver_ImageRef_EmptyPrefix(t *testing.T) {
	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	got := d.imageRef(a)
	if got != "agent-claude:latest" {
		t.Errorf("empty prefix imageRef=%q want agent-claude:latest", got)
	}
}

// ── callbackURL ───────────────────────────────────────────────────────

func TestDockerDriver_CallbackURL_ClusterOverride(t *testing.T) {
	d := NewDockerDriver("", "", "", "http://fallback:8080")
	a := testAgent(t, nil, func(c *profile.ClusterProfile) {
		c.ParentCallbackURL = "http://192.168.1.51:8443"
	})
	if got := d.callbackURL(a); got != "http://192.168.1.51:8443" {
		t.Errorf("callbackURL=%q want cluster override", got)
	}
}

func TestDockerDriver_CallbackURL_DriverDefault(t *testing.T) {
	d := NewDockerDriver("", "", "", "http://driver-default:8080")
	a := testAgent(t, nil, nil)
	if got := d.callbackURL(a); got != "http://driver-default:8080" {
		t.Errorf("callbackURL=%q want driver default", got)
	}
}

// ── Spawn argv contract ────────────────────────────────────────────────

func TestDockerDriver_Spawn_InvocationContents(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)

	// Fake docker will: on "run", emit a container ID on stdout;
	// on "inspect", emit a networks JSON so containerIP resolves.
	if err := os.WriteFile(filepath.Join(dir, "output.run"), []byte("fake-cid-123\n"), 0644); err != nil {
		t.Fatal(err)
	}
	inspectOut := `{"bridge":{"IPAddress":"172.17.0.5"}}`
	if err := os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(inspectOut), 0644); err != nil {
		t.Fatal(err)
	}

	d := NewDockerDriver("", "harbor.dmzs.com/datawatch", "v2.4.5", "http://parent:8080")
	a := testAgent(t, func(p *profile.ProjectProfile) {
		p.Env = map[string]string{"FOO": "bar"}
	}, nil)

	if err := d.Spawn(context.Background(), a); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if a.DriverInstance != "fake-cid-123" {
		t.Errorf("DriverInstance=%q want fake-cid-123", a.DriverInstance)
	}
	if a.ContainerAddr != "172.17.0.5:8080" {
		t.Errorf("ContainerAddr=%q want 172.17.0.5:8080", a.ContainerAddr)
	}

	// Inspect the invocation log: first line should be the `run`,
	// second line the `inspect` lookup.
	logBytes, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
	log := string(logBytes)
	want := []string{
		"run", "-d",
		"--name", "dw-agent-xyz",
		"--label datawatch.role=agent-worker",
		"--label datawatch.agent_id=agent-xyz",
		"-e DATAWATCH_BOOTSTRAP_URL=http://parent:8080",
		"-e DATAWATCH_BOOTSTRAP_TOKEN=token-123",
		"-e DATAWATCH_AGENT_ID=agent-xyz",
		"-e FOO=bar",
		"-e DATAWATCH_TASK=echo hi",
		"harbor.dmzs.com/datawatch/agent-claude:v2.4.5",
	}
	for _, w := range want {
		if !strings.Contains(log, w) {
			t.Errorf("invocation log missing %q\nfull log:\n%s", w, log)
		}
	}
}

// WorkerBootstrapDeadlineSeconds: when set, must be injected into the
// container's env so the worker uses it. When unset, the env var must
// be absent so the worker falls back to its compiled-in default.
func TestDockerDriver_Spawn_BootstrapDeadlineEnv(t *testing.T) {
	t.Run("set → env injected", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)

		d := NewDockerDriver("", "p", "v1", "http://parent")
		d.WorkerBootstrapDeadlineSeconds = 120
		a := testAgent(t, nil, nil)
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		if !strings.Contains(string(log), "-e DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS=120") {
			t.Errorf("missing deadline env in invocation:\n%s", log)
		}
	})

	t.Run("unset → no env", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)

		d := NewDockerDriver("", "p", "v1", "http://parent") // deadline left at 0
		a := testAgent(t, nil, nil)
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		if strings.Contains(string(log), "DATAWATCH_BOOTSTRAP_DEADLINE_SECONDS") {
			t.Errorf("deadline env should be absent when zero:\n%s", log)
		}
	})
}

// F10 S4.3 — ParentCertFingerprint injection: present when set,
// absent when empty.
func TestDockerDriver_Spawn_ParentCertFingerprintEnv(t *testing.T) {
	t.Run("set → env injected", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)
		d := NewDockerDriver("", "p", "v1", "http://parent")
		d.ParentCertFingerprint = "deadbeef"
		a := testAgent(t, nil, nil)
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		if !strings.Contains(string(log), "-e DATAWATCH_PARENT_CERT_FINGERPRINT=deadbeef") {
			t.Errorf("missing fingerprint env:\n%s", log)
		}
	})

	t.Run("unset → no env", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)
		d := NewDockerDriver("", "p", "v1", "http://parent")
		a := testAgent(t, nil, nil)
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		if strings.Contains(string(log), "DATAWATCH_PARENT_CERT_FINGERPRINT") {
			t.Errorf("fingerprint env should be absent when empty:\n%s", log)
		}
	})
}

// BL95 — PQC env injection: when the Agent record holds PQCKeys the
// driver injects DATAWATCH_PQC_* env into the run; absent otherwise.
func TestDockerDriver_Spawn_PQCEnvInjection(t *testing.T) {
	t.Run("keys present → env injected", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)
		d := NewDockerDriver("", "p", "v1", "http://parent")
		a := testAgent(t, nil, nil)
		a.PQCKeys = &PQCKeys{
			KEMPrivateB64:  "kem-priv",
			KEMPublicB64:   "kem-pub",
			SignPrivateB64: "sign-priv",
		}
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		for _, want := range []string{
			"DATAWATCH_PQC_MODE=ml-kem-768+ml-dsa-65",
			"DATAWATCH_PQC_KEM_PRIV=kem-priv",
			"DATAWATCH_PQC_KEM_PUB=kem-pub",
			"DATAWATCH_PQC_SIGN_PRIV=sign-priv",
		} {
			if !strings.Contains(string(log), want) {
				t.Errorf("missing %q in invocation:\n%s", want, log)
			}
		}
	})

	t.Run("keys absent → env absent", func(t *testing.T) {
		dir := newFakeDocker(t)
		withFakePath(t, dir)
		_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("cid\n"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "output.inspect"), []byte(`{"bridge":{"IPAddress":"1.2.3.4"}}`), 0644)
		d := NewDockerDriver("", "p", "v1", "http://parent")
		a := testAgent(t, nil, nil) // a.PQCKeys nil
		if err := d.Spawn(context.Background(), a); err != nil {
			t.Fatalf("Spawn: %v", err)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "invocations.log"))
		if strings.Contains(string(log), "DATAWATCH_PQC_") {
			t.Errorf("PQC env should be absent when keys nil:\n%s", log)
		}
	})
}

// ── Spawn failure surfaces combined output ────────────────────────────

func TestDockerDriver_Spawn_ErrorIncludesOutput(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.run"), []byte("Unable to find image\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.run"), []byte("125"), 0644)

	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	err := d.Spawn(context.Background(), a)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "Unable to find") {
		t.Errorf("error should include docker output: %v", err)
	}
}

// ── Status mapping ─────────────────────────────────────────────────────

func TestDockerDriver_Status_Mapping(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	a.DriverInstance = "fake-cid"

	cases := map[string]State{
		"created":    StateStarting,
		"running":    StateStarting, // bootstrap hasn't happened yet
		"paused":     StateStarting,
		"restarting": StateStarting,
		"exited":     StateStopped,
		"dead":       StateStopped,
	}
	for dockerStatus, want := range cases {
		t.Run(dockerStatus, func(t *testing.T) {
			_ = os.WriteFile(filepath.Join(dir, "output.inspect"),
				[]byte(dockerStatus+"\n"), 0644)
			got, err := d.Status(context.Background(), a)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if got != want {
				t.Errorf("docker=%q → %q, want %q", dockerStatus, got, want)
			}
		})
	}
}

// ── Status: missing container treated as stopped ─────────────────────

func TestDockerDriver_Status_NotFound(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.inspect"),
		[]byte("Error: No such object: fake-cid\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.inspect"), []byte("1"), 0644)

	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	a.DriverInstance = "fake-cid"
	got, err := d.Status(context.Background(), a)
	if err != nil {
		t.Fatalf("want no error for missing container, got %v", err)
	}
	if got != StateStopped {
		t.Errorf("state=%s want stopped", got)
	}
}

// ── Logs ────────────────────────────────────────────────────────────────

func TestDockerDriver_Logs(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.logs"),
		[]byte("line1\nline2\n"), 0644)

	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	a.DriverInstance = "fake-cid"
	out, err := d.Logs(context.Background(), a, 50)
	if err != nil {
		t.Fatal(err)
	}
	if out != "line1\nline2\n" {
		t.Errorf("logs=%q", out)
	}
}

// ── Terminate ──────────────────────────────────────────────────────────

func TestDockerDriver_Terminate_NoOpWhenEmptyCID(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	// No DriverInstance set
	if err := d.Terminate(context.Background(), a); err != nil {
		t.Errorf("empty-cid terminate should be no-op, got %v", err)
	}
}

func TestDockerDriver_Terminate_NotFoundIgnored(t *testing.T) {
	dir := newFakeDocker(t)
	withFakePath(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "output.rm"),
		[]byte("Error: No such container: fake\n"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "exitcode.rm"), []byte("1"), 0644)

	d := NewDockerDriver("", "", "", "")
	a := testAgent(t, nil, nil)
	a.DriverInstance = "fake"
	if err := d.Terminate(context.Background(), a); err != nil {
		t.Errorf("not-found should be ignored, got %v", err)
	}
}

func TestDockerDriver_Kind(t *testing.T) {
	d := NewDockerDriver("", "", "", "")
	if d.Kind() != "docker" {
		t.Errorf("Kind=%q want docker", d.Kind())
	}
}
