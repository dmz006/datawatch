package delivery

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type timerRig struct {
	t      *testing.T
	script string
	data   string
	state  string
	home   string
	sent   string
}

func timerNew(t *testing.T) *timerRig {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash on this host")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "delivery-outside-timer.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("the outside timer script is missing: %v", err)
	}
	r := &timerRig{t: t, script: script, data: t.TempDir(), state: filepath.Join(t.TempDir(), "state"), home: t.TempDir()}
	r.sent = filepath.Join(r.home, "sent")
	return r
}

func (r *timerRig) write(body string) {
	r.t.Helper()
	dir := filepath.Join(r.data, HeartbeatDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, HeartbeatFile), []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// beat writes a heartbeat as datawatch does, age old.
func (r *timerRig) beat(state string, age time.Duration, indent bool) {
	r.t.Helper()
	at := time.Now().Add(-age).UTC()
	hb := Heartbeat{At: at, Unix: at.Unix(), IntervalSeconds: 300, State: state, Pass: 7, Open: 3}
	b, err := json.Marshal(hb)
	if indent {
		b, err = json.MarshalIndent(hb, "", "  ")
	}
	if err != nil {
		r.t.Fatal(err)
	}
	r.write(string(b) + "\n")
}

func (r *timerRig) exec(env []string, args ...string) (string, int) {
	r.t.Helper()
	cmd := exec.Command("bash", args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + r.home, "DELIVERY_TIMER_STATE_DIR=" + r.state, "SENT=" + r.sent}, env...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return string(out), exit.ExitCode()
	}
	if err != nil {
		r.t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out), 0
}

// plain runs the script as the timer unit does, with no message route.
func (r *timerRig) plain(env ...string) (string, int) {
	return r.exec(env, r.script, r.data)
}

// routed sources the script and replaces the route with one that "sends".
func (r *timerRig) routed(routeStatus string, env ...string) (string, int) {
	const prog = `source "$1"
delivery_timer_send_alert() { printf '%s\n' "$1" >>"$SENT"; return ${ROUTE_STATUS:-0}; }
delivery_timer_main "$2"`
	return r.exec(append(env, "ROUTE_STATUS="+routeStatus), "-c", prog, "timer-test", r.script, r.data)
}

func (r *timerRig) messages() []string {
	b, err := os.ReadFile(r.sent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestTimerStaysQuietOnAFreshHeartbeat(t *testing.T) {
	limit := 2*300*time.Second + 60*time.Second
	for _, tc := range []struct {
		state  string
		age    time.Duration
		indent bool
	}{
		{HeartbeatOK, 0, false},
		{HeartbeatOK, limit - 30*time.Second, true},
		// red with a fresh time: datawatch can tell you itself.
		{HeartbeatRed, time.Minute, false},
		// A small clock difference is not an alert.
		{HeartbeatOK, -30 * time.Second, false},
	} {
		r := timerNew(t)
		r.beat(tc.state, tc.age, tc.indent)
		if out, code := r.routed("0"); code != 0 || r.messages() != nil {
			t.Fatalf("%s aged %s: code=%d messages=%v\n%s", tc.state, tc.age, code, r.messages(), out)
		}
		if out, code := r.plain(); code != 0 {
			t.Fatalf("%s aged %s: code=%d\n%s", tc.state, tc.age, code, out)
		}
	}
}

func TestTimerAlertsAndTheFakeRouteSends(t *testing.T) {
	good := `{"unix":` + "NOW" + `,"interval_seconds":300,"state":"ok"}`
	cases := map[string]struct {
		reason string
		plant  func(r *timerRig)
	}{
		"no file":       {"heartbeat_missing", func(*timerRig) {}},
		"empty":         {"heartbeat_unreadable", func(r *timerRig) { r.write("") }},
		"not json":      {"heartbeat_unreadable", func(r *timerRig) { r.write("DWATCH3\x00\x01garbage") }},
		"cut short":     {"heartbeat_unreadable", func(r *timerRig) { r.write(`{"unix":1700000000,"interval_seconds":300,"state":"ok"`) }},
		"no time":       {"heartbeat_unreadable", func(r *timerRig) { r.write(`{"interval_seconds":300,"state":"ok"}`) }},
		"no interval":   {"heartbeat_unreadable", func(r *timerRig) { r.write(strings.Replace(good, `"interval_seconds":300,`, "", 1)) }},
		"zero interval": {"heartbeat_unreadable", func(r *timerRig) { r.write(strings.Replace(good, `:300`, `:0`, 1)) }},
		"huge interval": {"heartbeat_unreadable", func(r *timerRig) { r.write(strings.Replace(good, `:300`, `:999999999999`, 1)) }},
		"no state":      {"heartbeat_unreadable", func(r *timerRig) { r.write(`{"unix":1700000000,"interval_seconds":300}`) }},
		"unknown state": {"heartbeat_unreadable", func(r *timerRig) { r.beat("fine", 0, false) }},
		"a directory": {"heartbeat_unreadable", func(r *timerRig) {
			if err := os.MkdirAll(filepath.Join(r.data, HeartbeatDir, HeartbeatFile), 0o700); err != nil {
				r.t.Fatal(err)
			}
		}},
		"stale ok":        {"heartbeat_stale", func(r *timerRig) { r.beat(HeartbeatOK, 700*time.Second, false) }},
		"stale red":       {"heartbeat_stale", func(r *timerRig) { r.beat(HeartbeatRed, 48*time.Hour, true) }},
		"from the future": {"heartbeat_in_future", func(r *timerRig) { r.beat(HeartbeatOK, -24*time.Hour, false) }},
		"cannot tell":     {"cannot_tell", func(r *timerRig) { r.beat(HeartbeatCannotTell, 0, false) }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := timerNew(t)
			tc.plant(r)
			out, code := r.routed("0")
			msgs := r.messages()
			if code != 0 || len(msgs) != 1 || !strings.Contains(msgs[0], "(reason: "+tc.reason+")") {
				t.Fatalf("code=%d messages=%q\n%s", code, msgs, out)
			}
			// With no route plugged in, it says what it would report and fails.
			r2 := timerNew(t)
			r2.data = r.data
			out, code = r2.plain()
			if code == 0 || !strings.Contains(out, "(reason: "+tc.reason+")") || !strings.Contains(out, "no message route") {
				t.Fatalf("without a route: code=%d\n%s", code, out)
			}
		})
	}
}

func TestTimerMessageHoldsNoFileContent(t *testing.T) {
	r := timerNew(t)
	r.write(`{"unix":1700000000,"interval_seconds":300,"state":"SECRET words","note":"SECRET"}`)
	out, _ := r.routed("0")
	if strings.Contains(out, "SECRET") || strings.Contains(strings.Join(r.messages(), " "), "SECRET") {
		t.Fatalf("%s %q", out, r.messages())
	}
}

func TestTimerDoesNotRepeatADeliveredAlertForHours(t *testing.T) {
	r := timerNew(t)
	for i := 0; i < 4; i++ {
		if _, code := r.routed("0"); code != 0 {
			t.Fatalf("run %d: code=%d", i, code)
		}
	}
	if n := len(r.messages()); n != 1 {
		t.Fatalf("%d messages for one dead heartbeat", n)
	}
	// A different reason is new.
	r.beat(HeartbeatCannotTell, 0, false)
	r.routed("0")
	if n := len(r.messages()); n != 2 {
		t.Fatalf("a changed reason: %d messages", n)
	}
	// Healthy again, then dead again: told again.
	r.beat(HeartbeatOK, 0, false)
	r.routed("0")
	r.beat(HeartbeatCannotTell, 0, false)
	r.routed("0")
	if n := len(r.messages()); n != 3 {
		t.Fatalf("after recovering: %d messages", n)
	}
	// And again once the repeat time has passed.
	r.routed("0", "DELIVERY_TIMER_REPEAT_SECONDS=0")
	if n := len(r.messages()); n != 4 {
		t.Fatalf("after the repeat time: %d messages", n)
	}
}

func TestTimerAFailedSendIsTriedAtEveryRun(t *testing.T) {
	r := timerNew(t)
	for i := 0; i < 3; i++ {
		if _, code := r.routed("1"); code != 1 {
			t.Fatalf("run %d: a send that failed must exit 1, got %d", i, code)
		}
	}
	if n := len(r.messages()); n != 3 {
		t.Fatalf("%d tries", n)
	}
}

func TestTimerSendsEvenWhenItCannotKeepItsOwnNote(t *testing.T) {
	r := timerNew(t)
	blocker := filepath.Join(r.home, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.state = filepath.Join(blocker, "state") // cannot be created
	for i := 0; i < 2; i++ {
		if out, code := r.routed("0"); code != 0 {
			t.Fatalf("code=%d\n%s", code, out)
		}
	}
	if n := len(r.messages()); n != 2 {
		t.Fatalf("%d messages", n)
	}
}

func TestTimerWritesNothingInTheDataFolder(t *testing.T) {
	r := timerNew(t)
	r.routed("0")
	left, err := os.ReadDir(r.data)
	if err != nil || len(left) != 0 {
		t.Fatalf("%v %v", left, err)
	}
}

func TestTimerDataFolderDefaults(t *testing.T) {
	r := timerNew(t)
	r.beat(HeartbeatOK, 0, false)
	if out, code := r.exec([]string{"DATAWATCH_DATA_DIR=" + r.data}, r.script); code != 0 {
		t.Fatalf("from the environment: %d\n%s", code, out)
	}
	if err := os.Rename(r.data, filepath.Join(r.home, ".datawatch")); err != nil {
		t.Fatal(err)
	}
	if out, code := r.exec(nil, r.script); code != 0 {
		t.Fatalf("from the home folder: %d\n%s", code, out)
	}
	if _, code := r.exec(nil, r.script, "a", "b"); code != 2 {
		t.Fatalf("two arguments: code=%d, want 2", code)
	}
}

func TestTimerUnitsAreShippedAndRunTheScript(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "systemd", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	svc, timer := read("datawatch-delivery-timer.service"), read("datawatch-delivery-timer.timer")
	if !strings.Contains(svc, "Type=oneshot") || !strings.Contains(svc, "delivery-outside-timer") {
		t.Fatalf("%s", svc)
	}
	// The timer must not depend on datawatch running.
	for _, dep := range []string{"Requires=", "BindsTo=", "PartOf=", "After=datawatch"} {
		if strings.Contains(svc+timer, dep) {
			t.Fatalf("the timer depends on another unit: %s", dep)
		}
	}
	if !strings.Contains(timer, "OnUnitActiveSec=5min") || !strings.Contains(timer, "WantedBy=timers.target") {
		t.Fatalf("%s", timer)
	}
}
