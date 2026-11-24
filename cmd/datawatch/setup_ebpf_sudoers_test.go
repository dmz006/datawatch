package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/ebpfsetup"
)

// fakeEBPFRunner records commands and emulates sudo, visudo and getcap over
// an in-memory /etc/sudoers.d. It never touches the host.
type fakeEBPFRunner struct {
	calls   []string
	files   map[string]string
	rule    bool
	denied  bool
	caps    string
	visudoE bool
}

func (f *fakeEBPFRunner) Run(_ context.Context, name string, args ...string) (ebpfsetup.Result, error) {
	f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	switch name {
	case "visudo":
		if f.visudoE {
			return ebpfsetup.Result{Stderr: "syntax error"}, errors.New("exit status 1")
		}
		return ebpfsetup.Result{}, nil
	case "getcap":
		return ebpfsetup.Result{Stdout: f.caps}, nil
	}
	if f.denied {
		return ebpfsetup.Result{Stderr: "a password is required"}, errors.New("exit status 1")
	}
	rest := args
	if len(rest) > 0 && rest[0] == "-n" {
		rest = rest[1:]
	}
	switch {
	case len(rest) > 0 && rest[0] == "-l":
		if f.rule {
			return ebpfsetup.Result{}, nil
		}
		return ebpfsetup.Result{}, errors.New("exit status 1")
	case len(rest) >= 3 && rest[0] == "test":
		if _, ok := f.files[rest[2]]; ok {
			return ebpfsetup.Result{}, nil
		}
		return ebpfsetup.Result{}, exitOne()
	case len(rest) >= 3 && rest[0] == "cat":
		return ebpfsetup.Result{Stdout: f.files[rest[2]]}, nil
	case len(rest) > 0 && rest[0] == "install":
		b, _ := os.ReadFile(rest[len(rest)-2])
		f.files[rest[len(rest)-1]] = string(b)
	case len(rest) > 0 && rest[0] == "rm":
		delete(f.files, rest[len(rest)-1])
	}
	return ebpfsetup.Result{}, nil
}

// exitOne returns a real *exec.ExitError with code 1, like `sudo test -e` on a
// missing file.
func exitOne() error { return exec.Command("sh", "-c", "exit 1").Run() }

func testEBPFEnv(f *fakeEBPFRunner, out *bytes.Buffer) ebpfSetupEnv {
	return ebpfSetupEnv{
		runner: f,
		user:   func() (string, error) { return "alice", nil },
		setcap: func() (string, error) { return "/usr/sbin/setcap", nil },
		binary: func(string) (string, error) { return "/opt/dw/bin/datawatch", nil },
		daemon: func() ebpfsetup.DaemonView {
			return ebpfsetup.DaemonView{Reachable: true, Capability: true, KprobesLoaded: true}
		},
		ebpfEnabled: func() bool { return true },
		tmpDir:      os.TempDir(),
		out:         out,
	}
}

func TestSetupEBPF_PrintModePinned(t *testing.T) {
	var out bytes.Buffer
	if err := runEBPFHostMode(context.Background(), testEBPFEnv(&fakeEBPFRunner{}, &out), "sudoers", ""); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"/etc/sudoers.d/datawatch-ebpf-alice",
		`alice ALL=(root) NOPASSWD: /usr/sbin/setcap cap_bpf\,cap_perfmon\,cap_sys_resource\=ep /opt/dw/bin/datawatch`,
		"# datawatch-managed: ebpf-setcap",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("print mode lacks %q:\n%s", want, out.String())
		}
	}
	if strings.ContainsAny(strings.SplitN(out.String(), "--- begin file ---", 2)[1], "*?") {
		t.Error("printed rule contains a wildcard")
	}
}

func TestSetupEBPF_InstallStatusRemove(t *testing.T) {
	f := &fakeEBPFRunner{files: map[string]string{}}
	var out bytes.Buffer
	env := testEBPFEnv(f, &out)
	if err := runEBPFHostMode(context.Background(), env, "install-sudoers", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.files["/etc/sudoers.d/datawatch-ebpf-alice"]; !ok {
		t.Fatalf("not installed; calls=%v", f.calls)
	}
	f.rule, f.caps = true, "/opt/dw/bin/datawatch cap_sys_resource,cap_perfmon,cap_bpf=ep\n"
	out.Reset()
	if err := runEBPFHostMode(context.Background(), env, "status", ""); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"file has the capabilities:      yes", "running daemon holds them:      yes", "kernel probes loaded:           yes", "passwordless rule present:      yes"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("status lacks %q:\n%s", w, out.String())
		}
	}
	out.Reset()
	if err := runEBPFHostMode(context.Background(), env, "remove-sudoers", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 0 {
		t.Errorf("drop-in still present: %v", f.files)
	}
	out.Reset()
	if err := runEBPFHostMode(context.Background(), env, "remove-sudoers", ""); err != nil || !strings.Contains(out.String(), "nothing to remove") {
		t.Errorf("second remove: %v %s", err, out.String())
	}
}

func TestSetupEBPF_InstallInvalidAndRemoveForeign(t *testing.T) {
	f := &fakeEBPFRunner{files: map[string]string{}, visudoE: true}
	var out bytes.Buffer
	env := testEBPFEnv(f, &out)
	if err := runEBPFHostMode(context.Background(), env, "install-sudoers", ""); err == nil {
		t.Fatal("an invalid rule must fail")
	}
	if len(f.files) != 0 {
		t.Fatal("invalid rule installed")
	}
	f.files["/etc/sudoers.d/datawatch-ebpf-alice"] = "alice ALL=(ALL) ALL\n"
	if err := runEBPFHostMode(context.Background(), env, "remove-sudoers", ""); err == nil || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("want marker refusal, got %v", err)
	}
	if len(f.files) != 1 {
		t.Fatal("foreign file removed")
	}
	if err := runEBPFHostMode(context.Background(), env, "bogus", ""); err == nil {
		t.Error("unknown mode must error")
	}
}

func TestSetupEBPF_ReapplyMode(t *testing.T) {
	f := &fakeEBPFRunner{files: map[string]string{}, rule: true}
	var out bytes.Buffer
	if err := runEBPFHostMode(context.Background(), testEBPFEnv(f, &out), "reapply", ""); err != nil {
		t.Fatal(err)
	}
	want := "sudo -n /usr/sbin/setcap cap_bpf,cap_perfmon,cap_sys_resource=ep /opt/dw/bin/datawatch"
	if !hasCall(f.calls, want) {
		t.Fatalf("calls %v lack %q", f.calls, want)
	}
	// eBPF not enabled and no rule: nothing printed.
	f2 := &fakeEBPFRunner{files: map[string]string{}}
	var out2 bytes.Buffer
	env := testEBPFEnv(f2, &out2)
	env.ebpfEnabled = func() bool { return false }
	if err := runEBPFHostMode(context.Background(), env, "reapply", ""); err != nil || out2.Len() != 0 {
		t.Errorf("err=%v out=%q", err, out2.String())
	}
}

func hasCall(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestSetupEBPF_PlanErrorsSurface(t *testing.T) {
	var out bytes.Buffer
	env := testEBPFEnv(&fakeEBPFRunner{}, &out)
	env.binary = func(string) (string, error) { return "/opt/my dir/datawatch", nil }
	if err := runEBPFHostMode(context.Background(), env, "sudoers", ""); err == nil {
		t.Error("a path with a space must be refused")
	}
	env = testEBPFEnv(&fakeEBPFRunner{}, &out)
	env.setcap = func() (string, error) { return "", errors.New("setcap not found") }
	if err := runEBPFHostMode(context.Background(), env, "sudoers", ""); err == nil {
		t.Error("missing setcap must error")
	}
	env = testEBPFEnv(&fakeEBPFRunner{}, &out)
	env.user = func() (string, error) { return "", errors.New("no user") }
	if err := runEBPFHostMode(context.Background(), env, "sudoers", ""); err == nil {
		t.Error("missing user must error")
	}
}

func TestSetupEBPF_FlagsMutuallyExclusive(t *testing.T) {
	for _, args := range [][]string{{"--sudoers", "--status"}, {"--install-sudoers", "--remove-sudoers"}, {"--disable", "--sudoers"}} {
		cmd := newSetupEBPFCmd()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil {
			t.Errorf("%v accepted together", args)
		}
	}
}

// TestReplaceMainBinary_ReappliesAfterReplace: the update path restores the
// capabilities after the file is replaced, remembers whether the old file had
// them, and does nothing when the replace failed.
func TestReplaceMainBinary_ReappliesAfterReplace(t *testing.T) {
	oldBefore, oldAfter := ebpfCapsBeforeReplace, reapplyEBPFCapsAfterReplace
	defer func() { ebpfCapsBeforeReplace, reapplyEBPFCapsAfterReplace = oldBefore, oldAfter }()

	dir := t.TempDir()
	dest := filepath.Join(dir, "datawatch")
	src := filepath.Join(dir, "new")
	if err := os.WriteFile(dest, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	var gotPath string
	var gotHad, called bool
	var contentAtCall string
	ebpfCapsBeforeReplace = func(string) bool { return true }
	reapplyEBPFCapsAfterReplace = func(p string, had bool) {
		called, gotPath, gotHad = true, p, had
		b, _ := os.ReadFile(p)
		contentAtCall = string(b)
	}
	if err := replaceMainBinary(dest, src); err != nil {
		t.Fatal(err)
	}
	if !called || gotPath != dest || !gotHad || contentAtCall != "new" {
		t.Fatalf("called=%v path=%s had=%v contentAtCall=%q", called, gotPath, gotHad, contentAtCall)
	}

	called = false
	if err := replaceMainBinary(dest, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("replace of a missing source must fail")
	}
	if called {
		t.Error("re-apply ran although the replace failed")
	}
}
