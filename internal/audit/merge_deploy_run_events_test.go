package audit

import (
	"strings"
	"testing"
	"time"
)

// The refused merge run / deploy run event: a JSON-lines round trip and a CEF
// line with its own signature and escaped values.
func TestMergeDeployRunRefusedAudit_JSONLines(t *testing.T) {
	l, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.Write(Entry{Actor: "peer:a", Action: ActionMergeDeployRunRefused, Subject: "merge_run",
		Details: map[string]any{"setting": "autonomous.merge_deploy_run_enabled", "outcome": "refused"}}); err != nil {
		t.Fatal(err)
	}
	got, err := l.Read(QueryFilter{Action: ActionMergeDeployRunRefused})
	if err != nil || len(got) != 1 {
		t.Fatalf("read: %v %+v", err, got)
	}
	if got[0].Subject != "merge_run" || got[0].Actor != "peer:a" || got[0].Details["outcome"] != "refused" {
		t.Fatalf("entry = %+v", got[0])
	}
}

func TestMergeDeployRunRefusedAudit_CEF(t *testing.T) {
	SetCEFVersionFn(func() string { return `9|0\1` })
	defer SetCEFVersionFn(func() string { return "v?" })
	line := FormatCEFLine(Entry{Timestamp: time.Now(), Actor: "peer:a=b\nCEF:0|evil", Action: ActionMergeDeployRunRefused,
		Subject: "deploy_run", Details: map[string]any{"setting": "k=v\\w"}})
	if want := `CEF:0|datawatch|datawatch|9\|0\\1|102|MergeDeployRunRefused|4|`; !strings.HasPrefix(line, want) {
		t.Fatalf("header = %q, want prefix %q", line, want)
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("raw newline survived: %q", line)
	}
	for _, want := range []string{`suser=peer:a\=b\nCEF:0|evil`, `k\=v\\w`, "deviceCustomString6=deploy_run"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %q", want, line)
		}
	}
}
