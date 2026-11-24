package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestParse_Pause(t *testing.T) {
	c := Parse("pause release freeze until friday")
	if c.Type != CmdPause || c.PauseVerb != "set" || c.PauseReason != "release freeze until friday" || c.PauseProject != "" {
		t.Fatalf("global = %+v", c)
	}
	c = Parse("PAUSE project=web reviewing the plan")
	if c.Type != CmdPause || c.PauseVerb != "set" || c.PauseProject != "web" || c.PauseReason != "reviewing the plan" {
		t.Fatalf("project = %+v", c)
	}
	for _, in := range []string{"pause status", "pause", "pause peers"} {
		if c := Parse(in); c.Type != CmdPause || c.PauseVerb != "status" {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
	c = Parse("pause peers=all freeze")
	if !c.PauseAllPeers || c.PauseReason != "freeze" {
		t.Fatalf("peers=all = %+v", c)
	}
	c = Parse("pause peers=laptop,desk freeze")
	if len(c.PausePeers) != 2 || c.PausePeers[0] != "laptop" {
		t.Fatalf("named peers = %+v", c)
	}
}

func TestParse_Resume(t *testing.T) {
	cases := map[string]struct {
		all     bool
		project string
	}{
		"resume":             {false, ""},
		"resume all":         {true, ""},
		"resume project=web": {false, "web"},
		"RESUME ALL":         {true, ""},
	}
	for in, want := range cases {
		c := Parse(in)
		if c.Type != CmdResume || c.PauseAll != want.all || c.PauseProject != want.project {
			t.Errorf("Parse(%q) = %+v", in, c)
		}
	}
	if c := Parse("resume peers=all all"); !c.PauseAll || !c.PauseAllPeers {
		t.Errorf("resume all peers = %+v", c)
	}
}

func TestParse_PauseDoesNotCaptureOtherText(t *testing.T) {
	for _, in := range []string{"paused", "pausebutton", "resumes the build", "pause_x"} {
		if c := Parse(in); c.Type == CmdPause || c.Type == CmdResume {
			t.Errorf("Parse(%q) captured as %s", in, c.Type)
		}
	}
}

type fakePauseREST struct {
	mu      sync.Mutex
	methods []string
	paths   []string
	bodies  []map[string]any
}

func (f *fakePauseREST) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	data, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(data, &body)
	f.methods = append(f.methods, r.Method)
	f.paths = append(f.paths, r.URL.RequestURI())
	f.bodies = append(f.bodies, body)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"paused": true})
}

func TestPauseComm_RoundTrip(t *testing.T) {
	fake := &fakePauseREST{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pause", fake.handler)
	mux.HandleFunc("/api/pause/resume", fake.handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	send := func(msg string) string { return strings.Join(r.HandleTestMessage(msg), "\n") }

	out := send("pause project=web reviewing")
	if !strings.Contains(out, "pause") {
		t.Errorf("pause reply = %q", out)
	}
	b := fake.bodies[0]
	if fake.methods[0] != http.MethodPost || fake.paths[0] != "/api/pause" ||
		b["scope"] != "project" || b["project"] != "web" || b["reason"] != "reviewing" || b["actor"] != "channel" {
		t.Fatalf("pause request = %s %s %v", fake.methods[0], fake.paths[0], b)
	}

	send("pause freeze peers=all")
	if b := fake.bodies[1]; b["scope"] != "global" || b["all_peers"] != true {
		t.Fatalf("global pause body = %v", b)
	}

	send("pause status")
	if fake.methods[2] != http.MethodGet || fake.paths[2] != "/api/pause" {
		t.Fatalf("status request = %s %s", fake.methods[2], fake.paths[2])
	}

	send("resume all")
	if fake.paths[3] != "/api/pause/resume" || fake.bodies[3]["all"] != true || fake.bodies[3]["actor"] != "channel" {
		t.Fatalf("resume all = %s %v", fake.paths[3], fake.bodies[3])
	}
	send("resume project=web")
	if b := fake.bodies[4]; b["scope"] != "project" || b["project"] != "web" {
		t.Fatalf("resume project body = %v", b)
	}
	send("resume")
	if b := fake.bodies[5]; b["scope"] != "global" {
		t.Fatalf("resume body = %v", b)
	}
}

func TestPauseComm_PauseNeedsAReasonBeforeAnythingIsSent(t *testing.T) {
	fake := &fakePauseREST{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/pause", fake.handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	out := strings.Join(r.HandleTestMessage("pause project=web"), "\n")
	if !strings.Contains(out, "reason") {
		t.Errorf("reply = %q, want the usage naming the reason", out)
	}
	if len(fake.bodies) != 0 {
		t.Errorf("nothing may be sent without a reason: %v", fake.bodies)
	}
}
