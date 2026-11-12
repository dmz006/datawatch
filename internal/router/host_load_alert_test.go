package router

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestHostLoadAlert_ConfigureHelpAndListMentionKeys(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(string, string) error { return nil })
	for _, text := range []string{"help", "list"} {
		wg.Add(1)
		r.handleConfigure(Command{Type: CmdConfigure, Text: text})
		wg.Wait()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent=%q", sent)
	}
	for i, s := range sent {
		for _, key := range []string{"autonomous.host_load_alert.mode", "autonomous.host_load_alert.multiple", "autonomous.host_load_alert.minutes"} {
			if !strings.Contains(s, key) {
				t.Errorf("reply %d lacks %s", i, key)
			}
		}
	}
}

func TestHostLoadAlert_ConfigureForwardsKeyAndValue(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	var sent []string
	capture := func(s string) {
		mu.Lock()
		sent = append(sent, s)
		mu.Unlock()
		wg.Done()
	}
	var gotKey, gotValue string
	r := &Router{hostname: "h", backend: &captureBackend{name: "test", capture: capture}}
	r.SetConfigureFunc(func(key, value string) error {
		gotKey, gotValue = key, value
		return nil
	})
	wg.Add(1)
	r.handleConfigure(Command{Type: CmdConfigure, Text: "autonomous.host_load_alert.mode=reap"})
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if gotKey != "autonomous.host_load_alert.mode" || gotValue != "reap" {
		t.Fatalf("configureFn got key=%q value=%q", gotKey, gotValue)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Set autonomous.host_load_alert.mode = reap") {
		t.Fatalf("sent=%q", sent)
	}
}

func hostLoadCommConfigBody(mode string) string {
	return `{"autonomous":{"host_load_alert":{"mode":"` + mode + `","multiple":1.5,"minutes":10}}}`
}

func TestHostLoadComm_VerbRendersAlertMode(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, hostLoadCommConfigBody("alert"))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous host-load"), " ")
	if log.path != "/api/config" {
		t.Errorf("path=%s, want /api/config", log.path)
	}
	for _, want := range []string{"mode alert", "1.5 × the core count for 10 minutes"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestHostLoadComm_VerbRendersReapMode(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, `{"autonomous":{"host_load_alert":{"mode":"reap","multiple":2,"minutes":15}}}`)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous host_load"), " ")
	if log.path != "/api/config" {
		t.Errorf("path=%s, want /api/config", log.path)
	}
	for _, want := range []string{"mode reap", "2 × the core count for 15 minutes", "orphan reaper"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestHostLoadComm_VerbRendersOffMode(t *testing.T) {
	_, port := laneFillCommDaemon(t, http.StatusOK, hostLoadCommConfigBody("off"))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous host-load"), " ")
	if !strings.Contains(got, "off") {
		t.Errorf("reply must say the check is off, got: %q", got)
	}
}

func TestHostLoadComm_TakesNoArguments(t *testing.T) {
	log, port := laneFillCommDaemon(t, http.StatusOK, hostLoadCommConfigBody("alert"))
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous host-load 2"), " ")
	if !strings.Contains(got, "usage: autonomous host-load") {
		t.Errorf("a stray argument must show the usage, got: %q", got)
	}
	if log.calls != 0 {
		t.Errorf("a stray argument must not reach the daemon, got %d calls", log.calls)
	}
}

func TestHostLoadComm_ParityListHasVerb(t *testing.T) {
	verbs := HostLoadCommVerbs()
	if !slices.Contains(verbs, "host-load") {
		t.Errorf("HostLoadCommVerbs() = %v, must contain %q", verbs, "host-load")
	}
	for _, verb := range verbs {
		if c := Parse("autonomous " + verb); c.Type != CmdAutonomous {
			t.Errorf(`Parse("autonomous %s").Type = %v, want CmdAutonomous`, verb, c.Type)
		}
	}
}
