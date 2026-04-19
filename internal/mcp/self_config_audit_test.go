// BL110 — self-modify audit sink test.

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestAuditSelfConfig_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit", "config-self-modify.jsonl")
	s := &Server{cfg: &config.MCPConfig{
		AllowSelfConfig:     true,
		SelfConfigAuditPath: path,
	}}

	s.auditSelfConfig("foo.bar", "42")
	s.auditSelfConfig("foo.baz", `"hello"`)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("audit file empty")
	}
	// Quick sanity: each line should be parseable JSON and the keys
	// should match what we wrote.
	lines := []string{}
	cur := ""
	for _, b := range raw {
		if b == '\n' {
			if cur != "" {
				lines = append(lines, cur)
			}
			cur = ""
			continue
		}
		cur += string(b)
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d audit lines, want 2", len(lines))
	}
	var ev SelfConfigAudit
	if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
		t.Fatalf("parse audit: %v", err)
	}
	if ev.Key != "foo.bar" || ev.Value != "42" {
		t.Errorf("event mismatch: %+v", ev)
	}
}

func TestAuditSelfConfig_NilCfg_NoCrash(t *testing.T) {
	// When cfg is nil the function still writes to stderr; the test
	// just asserts it doesn't panic.
	s := &Server{}
	s.auditSelfConfig("x", "y")
}

func TestAuditSelfConfig_NoFilePath_StderrOnly(t *testing.T) {
	s := &Server{cfg: &config.MCPConfig{AllowSelfConfig: true}}
	// SelfConfigAuditPath empty — must not panic, must not error.
	s.auditSelfConfig("x", "y")
}
