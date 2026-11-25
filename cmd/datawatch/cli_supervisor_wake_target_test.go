package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --session sets or clears the designation and the output shows the target
// and why.
func TestSupervisorWakeCLIDesignatesAndShowsTarget(t *testing.T) {
	var patches []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			var m map[string]any
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &m)
			patches = append(patches, m)
		}
		_, _ = w.Write([]byte(`{"enabled":true,"session":"sup1","target":"host-sup1","target_reason":"designated by supervisor_loop.wake.session and running"}`))
	}))
	defer srv.Close()
	prev := serverURL
	serverURL = srv.URL
	defer func() { serverURL = prev }()
	out, err := runSupervisorLoopCmd(t, "wake", "--session", " sup1 ")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"designated session: sup1", "target: host-sup1", "why: designated by"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := runSupervisorLoopCmd(t, "wake", "--session", ""); err != nil {
		t.Fatal(err)
	}
	if len(patches) != 2 || patches[0]["supervisor_loop.wake.session"] != "sup1" || patches[1]["supervisor_loop.wake.session"] != "" {
		t.Errorf("patches = %v", patches)
	}
}
