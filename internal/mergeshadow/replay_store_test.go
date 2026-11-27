package mergeshadow

import (
	"testing"
	"time"
)

func TestReplayStoreKeepsHandChecksApart(t *testing.T) {
	dir := t.TempDir()
	r := matchedReplay("a", time.Now())
	r.Key = ReplayKey("a", "0123456789abcdef")
	if r.Key != "a-0123456789ab" {
		t.Fatalf("key %q", r.Key)
	}
	// A result recorded while the shadow runs survives its later saves.
	if err := SaveHandChecks(dir, r.Key, map[string]string{"lint": "pass"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveReplay(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := ListReplays(dir)
	if err != nil || len(got) != 1 || len(got[0].HandChecks) != 1 || got[0].HandChecks["lint"] != "pass" {
		t.Fatalf("%v %+v", err, got)
	}
	if err := SaveHandChecks(dir, r.Key, map[string]string{"lint": "passed"}); err == nil {
		t.Error("an unknown result was stored")
	}
	for _, bad := range []string{"../x", "a/b", ""} {
		if err := SaveReplay(dir, &Replay{Key: bad}); err == nil {
			t.Errorf("key %q was accepted", bad)
		}
	}
}

func TestParseHandChecks(t *testing.T) {
	got, err := ParseHandChecks([]string{"lint=pass,build=PASS", " go-test = fail "})
	if err != nil || len(got) != 3 || got["build"] != "pass" || got["go-test"] != "fail" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []string{"lint", "lint=ok", "=pass", "lint=pass,lint=fail"} {
		if _, err := ParseHandChecks([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
