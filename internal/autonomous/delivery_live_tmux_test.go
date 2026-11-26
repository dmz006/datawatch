// One answer send and one wake send failed for real, then delivered once. The
// production session manager and tmux layer run unchanged; a "tmux" wrapper
// first on PATH pins every call to a throwaway server with its own -S socket
// under a private folder, so nothing here can reach another tmux server.

package autonomous

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
	"github.com/dmz006/datawatch/internal/supervisorloop"
)

const livePrompt = ` Bash command

   rm -rf ./build
   Remove build output

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands
   3. No, and tell Claude what to do differently (esc)
`

type livePane struct {
	t    *testing.T
	dir  string
	sock string
	real string
	mgr  *session.Manager
}

// newLivePane starts a pane that shows an approval prompt until clearPrompt,
// then an idle screen. Every line that reaches its terminal, at any time, ends
// up in the received file.
func newLivePane(t *testing.T, sessionID string) *livePane {
	t.Helper()
	realBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not found")
	}
	dir, err := os.MkdirTemp("", "dwlv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	p := &livePane{t: t, dir: dir, sock: filepath.Join(dir, "s"), real: realBin}
	if len(p.sock) > 100 {
		t.Skip("socket path too long for a unix socket")
	}
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("bin/tmux", "#!/bin/sh\nunset TMUX TMUX_TMPDIR\nexec '"+realBin+"' -S '"+p.sock+"' -f /dev/null \"$@\"\n", 0o700)
	write("prompt", livePrompt, 0o600)
	write("pane.sh", `#!/bin/sh
cd "$1" || exit 1
cat prompt
while [ ! -f cleared ]; do sleep 0.05; done
printf '\033[2J\033[H'
tmux clear-history
printf '  Done.\n\n  ? for shortcuts\n'
exec cat >> received
`, 0o700)
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", "")
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

	p.mgr, err = session.NewManager("testhost", filepath.Join(dir, "data"), "/bin/true", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.mgr.Close() })
	if out, err := p.tmux("new-session", "-d", "-x", "120", "-y", "40", "-s", "cs-live", "sh '"+filepath.Join(dir, "pane.sh")+"' '"+dir+"'"); err != nil {
		t.Fatalf("start throwaway tmux: %v: %s", err, out)
	}
	t.Cleanup(func() { _, _ = p.tmux("kill-server") }) // the private -S server only
	if err := p.mgr.SaveSession(&session.Session{
		ID: sessionID, FullID: sessionID, Hostname: "testhost", TmuxSession: "cs-live",
		BackendFamily: "claude-code", State: session.StateWaitingInput,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	p.waitScreen("Do you want to proceed?")
	return p
}

func (p *livePane) tmux(args ...string) (string, error) {
	out, err := exec.Command(p.real, append([]string{"-S", p.sock, "-f", os.DevNull}, args...)...).CombinedOutput() // #nosec G204 -- fixed test arguments
	return string(out), err
}

func (p *livePane) screen() string {
	out, _ := p.tmux("capture-pane", "-p", "-t", "cs-live")
	return out
}

func (p *livePane) waitScreen(want string) {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(p.screen(), want) {
		if time.Now().After(deadline) {
			p.t.Fatalf("screen never showed %q:\n%s", want, p.screen())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (p *livePane) clearPrompt() {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.dir, "cleared"), nil, 0o600); err != nil {
		p.t.Fatal(err)
	}
	p.waitScreen("? for shortcuts")
}

// lines counts the received lines holding want, waiting for the first one and
// then a little longer for a second.
func (p *livePane) lines(want string) int {
	p.t.Helper()
	count := func() int {
		b, _ := os.ReadFile(filepath.Join(p.dir, "received"))
		n := 0
		for _, l := range strings.Split(string(b), "\n") {
			if strings.Contains(l, want) {
				n++
			}
		}
		return n
	}
	deadline := time.Now().Add(10 * time.Second)
	for count() == 0 && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	return count()
}

func TestLiveAnswerSendFailsThenArrivesOnce(t *testing.T) {
	p := newLivePane(t, "worker-sess")
	f := newDeliveryFixture(t)
	f.m.SetSessionContinueLineFn(func(sessionID, line string) error {
		return p.mgr.SendInput(sessionID, line, "automaton") // as the daemon wires it
	})
	ans := f.decide(t)

	f.tick()
	f.tick()
	if got := f.delivery(t); got != "not_typed" {
		t.Fatalf("with a real prompt on screen: delivery = %q, want not_typed (%v)", got, f.record(t))
	}
	if s := p.screen(); strings.Contains(s, "automaton decision") {
		t.Fatalf("text reached the prompt:\n%s", s)
	}
	if al := f.raised(); len(al) != 1 {
		t.Fatalf("alerts = %q, want the one waiting warning", al)
	}
	t.Logf("refused for real: %s", f.raised()[0])

	p.clearPrompt()
	f.tick()
	f.tick()
	if n := p.lines("answer=" + ans.ID); n != 1 {
		t.Fatalf("the worker received the decision %d times, want 1", n)
	}
	if got := f.delivery(t); got != "sent" {
		t.Fatalf("delivery = %q, want sent", got)
	}
}

func TestLiveWakeSendFailsThenArrivesOnce(t *testing.T) {
	p := newLivePane(t, "sup-sess")
	sub := &wakeTestSubscriber{}
	clock := &wakeTimerClock{now: time.Now()}
	var alerts []string
	st := supervisorloop.DefaultWakeSettings()
	st.Enabled, st.Kinds, st.CoalesceSeconds = true, []string{"task.failed"}, 0
	set := RegisterSupervisorWake(SupervisorWakeConfig{
		Subscriber: sub, Settings: st, Clock: clock,
		Session: func() string { return "sup-sess" },
		SendInput: func(id, text string) error {
			return p.mgr.SendInput(id, text, "supervisor-wake") // as the daemon wires it
		},
		Alert: func(level, title, body string) { alerts = append(alerts, level+"|"+title+"|"+body) },
	})
	defer set.Close()
	sub.emit(LifecycleEvent{Kind: "task.failed", PRDID: "a1", TaskID: "t1"})
	set.Wait()
	clock.pass(10 * time.Minute) // retries while the prompt is still up
	if s := p.screen(); strings.Contains(s, "supervisor wake") {
		t.Fatalf("text reached the prompt:\n%s", s)
	}
	if len(alerts) != 1 {
		t.Fatalf("alerts = %q, want one", alerts)
	}
	t.Logf("refused for real: %s", alerts[0])

	p.clearPrompt()
	clock.pass(time.Hour)
	if n := p.lines("supervisor wake: task.failed automaton=a1 task=t1"); n != 1 {
		t.Fatalf("the supervisor received the wake %d times, want 1", n)
	}
}

// A one-time message against a real prompt: nothing reaches the pane while the
// prompt is up, and it is typed once after. The session has a channel bridge
// port, which is never used.
func TestLiveOneTimeMessageNeverAnswersAPromptAndArrivesOnce(t *testing.T) {
	p := newLivePane(t, "ops-sess")
	r := newOneShotRig(t)
	r.e.cfg.FindSession = func(string) (string, int, bool) { return "ops-sess", 9013, true }
	r.e.cfg.SendInput = p.mgr.SendInput // as the daemon wires it
	sub := r.add(t)

	r.fire()
	r.waitState(t, sub.ID, "not_typed")
	settle()
	r.retries(3) // retries while the prompt is still up
	if s := p.screen(); strings.Contains(s, oneShotText) || !strings.Contains(s, "Do you want to proceed?") {
		t.Fatalf("the prompt was touched:\n%s", s)
	}
	if _, reason, _, _ := r.delivery(t, sub.ID); reason != "prompt_on_screen" {
		t.Fatalf("reason = %q", reason)
	}
	if v := r.view(); len(v.alerts) != 1 {
		t.Fatalf("alerts = %q, want one", v.alerts)
	} else {
		t.Logf("refused for real: %s", v.alerts[0])
	}

	p.clearPrompt()
	r.retries(3)
	r.waitGone(t, sub.ID)
	r.retries(2)
	if n := p.lines(oneShotText); n != 1 {
		t.Fatalf("the session received the one-time message %d times, want 1", n)
	}
	if v := r.view(); len(v.bridged) != 0 {
		t.Fatalf("sent over the bridge: %q", v.bridged)
	}
}
