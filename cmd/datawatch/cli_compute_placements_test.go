// CLI surface for `compute placements` (list / approve / deny /
// config-get / config-set). Each verb must hit the daemon endpoint the
// REST handlers expect and pass operator input through (or refuse invalid
// input without touching the daemon). Same fake-daemon pattern as
// cli_compute_tls_test.go.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
)

type placementCLICall struct {
	method  string
	path    string
	rawPath string
	body    map[string]any
}

// placementCLIDaemon answers the endpoints the placements CLI verbs call,
// echoing the decision semantics of the real handlers: 200 for known asks,
// 404 for unknown IDs, 409 for already-decided ones.
type placementCLIDaemon struct {
	mu    sync.Mutex
	calls []placementCLICall
}

func withPlacementCLIDaemon(t *testing.T) *placementCLIDaemon {
	t.Helper()
	d := &placementCLIDaemon{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		d.mu.Lock()
		d.calls = append(d.calls, placementCLICall{method: r.Method, path: r.URL.Path, rawPath: r.RequestURI, body: body})
		d.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/compute/placements":
			_, _ = w.Write([]byte(`{"asks":[{"id":"ask-1","model":"deepseek:8b","node":"gpu-1"}],"pending":1}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/compute/placements/"):
			rest := strings.TrimPrefix(r.URL.Path, "/api/compute/placements/")
			parts := strings.Split(rest, "/")
			if len(parts) != 2 || parts[0] == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`expected /api/compute/placements/<id>/approve or /deny`))
				return
			}
			id, action := parts[0], parts[1]
			switch id {
			case "missing":
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`unknown ask ` + id + ` not found in the placement ask store`))
			case "conflict":
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`already ` + action + `d (decisions are one-shot)`))
			default:
				verb := "approved"
				if action == "deny" {
					verb = "denied"
				}
				_, _ = w.Write([]byte(`{"ask":{"id":"` + id + `","decision":"` + verb + `"}}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/config":
			_, _ = w.Write([]byte(`{"compute_placement":{"missing_model":"ask","mem_headroom_pct":10,"disk_headroom_gb":5,"ask_timeout_minutes":30,"assume_model_size_gb":0},"llm_registry":{"failover":["a","b"]}}`))
		case r.Method == http.MethodPut && r.URL.Path == "/api/config":
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`no such endpoint ` + r.URL.Path))
		}
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	t.Setenv("DATAWATCH_TOKEN", "cli-test-token")
	return d
}

// placementCalls returns a copy of every request the daemon recorded.
func (d *placementCLIDaemon) placementCalls() []placementCLICall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]placementCLICall(nil), d.calls...)
}

// runPlacements executes `placements <args…>` against a root command and
// returns combined command output and captured stdout.
func runPlacements(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "datawatch", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newComputePlacementCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"placements"}, args...))

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	var std bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&std, r); close(done) }()

	_, execErr := root.ExecuteC()

	_ = w.Close()
	<-done
	os.Stdout = oldStdout
	return out.String(), std.String(), execErr
}

// TestComputePlacementsCmdTree checks the subcommand set, the arg guards,
// and the config-set flag declarations.
func TestComputePlacementsCmdTree(t *testing.T) {
	placements := newComputePlacementCmd()
	children := map[string]*cobra.Command{}
	for _, c := range placements.Commands() {
		children[c.Name()] = c
	}
	for _, name := range []string{"list", "approve", "deny", "config-get", "config-set"} {
		if _, ok := children[name]; !ok {
			t.Fatalf("placements is missing subcommand %q (has %v)", name, children)
		}
	}
	for _, name := range []string{"list", "config-get", "config-set"} {
		if children[name].Args == nil {
			t.Errorf("%s: no Args guard declared", name)
		} else if err := children[name].Args(children[name], []string{"x"}); err == nil {
			t.Errorf("%s: Args guard rejected the extra arg but should be cobra.NoArgs — nil check only", name)
		}
	}
	for _, name := range []string{"approve", "deny"} {
		if err := children[name].Args(children[name], nil); err == nil {
			t.Errorf("%s: Args guard accepts zero args, want exact 1", name)
		}
		if err := children[name].Args(children[name], []string{"a", "b"}); err == nil {
			t.Errorf("%s: Args guard accepts two args, want exact 1", name)
		}
		if err := children[name].Args(children[name], []string{"one"}); err != nil {
			t.Errorf("%s: Args guard rejects one arg (%v)", name, err)
		}
	}
	cs := children["config-set"]
	wantFlags := map[string]string{
		"missing-model":        "string",
		"mem-headroom-pct":     "int",
		"disk-headroom-gb":     "int",
		"ask-timeout-minutes":  "int",
		"assume-model-size-gb": "int",
	}
	for name, typ := range wantFlags {
		f := cs.Flags().Lookup(name)
		if f == nil {
			t.Errorf("config-set: missing flag --%s", name)
			continue
		}
		if f.Value.Type() != typ {
			t.Errorf("config-set --%s type = %s, want %s", name, f.Value.Type(), typ)
		}
	}
}

// TestComputePlacementsListHitsEndpoint checks `list` GETs the pending-ask
// endpoint and prints the daemon's list.
func TestComputePlacementsListHitsEndpoint(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, std, err := runPlacements(t, "list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	calls := d.placementCalls()
	if len(calls) != 1 {
		t.Fatalf("list: %d daemon calls, want 1: %+v", len(calls), calls)
	}
	if calls[0].method != http.MethodGet || calls[0].path != "/api/compute/placements" {
		t.Errorf("list: got %s %s, want GET /api/compute/placements", calls[0].method, calls[0].path)
	}
	if !strings.Contains(std, "ask-1") || !strings.Contains(std, "deepseek:8b") {
		t.Errorf("list: output missing the ask: %q", std)
	}
}

// TestComputePlacementsApproveAndDenyPOSTs checks both decision verbs POST
// to /api/compute/placements/<id>/<decision>.
func TestComputePlacementsApproveAndDenyPOSTs(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	cases := []struct {
		verb   string
		suffix string
		want   string
	}{
		{"approve", "approve", "approved"},
		{"deny", "deny", "denied"},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			_, std, err := runPlacements(t, tc.verb, "ask-1")
			if err != nil {
				t.Fatalf("%s: %v", tc.verb, err)
			}
			calls := d.placementCalls()
			last := calls[len(calls)-1]
			if last.method != http.MethodPost || last.path != "/api/compute/placements/ask-1/"+tc.suffix {
				t.Errorf("%s: got %s %s, want POST /api/compute/placements/ask-1/%s", tc.verb, last.method, last.path, tc.suffix)
			}
			if last.body != nil {
				t.Errorf("%s: unexpected request body %v", tc.verb, last.body)
			}
			if !strings.Contains(std, tc.want) {
				t.Errorf("%s: response not echoed in output: %q", tc.verb, std)
			}
		})
	}
}

// TestComputePlacementsEscapesAskID checks an ask ID needing URL escaping
// (a space) is escaped in path position and arrives decoded to the
// daemon's handler.
func TestComputePlacementsEscapesAskID(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, _, err := runPlacements(t, "approve", "needs escape")
	if err != nil {
		t.Fatalf("approve \"needs escape\": %v", err)
	}
	calls := d.placementCalls()
	last := calls[len(calls)-1]
	if last.rawPath != "/api/compute/placements/needs%20escape/approve" {
		t.Errorf("rawPath = %q, want /api/compute/placements/needs%%20escape/approve", last.rawPath)
	}
	if last.path != "/api/compute/placements/needs escape/approve" {
		t.Errorf("path = %q, want decoded single-segment ask id", last.path)
	}
}

// TestComputePlacementsDecisionArgGuards checks wrong arities fail locally
// without contacting the daemon.
func TestComputePlacementsDecisionArgGuards(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	for _, verb := range []string{"approve", "deny"} {
		_, _, err := runPlacements(t, verb)
		if err == nil || !strings.Contains(err.Error(), "accepts 1 arg") {
			t.Errorf("%s (no args): err = %v, want \"accepts 1 arg\"", verb, err)
		}
		_, _, err = runPlacements(t, verb, "a", "b")
		if err == nil || !strings.Contains(err.Error(), "accepts 1 arg") {
			t.Errorf("%s (two args): err = %v, want \"accepts 1 arg\"", verb, err)
		}
	}
	if calls := d.placementCalls(); len(calls) != 0 {
		t.Errorf("arg-guard failures sent %d daemon calls, want 0: %+v", len(calls), calls)
	}
}

// TestComputePlacementsUnknownAskSurfacesDaemonError checks 404 and 409
// from the daemon surface as command errors with the daemon's message.
func TestComputePlacementsUnknownAskSurfacesDaemonError(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, _, err := runPlacements(t, "approve", "missing")
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("approve missing: err = %v, want HTTP 404", err)
	}
	_, _, err = runPlacements(t, "deny", "conflict")
	if err == nil || !strings.Contains(err.Error(), "HTTP 409") {
		t.Errorf("deny conflict: err = %v, want HTTP 409", err)
	}
	_ = d
}

// TestComputePlacementsConfigGetShowsOnlySection checks config-get GETs
// /api/config and prints the compute_placement section only.
func TestComputePlacementsConfigGetShowsOnlySection(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, std, err := runPlacements(t, "config-get")
	if err != nil {
		t.Fatalf("config-get: %v", err)
	}
	calls := d.placementCalls()
	if len(calls) != 1 || calls[0].method != http.MethodGet || calls[0].path != "/api/config" {
		t.Fatalf("config-get: calls = %+v, want GET /api/config", calls)
	}
	for _, phrase := range []string{"missing_model", "mem_headroom_pct", "ask_timeout_minutes"} {
		if !strings.Contains(std, phrase) {
			t.Errorf("config-get: output missing %q: %q", phrase, std)
		}
	}
	if strings.Contains(std, "llm_registry") {
		t.Errorf("config-get: output bleeds another section into the display: %q", std)
	}
}

// TestComputePlacementsConfigSetAllKeys checks every flag lands in the PUT
// body under its dotted compute_placement key with the right JSON type.
func TestComputePlacementsConfigSetAllKeys(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, std, err := runPlacements(t, "config-set",
		"--missing-model=auto_load",
		"--mem-headroom-pct=25",
		"--disk-headroom-gb=40",
		"--ask-timeout-minutes=15",
		"--assume-model-size-gb=12",
	)
	if err != nil {
		t.Fatalf("config-set: %v (output %q)", err, std)
	}
	calls := d.placementCalls()
	if len(calls) != 1 {
		t.Fatalf("config-set: %d calls, want 1: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.method != http.MethodPut || c.path != "/api/config" {
		t.Fatalf("config-set: got %s %s, want PUT /api/config", c.method, c.path)
	}
	want := map[string]any{
		"compute_placement.missing_model":        "auto_load",
		"compute_placement.mem_headroom_pct":     float64(25),
		"compute_placement.disk_headroom_gb":     float64(40),
		"compute_placement.ask_timeout_minutes":  float64(15),
		"compute_placement.assume_model_size_gb": float64(12),
	}
	if len(c.body) != len(want) {
		t.Errorf("config-set body has %d keys, want %d: %v", len(c.body), len(want), c.body)
	}
	for k, v := range want {
		if got, ok := c.body[k]; !ok {
			t.Errorf("config-set body missing key %q", k)
		} else if got != v {
			t.Errorf("config-set body[%s] = %v, want %v", k, got, v)
		}
	}
}

// TestComputePlacementsConfigSetPartial checks only changed keys are put.
func TestComputePlacementsConfigSetPartial(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, _, err := runPlacements(t, "config-set", "--missing-model=refuse")
	if err != nil {
		t.Fatalf("config-set --missing-model=refuse: %v", err)
	}
	calls := d.placementCalls()
	c := calls[len(calls)-1]
	if c.method != http.MethodPut || c.path != "/api/config" {
		t.Fatalf("partial config-set: got %s %s, want PUT /api/config", c.method, c.path)
	}
	if len(c.body) != 1 || c.body["compute_placement.missing_model"] != "refuse" {
		t.Errorf("partial config-set body = %v, want exactly {compute_placement.missing_model: refuse}", c.body)
	}
}

// TestComputePlacementsConfigSetNoFlagsErrors checks the no-op config-set
// is refused locally.
func TestComputePlacementsConfigSetNoFlagsErrors(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	_, _, err := runPlacements(t, "config-set")
	if err == nil || !strings.Contains(err.Error(), "at least one key") {
		t.Fatalf("config-set (no flags): err = %v, want \"at least one key\"", err)
	}
	if calls := d.placementCalls(); len(calls) != 0 {
		t.Errorf("no-flag config-set sent %d daemon calls, want 0: %+v", len(calls), calls)
	}
}

// TestComputePlacementsConfigSetInvalidValues checks each range guard
// refuses out-of-range values without contacting the daemon.
func TestComputePlacementsConfigSetInvalidValues(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	cases := []struct {
		flag  string
		value string
		want  string
	}{
		{"mem-headroom-pct", "120", "0 to 100"},
		{"mem-headroom-pct", "-5", "0 to 100"},
		{"disk-headroom-gb", "-1", ">= 0"},
		{"ask-timeout-minutes", "-1", ">= 0"},
		{"assume-model-size-gb", "-1", ">= 0"},
	}
	for _, tc := range cases {
		_, _, err := runPlacements(t, "config-set", "--"+tc.flag+"="+tc.value)
		if err == nil {
			t.Errorf("--%s=%s: no error, want range guard", tc.flag, tc.value)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("--%s=%s: err = %v, want a message containing %q", tc.flag, tc.value, err, tc.want)
		}
	}
	if calls := d.placementCalls(); len(calls) != 0 {
		t.Errorf("invalid-value config-sends sent %d daemon calls, want 0: %+v", len(calls), calls)
	}
}

// TestComputePlacementsConfigSetBoundaryValues checks the inclusive range
// edges (0 and 100 for mem headroom) are accepted.
func TestComputePlacementsConfigSetBoundaryValues(t *testing.T) {
	d := withPlacementCLIDaemon(t)
	for _, mem := range []string{"0", "100"} {
		_, _, err := runPlacements(t, "config-set", "--mem-headroom-pct="+mem)
		if err != nil {
			t.Fatalf("--mem-headroom-pct %s: %v", mem, err)
		}
		calls := d.placementCalls()
		c := calls[len(calls)-1]
		var want float64
		if mem == "100" {
			want = 100
		}
		if got := c.body["compute_placement.mem_headroom_pct"]; got != want {
			t.Errorf("body[compute_placement.mem_headroom_pct] = %v, want %v", got, want)
		}
	}
}
