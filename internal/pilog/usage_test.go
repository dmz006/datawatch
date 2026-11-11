package pilog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadUsage_SumsAssistantTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.jsonl")
	body := `{"type":"message","message":{"role":"assistant","usage":{"input":10,"output":5,"cacheRead":100,"cacheWrite":20}}}
{"type":"message","message":{"role":"user","usage":{"input":999}}}
not json
{"type":"other"}
{"type":"message","message":{"role":"assistant","usage":{"input":1,"output":2,"cacheRead":3,"cacheWrite":4}}}
{"type":"message","message":{"role":"assistant"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := ReadUsage(path)
	if err != nil {
		t.Fatal(err)
	}
	if u != (Usage{Input: 11, Output: 7, CacheRead: 103, CacheWrite: 24}) {
		t.Fatalf("usage = %+v", u)
	}
}

func TestReadUsage_MissingFile(t *testing.T) {
	if _, err := ReadUsage(filepath.Join(t.TempDir(), "none.jsonl")); err == nil {
		t.Fatal("want an error for a missing log")
	}
}
