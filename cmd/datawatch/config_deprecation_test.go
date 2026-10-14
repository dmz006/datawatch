package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	auditpkg "github.com/dmz006/datawatch/internal/audit"
)

func TestWarnDeprecatedConfigKeys_OneAuditEntryOnce(t *testing.T) {
	deprecatedConfigWarnOnce = sync.Once{}
	t.Cleanup(func() { deprecatedConfigWarnOnce = sync.Once{} })

	dir := t.TempDir()
	l, err := auditpkg.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()

	keys := []string{"autonomous.max_parallel_tasks"}
	warnDeprecatedConfigKeys(keys, l)
	warnDeprecatedConfigKeys(keys, l) // a reload must not repeat it

	f, err := os.Open(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var entries []auditpkg.Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e auditpkg.Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("bad JSON line %q: %v", sc.Text(), err)
		}
		entries = append(entries, e)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries, want exactly 1: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.Action != "config_deprecated_key" || e.Actor != "system" {
		t.Fatalf("entry = %+v, want action config_deprecated_key by system", e)
	}
	if e.Details["key"] != "autonomous.max_parallel_tasks" || e.Details["replacement"] != "autonomous.max_concurrent_tasks" {
		t.Fatalf("details = %v", e.Details)
	}
}

func TestWarnDeprecatedConfigKeys_NilAuditLog(t *testing.T) {
	deprecatedConfigWarnOnce = sync.Once{}
	t.Cleanup(func() { deprecatedConfigWarnOnce = sync.Once{} })
	warnDeprecatedConfigKeys([]string{"autonomous.max_parallel_tasks"}, nil) // must not panic
}

// AGENT.md Audit Logging Rule — CEF format for config_deprecated_key:
// header pipe-escaping, extension equals/newline escaping, signature triple.
func TestConfigDeprecatedKey_CEF(t *testing.T) {
	auditpkg.SetCEFVersionFn(func() string { return `9|0\x` })
	defer auditpkg.SetCEFVersionFn(func() string { return "v?" })

	line := auditpkg.FormatCEFLine(auditpkg.Entry{
		Actor:  "system",
		Action: "config_deprecated_key",
		Details: map[string]any{
			"key":         "autonomous.max_parallel_tasks",
			"replacement": "a=b\nc",
		},
	})
	wantPrefix := `CEF:0|datawatch|datawatch|9\|0\\x|27|ConfigDeprecatedKey|2|`
	if !strings.HasPrefix(line, wantPrefix) {
		t.Fatalf("line = %q, want prefix %q", line, wantPrefix)
	}
	if !strings.Contains(line, "deviceCustomString1=config_deprecated_key") {
		t.Errorf("action not in extension: %q", line)
	}
	if !strings.Contains(line, "deviceCustomString3Label=key deviceCustomString3=autonomous.max_parallel_tasks") {
		t.Errorf("key detail missing: %q", line)
	}
	if !strings.Contains(line, `deviceCustomString4=a\=b\nc`) {
		t.Errorf("replacement detail not escaped: %q", line)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("CEF line contains a raw newline: %q", line)
	}
}
