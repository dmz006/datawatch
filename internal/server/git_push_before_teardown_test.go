// git.push_before_teardown REST coverage: off by default, PUT and GET round
// trip, and the value reaches the config the session manager reads live.

package server

import (
	"net/http"
	"testing"
)

func TestGitPushBeforeTeardownDefaultsOff(t *testing.T) {
	s := gitPushServer(t)
	if got := gitPushGet(t, s)["push_before_teardown"]; got != false {
		t.Fatalf("GET push_before_teardown = %v, want false (opt-in)", got)
	}
	if s.cfg.Git.PushBeforeTeardown {
		t.Fatal("config default is on")
	}
}

func TestGitPushBeforeTeardownPutGetRoundTrip(t *testing.T) {
	s := gitPushServer(t)
	if code := gitPushPut(t, s, map[string]any{"git.push_before_teardown": true}); code != http.StatusOK {
		t.Fatalf("PUT: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_before_teardown"]; got != true {
		t.Fatalf("after PUT true, GET = %v", got)
	}
	if !s.cfg.Git.PushBeforeTeardown {
		t.Fatal("the live config did not change")
	}
	if code := gitPushPut(t, s, map[string]any{"git.push_before_teardown": false}); code != http.StatusOK {
		t.Fatalf("PUT false: HTTP %d", code)
	}
	if got := gitPushGet(t, s)["push_before_teardown"]; got != false {
		t.Fatalf("after PUT false, GET = %v", got)
	}
}
