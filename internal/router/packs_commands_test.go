package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type packsCall struct{ method, uri, body string }

func packsServer(t *testing.T, calls *[]packsCall, status int, reply string) *Router {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		*calls = append(*calls, packsCall{req.Method, req.URL.RequestURI(), string(b)})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return loopbackRouter(t, srv)
}

func TestParse_Packs(t *testing.T) {
	for in, text := range map[string]string{"packs": "", "PACKS list": "list", "packs accept a 1.0.0": "accept a 1.0.0"} {
		c := Parse(in)
		if c.Type != CmdPacks || c.Text != text {
			t.Errorf("Parse(%q) = %q/%q", in, c.Type, c.Text)
		}
	}
	if Parse("packsx").Type == CmdPacks {
		t.Error("packsx must not parse as packs")
	}
}

func TestPacksCommands_Routes(t *testing.T) {
	cases := []struct {
		in, method, uri, body string
	}{
		{"packs", "GET", "/api/packs", ""},
		{"packs list", "GET", "/api/packs", ""},
		{"packs setting", "GET", "/api/packs/config", ""},
		{"packs setting enabled=true active=a,b", "PUT", "/api/packs/config", `{"active":["a","b"],"enabled":true}`},
		{"packs setting active=none", "PUT", "/api/packs/config", `{"active":[]}`},
		{"packs install /srv/pack", "POST", "/api/packs/install", `{"source_dir":"/srv/pack"}`},
		{"packs accept demo 1.2.0", "POST", "/api/packs/accept", `{"name":"demo","version":"1.2.0"}`},
		{"packs remove demo 1.2.0", "DELETE", "/api/packs/demo/1.2.0", ""},
		{"packs inputs get global demo", "GET", "/api/packs/inputs?level=global&pack=demo", ""},
		{"packs inputs get project demo proj1", "GET", "/api/packs/inputs?key=proj1&level=project&pack=demo", ""},
		{"packs inputs set automaton demo a1 x=1 y=two", "PUT", "/api/packs/inputs?key=a1&level=automaton&pack=demo", `{"inputs":{"x":"1","y":"two"}}`},
	}
	for _, c := range cases {
		var calls []packsCall
		r := packsServer(t, &calls, 200, `{"ok":true}`)
		out := strings.Join(r.HandleTestMessage(c.in), "\n")
		if len(calls) != 1 {
			t.Fatalf("%q: %d calls, want 1 (%s)", c.in, len(calls), out)
		}
		got := calls[0]
		if got.method != c.method || got.uri != c.uri || got.body != c.body {
			t.Errorf("%q: got %+v, want %s %s %s", c.in, got, c.method, c.uri, c.body)
		}
		if !strings.Contains(out, `"ok":true`) {
			t.Errorf("%q: reply lacks body: %s", c.in, out)
		}
	}
}

func TestPacksCommands_Refusals(t *testing.T) {
	for _, in := range []string{
		"packs bogus",
		"packs install",
		"packs accept onlyname",
		"packs remove ../x 1",
		"packs setting enabled=maybe",
		"packs setting nope=1",
		"packs inputs get weird demo",
		"packs inputs get project demo",
		"packs inputs set global",
	} {
		var calls []packsCall
		r := packsServer(t, &calls, 200, `{}`)
		out := strings.Join(r.HandleTestMessage(in), "\n")
		if len(calls) != 0 {
			t.Errorf("%q reached the daemon", in)
		}
		if !strings.Contains(out, "failed") {
			t.Errorf("%q: no failure reply: %s", in, out)
		}
	}
}

func TestPacksCommands_DaemonError(t *testing.T) {
	var calls []packsCall
	r := packsServer(t, &calls, 400, "pack not installed")
	out := strings.Join(r.HandleTestMessage("packs accept demo 9"), "\n")
	if !strings.Contains(out, "packs accept failed") || !strings.Contains(out, "pack not installed") {
		t.Errorf("daemon refusal not surfaced: %s", out)
	}
}
