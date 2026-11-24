package ebpfsetup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// call is one recorded command.
type call struct {
	name string
	args []string
}

func (c call) line() string { return strings.TrimSpace(c.name + " " + strings.Join(c.args, " ")) }

// fakeHost stands in for sudo, visudo, getcap and the file system under
// /etc/sudoers.d. It records every command and never touches the real host.
type fakeHost struct {
	t          *testing.T
	calls      []call
	etc        map[string]string // fake root-owned files by path
	visudoFail bool              // visudo -cf rejects the file
	sudoDenied bool              // every sudo asks for a password
	ruleListed bool              // sudo -n -l <exact command> succeeds
	caps       string            // getcap output
	installed  []string          // install argv recorded
}

func newFake(t *testing.T) *fakeHost { return &fakeHost{t: t, etc: map[string]string{}} }

var errExit1 = func() error {
	err := exec.Command("sh", "-c", "exit 1").Run()
	return err
}()

func (f *fakeHost) Run(_ context.Context, name string, args ...string) (Result, error) {
	f.calls = append(f.calls, call{name, args})
	switch name {
	case "visudo":
		if f.visudoFail {
			return Result{Stderr: "visudo: syntax error on line 2"}, errors.New("exit status 1")
		}
		return Result{Stdout: args[1] + ": parsed OK"}, nil
	case "getcap":
		return Result{Stdout: f.caps}, nil
	case "sudo":
		if f.sudoDenied {
			return Result{Stderr: "sudo: a password is required"}, errors.New("exit status 1")
		}
		rest := args
		if len(rest) > 0 && rest[0] == "-n" {
			rest = rest[1:]
		}
		switch {
		case len(rest) > 0 && rest[0] == "-l":
			if f.ruleListed {
				return Result{Stdout: strings.Join(rest[1:], " ")}, nil
			}
			return Result{Stderr: "sudo: a password is required"}, errors.New("exit status 1")
		case len(rest) >= 3 && rest[0] == "test" && rest[1] == "-e":
			if _, ok := f.etc[rest[2]]; ok {
				return Result{}, nil
			}
			return Result{}, errExit1
		case len(rest) >= 3 && rest[0] == "cat":
			return Result{Stdout: f.etc[rest[2]]}, nil
		case len(rest) >= 1 && rest[0] == "install":
			f.installed = rest
			src, dst := rest[len(rest)-2], rest[len(rest)-1]
			b, err := os.ReadFile(src)
			if err != nil {
				f.t.Fatalf("fake install: %v", err)
			}
			f.etc[dst] = string(b)
			return Result{}, nil
		case len(rest) >= 4 && rest[0] == "rm":
			delete(f.etc, rest[len(rest)-1])
			return Result{}, nil
		default: // sudo setcap ...
			f.installed = nil
			return Result{}, nil
		}
	}
	f.t.Fatalf("unexpected command %s", call{name, args}.line())
	return Result{}, nil
}

func (f *fakeHost) lines() []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.line())
	}
	return out
}

func testPlan(t *testing.T) *Plan {
	t.Helper()
	p, err := NewPlan("alice", "/usr/sbin/setcap", "/opt/dw/bin/datawatch", "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRule_PinnedExactly pins the printed rule byte for byte: the real user,
// the real setcap and binary paths, the exact capability argument with =ep,
// escaped for sudoers, and no wildcard.
func TestRule_PinnedExactly(t *testing.T) {
	p := testPlan(t)
	want := `alice ALL=(root) NOPASSWD: /usr/sbin/setcap cap_bpf\,cap_perfmon\,cap_sys_resource\=ep /opt/dw/bin/datawatch`
	if got := p.Rule(); got != want {
		t.Fatalf("rule\n got: %s\nwant: %s", got, want)
	}
	if strings.ContainsAny(p.Rule(), "*?[]!") {
		t.Fatalf("rule contains a wildcard character: %s", p.Rule())
	}
	if got, want := p.DropIn, "/etc/sudoers.d/datawatch-ebpf-alice"; got != want {
		t.Errorf("drop-in %s want %s", got, want)
	}
	if got, want := strings.Join(p.SetcapArgv(), " "), "/usr/sbin/setcap cap_bpf,cap_perfmon,cap_sys_resource=ep /opt/dw/bin/datawatch"; got != want {
		t.Errorf("argv %q want %q", got, want)
	}
	fc := p.FileContents()
	if !strings.HasPrefix(fc, Marker+"\n") || !strings.HasSuffix(fc, p.Rule()+"\n") {
		t.Errorf("file contents wrong:\n%s", fc)
	}
	d := p.Describe()
	for _, w := range []string{p.DropIn, p.Rule(), "--install-sudoers", "--remove-sudoers", "not writable by anyone else", "sudo visudo -f " + p.DropIn} {
		if !strings.Contains(d, w) {
			t.Errorf("describe lacks %q", w)
		}
	}
}

// TestRule_AcceptedByRealVisudo checks the escaping against the real parser
// when visudo exists (it parses a file without root).
func TestRule_AcceptedByRealVisudo(t *testing.T) {
	v, err := exec.LookPath("visudo")
	if err != nil {
		t.Skip("visudo not installed")
	}
	p := testPlan(t)
	f := filepath.Join(t.TempDir(), "rule")
	if err := os.WriteFile(f, []byte(p.FileContents()), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(v, "-cf", f).CombinedOutput(); err != nil {
		t.Skipf("visudo cannot check a file as this user here: %v: %s", err, out)
	}
	bad := strings.ReplaceAll(p.FileContents(), `\`, "") // unescaped commas must be rejected
	if err := os.WriteFile(f, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(v, "-cf", f).Run(); err == nil {
		t.Error("real visudo accepted an unescaped rule; the escaping test premise is wrong")
	}
}

func TestNewPlan_RejectsUnsafeValues(t *testing.T) {
	for _, c := range []struct{ user, setcap, bin, target string }{
		{"Bad User", "/usr/sbin/setcap", "/opt/x/datawatch", ""},
		{"alice", "/usr/sbin/setcap", "/opt/my dir/datawatch", ""},
		{"alice", "/usr/sbin/setcap", "/opt/x/data*", ""},
		{"alice", "/usr/sbin/setcap", "/opt/x/../y/datawatch", ""},
		{"alice", "/usr/sbin/setcap", "relative/datawatch", ""},
		{"alice", "/usr/sbin/setcap", "/opt/x/a,b", ""},
		{"alice", "/usr/sbin/setcap", "/opt/x/a=b", ""},
		{"alice", "setcap", "/opt/x/datawatch", ""},
		{"alice", "/usr/sbin/setcap", "/opt/x/datawatch", "bad target"},
		{"", "/usr/sbin/setcap", "/opt/x/datawatch", ""},
	} {
		if _, err := NewPlan(c.user, c.setcap, c.bin, c.target); err == nil {
			t.Errorf("NewPlan(%+v) accepted an unsafe value", c)
		}
	}
	p, err := NewPlan("alice", "/usr/sbin/setcap", "/usr/local/bin/datawatch-stats", "stats")
	if err != nil || p.DropIn != "/etc/sudoers.d/datawatch-ebpf-alice-stats" {
		t.Errorf("stats plan: %v %+v", err, p)
	}
}

func TestInstall_ValidatesThenInstalls(t *testing.T) {
	f := newFake(t)
	p := testPlan(t)
	if err := p.Install(context.Background(), f, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got := f.etc[p.DropIn]
	if got != p.FileContents() {
		t.Fatalf("installed content\n%q\nwant\n%q", got, p.FileContents())
	}
	l := f.lines()
	if !strings.HasPrefix(l[0], "visudo -cf ") {
		t.Errorf("first step must be visudo -cf, got %q", l[0])
	}
	last := l[len(l)-1]
	if !strings.HasPrefix(last, "sudo install -o root -g root -m 0440 ") || !strings.HasSuffix(last, " "+p.DropIn) {
		t.Errorf("install command wrong: %q", last)
	}
	// Re-installing over our own marked file is allowed.
	if err := p.Install(context.Background(), f, t.TempDir()); err != nil {
		t.Errorf("reinstall over a marked file: %v", err)
	}
}

func TestInstall_InvalidRuleIsNeverInstalled(t *testing.T) {
	f := newFake(t)
	f.visudoFail = true
	p := testPlan(t)
	err := p.Install(context.Background(), f, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "syntax error") || !strings.Contains(err.Error(), "nothing was installed") {
		t.Fatalf("want a visudo error, got %v", err)
	}
	if len(f.etc) != 0 || f.installed != nil {
		t.Fatal("an invalid rule was installed")
	}
	for _, l := range f.lines() {
		if strings.HasPrefix(l, "sudo ") {
			t.Errorf("sudo was called after validation failed: %s", l)
		}
	}
}

func TestInstall_RefusesForeignFile(t *testing.T) {
	f := newFake(t)
	p := testPlan(t)
	f.etc[p.DropIn] = "alice ALL=(ALL) ALL\n"
	err := p.Install(context.Background(), f, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not overwriting") {
		t.Fatalf("want refusal, got %v", err)
	}
	if f.etc[p.DropIn] != "alice ALL=(ALL) ALL\n" || f.installed != nil {
		t.Fatal("foreign file was overwritten")
	}
}

func TestInstall_SudoFailureSurfaces(t *testing.T) {
	f := newFake(t)
	f.sudoDenied = true
	if err := testPlan(t).Install(context.Background(), f, t.TempDir()); err == nil || !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("want sudo error, got %v", err)
	}
}

func TestRemove_OnlyWithMarker(t *testing.T) {
	p := testPlan(t)
	f := newFake(t)
	f.etc[p.DropIn] = "alice ALL=(ALL) NOPASSWD: ALL\n" // an operator's own file
	removed, err := p.Remove(context.Background(), f)
	if err == nil || removed || !strings.Contains(err.Error(), "marker") {
		t.Fatalf("want refusal without the marker, got removed=%v err=%v", removed, err)
	}
	if _, ok := f.etc[p.DropIn]; !ok {
		t.Fatal("a file without the marker was removed")
	}
	for _, l := range f.lines() {
		if strings.HasPrefix(l, "sudo rm ") {
			t.Errorf("rm was called: %s", l)
		}
	}

	f.etc[p.DropIn] = p.FileContents()
	removed, err = p.Remove(context.Background(), f)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, ok := f.etc[p.DropIn]; ok {
		t.Fatal("marked file still present")
	}
	removed, err = p.Remove(context.Background(), f)
	if err != nil || removed {
		t.Fatalf("absent file: removed=%v err=%v", removed, err)
	}
}

func TestStatus_ThreeParts(t *testing.T) {
	p := testPlan(t)
	f := newFake(t)
	f.caps = p.BinaryPath + " cap_sys_resource,cap_perfmon,cap_bpf=ep\n"
	f.ruleListed = true
	st := p.ReadStatus(context.Background(), f, func() DaemonView {
		return DaemonView{Reachable: true, Capability: false, Message: "CAP_BPF not granted"}
	})
	if !st.FileHasCaps || !st.RulePresent || st.Daemon.Capability {
		t.Fatalf("status %+v", st)
	}
	out := p.Format(st)
	for _, w := range []string{"file has the capabilities:      yes", "running daemon holds them:      no", "passwordless rule present:      yes", "Restart the daemon"} {
		if !strings.Contains(out, w) {
			t.Errorf("status lacks %q:\n%s", w, out)
		}
	}
	// The rule check asks sudo about the exact command, never a wildcard.
	var ruleCall string
	for _, l := range f.lines() {
		if strings.HasPrefix(l, "sudo -n -l") {
			ruleCall = l
		}
	}
	if ruleCall != "sudo -n -l "+strings.Join(p.SetcapArgv(), " ") {
		t.Errorf("rule check %q", ruleCall)
	}

	f2 := newFake(t) // no caps, no rule, no daemon
	st2 := p.ReadStatus(context.Background(), f2, nil)
	out2 := p.Format(st2)
	for _, w := range []string{"file has the capabilities:      no", "unknown (daemon not reachable)", "passwordless rule present:      no", "datawatch setup ebpf"} {
		if !strings.Contains(out2, w) {
			t.Errorf("status lacks %q:\n%s", w, out2)
		}
	}
	// A file with only some of the capabilities is not "has".
	f3 := newFake(t)
	f3.caps = p.BinaryPath + " cap_bpf=ep\n"
	if FileHasCaps(context.Background(), f3, p.BinaryPath) {
		t.Error("partial capability set counted as complete")
	}
}

func TestReapply_WithRule(t *testing.T) {
	p := testPlan(t)
	f := newFake(t)
	f.ruleListed = true
	var out bytes.Buffer
	p.Reapply(context.Background(), f, false, &out)
	want := "sudo -n " + strings.Join(p.SetcapArgv(), " ")
	found := false
	for _, l := range f.lines() {
		if l == want {
			found = true
		}
		if strings.HasPrefix(l, "sudo ") && !strings.HasPrefix(l, "sudo -n") {
			t.Errorf("a sudo call that could prompt: %s", l)
		}
	}
	if !found {
		t.Fatalf("setcap not re-applied with sudo -n; calls: %v", f.lines())
	}
	if !strings.Contains(out.String(), "re-applied") {
		t.Errorf("output: %s", out.String())
	}
}

func TestReapply_PrintsCommandWhenSudoRefuses(t *testing.T) {
	p := testPlan(t)
	f := newFake(t)
	f.sudoDenied = true
	var out bytes.Buffer
	p.Reapply(context.Background(), f, true, &out)
	if !strings.Contains(out.String(), "Restore them with:  "+p.ManualCommand()) {
		t.Errorf("manual command missing:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "--install-sudoers") {
		t.Errorf("rule hint missing:\n%s", out.String())
	}
	// Rule listed but the apply itself is refused: still prints the command.
	f2 := newFake(t)
	f2.ruleListed = true
	f2.sudoDenied = false
	g := &refuseSetcap{fakeHost: f2}
	out.Reset()
	p.Reapply(context.Background(), g, true, &out)
	if !strings.Contains(out.String(), "could not be re-applied") || !strings.Contains(out.String(), p.ManualCommand()) {
		t.Errorf("refused apply output:\n%s", out.String())
	}
}

// refuseSetcap allows the rule listing but refuses the setcap itself.
type refuseSetcap struct{ *fakeHost }

func (r *refuseSetcap) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if name == "sudo" && len(args) > 1 && args[0] == "-n" && args[1] != "-l" {
		r.calls = append(r.calls, call{name, args})
		return Result{Stderr: "sudo: a password is required"}, errors.New("exit status 1")
	}
	return r.fakeHost.Run(ctx, name, args...)
}

func TestReapply_SilentWhenNotWanted(t *testing.T) {
	p := testPlan(t)
	f := newFake(t)
	var out bytes.Buffer
	p.Reapply(context.Background(), f, false, &out)
	if out.Len() != 0 {
		t.Errorf("printed for an operator that never set eBPF up: %s", out.String())
	}
	out.Reset()
	p.Reapply(context.Background(), f, true, &out)
	if !strings.Contains(out.String(), "removed its eBPF capabilities") {
		t.Errorf("a wanted but unrestorable release must say so:\n%s", out.String())
	}
}

func TestExecRunner_RunsRealCommand(t *testing.T) {
	res, err := ExecRunner{}.Run(context.Background(), "sh", "-c", "echo out; echo err >&2")
	if err != nil || strings.TrimSpace(res.Stdout) != "out" || strings.TrimSpace(res.Stderr) != "err" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := (ExecRunner{}).Run(context.Background(), "sh", "-c", "exit 3"); err == nil {
		t.Fatal("want error")
	}
}

func TestFindSetcap(t *testing.T) {
	p, err := FindSetcap()
	if err != nil {
		t.Skip("setcap not installed")
	}
	if !filepath.IsAbs(p) {
		t.Errorf("not absolute: %s", p)
	}
}
