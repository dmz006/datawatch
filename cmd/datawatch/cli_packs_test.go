// CLI tests for `datawatch packs`, against an httptest daemon.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type packsReq struct {
	method, path, query, body string
}

// withPacksDaemon serves every request with replyBody and records them all.
func withPacksDaemon(t *testing.T, replyBody string) *[]packsReq {
	t.Helper()
	var reqs []packsReq
	old := serverURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, packsReq{r.Method, r.URL.Path, r.URL.RawQuery, string(b)})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replyBody))
	}))
	serverURL = srv.URL
	t.Cleanup(func() { srv.Close(); serverURL = old })
	return &reqs
}

func runPacks(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPacksCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestPacksEnableDisable(t *testing.T) {
	for verb, want := range map[string]bool{"enable": true, "disable": false} {
		reqs := withPacksDaemon(t, `{"enabled":true}`)
		if _, err := runPacks(t, verb); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		r := (*reqs)[0]
		var body map[string]any
		_ = json.Unmarshal([]byte(r.body), &body)
		if r.method != "PUT" || r.path != "/api/packs/config" || body["enabled"] != want {
			t.Errorf("%s: got %+v", verb, r)
		}
	}
}

func TestPacksList(t *testing.T) {
	reqs := withPacksDaemon(t, `{"packs":[{"name":"acme","version":"1.0.0"}]}`)
	out, err := runPacks(t, "list")
	if err != nil {
		t.Fatal(err)
	}
	if r := (*reqs)[0]; r.method != "GET" || r.path != "/api/packs" {
		t.Errorf("got %+v", r)
	}
	_ = out
}

func TestPacksInstallAcceptRemove(t *testing.T) {
	reqs := withPacksDaemon(t, `{}`)
	for _, args := range [][]string{
		{"install", "/srv/packs/acme"},
		{"accept", "acme", "1.0.0"},
		{"remove", "acme", "1.0.0"},
	} {
		if _, err := runPacks(t, args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	got := *reqs
	if got[0].method != "POST" || got[0].path != "/api/packs/install" || !strings.Contains(got[0].body, `"source_dir":"/srv/packs/acme"`) {
		t.Errorf("install: %+v", got[0])
	}
	if got[1].method != "POST" || got[1].path != "/api/packs/accept" ||
		!strings.Contains(got[1].body, `"name":"acme"`) || !strings.Contains(got[1].body, `"version":"1.0.0"`) {
		t.Errorf("accept: %+v", got[1])
	}
	if got[2].method != "DELETE" || got[2].path != "/api/packs/acme/1.0.0" {
		t.Errorf("remove: %+v", got[2])
	}
}

func TestPacksArgValidation(t *testing.T) {
	withPacksDaemon(t, `{}`)
	for _, args := range [][]string{
		{"install"}, {"accept", "acme"}, {"remove", "acme"},
		{"inputs", "get", "global"}, {"inputs", "set", "global", "acme"},
	} {
		if _, err := runPacks(t, args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestPacksInputsGetEachLevel(t *testing.T) {
	cases := []struct {
		args  []string
		wants []string
	}{
		{[]string{"inputs", "get", "global", "acme"}, []string{"level=global", "pack=acme"}},
		{[]string{"inputs", "get", "project", "acme", "--key", "/home/x/proj"}, []string{"level=project", "key=%2Fhome%2Fx%2Fproj"}},
		{[]string{"inputs", "get", "automaton", "acme", "--key", "abc123"}, []string{"level=automaton", "key=abc123"}},
	}
	for _, c := range cases {
		reqs := withPacksDaemon(t, `{"inputs":{"region":"eu"}}`)
		if _, err := runPacks(t, c.args...); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		r := (*reqs)[0]
		if r.method != "GET" || r.path != "/api/packs/inputs" {
			t.Errorf("%v: got %+v", c.args, r)
		}
		for _, w := range c.wants {
			if !strings.Contains(r.query, w) {
				t.Errorf("%v: query %q lacks %q", c.args, r.query, w)
			}
		}
	}
}

func TestPacksInputsGetSingleInput(t *testing.T) {
	withPacksDaemon(t, `{"inputs":{"region":"eu"}}`)
	if _, err := runPacks(t, "inputs", "get", "global", "acme", "region"); err != nil {
		t.Fatal(err)
	}
	if _, err := runPacks(t, "inputs", "get", "global", "acme", "missing"); err == nil {
		t.Error("expected an error for an unset input")
	}
}

func TestPacksInputsKeyRequired(t *testing.T) {
	reqs := withPacksDaemon(t, `{}`)
	for _, lvl := range []string{"project", "automaton"} {
		if _, err := runPacks(t, "inputs", "get", lvl, "acme"); err == nil || !strings.Contains(err.Error(), "--key") {
			t.Errorf("get %s: err = %v", lvl, err)
		}
		if _, err := runPacks(t, "inputs", "set", lvl, "acme", "a=b"); err == nil {
			t.Errorf("set %s without key should fail", lvl)
		}
	}
	if _, err := runPacks(t, "inputs", "get", "galaxy", "acme"); err == nil {
		t.Error("bad level should fail")
	}
	if len(*reqs) != 0 {
		t.Errorf("no request should be sent on validation errors, got %v", *reqs)
	}
}

func TestPacksInputsSetMergesAndReplaces(t *testing.T) {
	reqs := withPacksDaemon(t, `{"inputs":{"old":"keep","region":"us"}}`)
	if _, err := runPacks(t, "inputs", "set", "automaton", "acme", "--key", "a1", "region=eu", "url=http://h/?x=1"); err != nil {
		t.Fatal(err)
	}
	got := *reqs
	if len(got) != 2 || got[0].method != "GET" || got[1].method != "PUT" {
		t.Fatalf("requests = %+v", got)
	}
	if got[1].path != "/api/packs/inputs" || !strings.Contains(got[1].query, "level=automaton") || !strings.Contains(got[1].query, "key=a1") {
		t.Errorf("put: %+v", got[1])
	}
	var body struct {
		Inputs map[string]string `json:"inputs"`
	}
	_ = json.Unmarshal([]byte(got[1].body), &body)
	want := map[string]string{"old": "keep", "region": "eu", "url": "http://h/?x=1"}
	if len(body.Inputs) != len(want) {
		t.Fatalf("inputs = %v", body.Inputs)
	}
	for k, v := range want {
		if body.Inputs[k] != v {
			t.Errorf("inputs[%s] = %q, want %q", k, body.Inputs[k], v)
		}
	}

	reqs2 := withPacksDaemon(t, `{}`)
	if _, err := runPacks(t, "inputs", "set", "global", "acme", "--replace", "only=1"); err != nil {
		t.Fatal(err)
	}
	if len(*reqs2) != 1 || (*reqs2)[0].method != "PUT" || !strings.Contains((*reqs2)[0].body, `"only":"1"`) ||
		strings.Contains((*reqs2)[0].body, "old") {
		t.Errorf("replace: %+v", *reqs2)
	}
}

func TestPacksInputsSetRejectsBadPair(t *testing.T) {
	withPacksDaemon(t, `{"inputs":{}}`)
	if _, err := runPacks(t, "inputs", "set", "global", "acme", "novalue"); err == nil {
		t.Error("expected name=value error")
	}
}

func TestPacksDaemonError(t *testing.T) {
	old := serverURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "pack is not installed", http.StatusNotFound)
	}))
	defer srv.Close()
	serverURL = srv.URL
	defer func() { serverURL = old }()
	if _, err := runPacks(t, "accept", "x", "1"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}
