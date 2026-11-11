package genartifacts

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

func fullRecord() *changerecord.ChangeRecord {
	return &changerecord.ChangeRecord{
		AutomatonID: "abc123",
		Files: []changerecord.FileChange{
			{Path: "internal/server/api.go", Status: "modified"},
			{Path: "internal/server/web/app.js", Status: "modified"},
			{Path: "internal/mcp/tools.go", Status: "added"},
			{Path: "cmd/datawatch/main.go", Status: "modified"},
			{Path: "internal/config/config.go", Status: "modified"},
		},
		Packages:        []string{"internal/server", "internal/mcp"},
		SettingsAdded:   []string{"foo.bar"},
		RoutesAdded:     []string{"/api/foo"},
		LocaleKeysAdded: []changerecord.LocaleKey{{Key: "foo.title", Value: "Foo | title"}},
		TestsAdded:      []string{"TestFoo"},
		Commands: []changerecord.CommandRun{
			{Command: "go build", ExitCode: 0},
			{Command: "go test", ExitCode: 1, Pass: 7, Fail: 2},
		},
	}
}

func TestRenderFullRecordSections(t *testing.T) {
	o := Options{Root: t.TempDir()}
	want := map[Kind][]string{
		GateReport:     {"# Gate report abc123", "| Commands run | 2 |", "| Commands passed | 1 |", "| Commands failed | 1 |", "| Files changed | 5 |", "| Packages touched | 2 |", "| Tests added | 1 |", "| Settings added | 1 |", "| Routes added | 1 |", "| Locale keys added | 1 |"},
		ChangelogDraft: {"## vX.Y.Z — ", "`foo.bar`", "`/api/foo`", "1 new locale keys"},
		ParityDraft:    {"## What changed", "## Settings to add to the mobile app", "## REST contract from routes added", "## New locale keys with English values", "## Screens", "`foo.bar`", "`/api/foo`", `| ` + "`foo.title`" + ` | Foo \| title |`, "internal/server/web/app.js"},
		TrackerRows:    {"REST API", "MCP", "CLI", "Config", "Web UI", "`TestFoo`", "**Yes**"},
		RegistryRow:    {"| NNN |", "abc123"},
	}
	for k, subs := range want {
		got, warns, err := Render(k, fullRecord(), o)
		if err != nil || len(warns) != 0 {
			t.Fatalf("%s: err=%v warns=%v", k, err, warns)
		}
		for _, s := range subs {
			if !strings.Contains(got, s) {
				t.Errorf("%s missing %q in:\n%s", k, s, got)
			}
		}
	}
}

func TestRenderEmptyRecord(t *testing.T) {
	o := Options{Root: t.TempDir()}
	for _, k := range Kinds() {
		for _, rec := range []*changerecord.ChangeRecord{nil, {}} {
			got, _, err := Render(k, rec, o)
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
			if k != TrackerRows && strings.TrimSpace(got) == "" {
				t.Errorf("%s rendered empty", k)
			}
		}
	}
	g, _, _ := Render(ParityDraft, nil, o)
	for _, h := range []string{"## What changed", "## Screens"} {
		if !strings.Contains(g, h) {
			t.Errorf("empty parity missing %q", h)
		}
	}
	g, _, _ = Render(GateReport, nil, o)
	if !strings.Contains(g, "| Commands run | 0 |") {
		t.Errorf("empty gate: %s", g)
	}
}

func TestGateReportIsNumbersOnly(t *testing.T) {
	rec := fullRecord()
	rec.Commands[1].Command = "SECRET-COMMAND-OUTPUT"
	got, _, err := Render(GateReport, rec, Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "SECRET-COMMAND-OUTPUT") || strings.Contains(got, "foo.bar") || strings.Contains(got, "/api/foo") {
		t.Errorf("gate report carries free text:\n%s", got)
	}
	re := regexp.MustCompile(`^\| [A-Za-z ]+ \| \d+ \|$`)
	if strings.Contains(got, "## Cost") {
		t.Errorf("record without cost renders a cost section:\n%s", got)
	}
	for _, l := range strings.Split(got, "\n") {
		if !strings.HasPrefix(l, "| ") || strings.HasPrefix(l, "| Count") {
			continue
		}
		if !re.MatchString(l) {
			t.Errorf("non-count row %q", l)
		}
	}
}

func TestChangelogHasPlaceholderNotVersion(t *testing.T) {
	got, _, _ := Render(ChangelogDraft, fullRecord(), Options{Root: t.TempDir()})
	if !strings.HasPrefix(got, "## vX.Y.Z") || regexp.MustCompile(`v\d+\.\d+\.\d+`).MatchString(got) {
		t.Errorf("version handling wrong:\n%s", got)
	}
}

func TestWriteMarkerAndPaths(t *testing.T) {
	root := t.TempDir()
	o := Options{Root: root}
	rec := fullRecord()
	for _, k := range Kinds() {
		res, err := Generate(k, rec, o)
		if err != nil || res.Status != Written {
			t.Fatalf("%s: %+v %v", k, res, err)
		}
		b, err := os.ReadFile(filepath.Join(root, TrackedPath(k, rec, o)))
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(string(b), "\n"); first != Marker {
			t.Errorf("%s first line %q", k, first)
		}
	}
	for k, want := range map[Kind]string{
		GateReport:  "docs/plans/reports/abc123-gate.md",
		ParityDraft: "docs/plans/parity-issues/abc123.md",
	} {
		if got := TrackedPath(k, rec, o); got != want {
			t.Errorf("%s path %s", k, got)
		}
	}
	// A configured path replaces the default.
	o.Paths = map[Kind]string{GateReport: "out/<automaton-id>.md"}
	if got := TrackedPath(GateReport, rec, o); got != "out/abc123.md" {
		t.Errorf("custom path %s", got)
	}
}

func TestRegenerateOverwritesUntouchedFile(t *testing.T) {
	root := t.TempDir()
	o := Options{Root: root}
	if r, _ := Write(GateReport, "one\n", "a.md", o); r.Status != Written {
		t.Fatal(r)
	}
	r, err := Write(GateReport, "two\n", "a.md", o)
	if err != nil || r.Status != Written {
		t.Fatalf("%+v %v", r, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "a.md"))
	if !strings.HasSuffix(string(b), "two\n") {
		t.Errorf("not regenerated: %q", b)
	}
}

func TestEditedFileNotOverwritten(t *testing.T) {
	root := t.TempDir()
	o := Options{Root: root}
	p := filepath.Join(root, "a.md")

	// No marker.
	_ = os.WriteFile(p, []byte("mine\n"), 0o644)
	if r, _ := Write(GateReport, "x", "a.md", o); r.Status != Skipped || r.Reason == "" {
		t.Errorf("no marker: %+v", r)
	}
	// Edited line.
	_ = os.WriteFile(p, []byte(Marker+"\n"+EditedMarker+"\nmine\n"), 0o644)
	if r, _ := Write(GateReport, "x", "a.md", o); r.Status != Skipped || r.Reason == "" {
		t.Errorf("edited: %+v", r)
	}
	// Body edited after generation (hash mismatch).
	_ = os.Remove(p)
	_, _ = Write(GateReport, "gen\n", "a.md", o)
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, append(b, []byte("person line\n")...), 0o644)
	if r, _ := Write(GateReport, "x", "a.md", o); r.Status != Skipped || r.Reason == "" {
		t.Errorf("hash: %+v", r)
	}
	b, _ = os.ReadFile(p)
	if !strings.Contains(string(b), "person line") || strings.Contains(string(b), "\nx") {
		t.Errorf("edit lost: %q", b)
	}
}

func TestOverrideTemplateReplacesDefault(t *testing.T) {
	root := t.TempDir()
	op := filepath.Join(root, OverridePath(GateReport))
	_ = os.MkdirAll(filepath.Dir(op), 0o755)
	_ = os.WriteFile(op, []byte("custom {{.ID}} runs={{.CommandsRun}}"), 0o644)
	got, warns, err := Render(GateReport, fullRecord(), Options{Root: root})
	if err != nil || len(warns) != 0 || got != "custom abc123 runs=2" {
		t.Errorf("got %q %v %v", got, warns, err)
	}
}

func TestBrokenOverrideFallsBack(t *testing.T) {
	root := t.TempDir()
	op := filepath.Join(root, OverridePath(GateReport))
	_ = os.MkdirAll(filepath.Dir(op), 0o755)
	for _, bad := range []string{"{{.ID", "{{.NoSuchField}}"} {
		_ = os.WriteFile(op, []byte(bad), 0o644)
		got, warns, err := Render(GateReport, fullRecord(), Options{Root: root})
		if err != nil || len(warns) != 1 || !strings.Contains(got, "# Gate report abc123") {
			t.Errorf("%q: got %q %v %v", bad, got, warns, err)
		}
	}
	res, err := Generate(GateReport, fullRecord(), Options{Root: root})
	if err != nil || res.Status != Written || len(res.Warnings) != 1 {
		t.Errorf("generate: %+v %v", res, err)
	}
}

func TestDisabledKindWritesNothing(t *testing.T) {
	root := t.TempDir()
	off := false
	cfg := config.GeneratedArtefacts{ParityDraft: &off}
	eff := cfg.Effective()
	o := Options{Root: root, Settings: &eff}
	if _, _, err := Render(ParityDraft, fullRecord(), o); !errors.Is(err, ErrDisabled) {
		t.Errorf("render err %v", err)
	}
	res, err := Generate(ParityDraft, fullRecord(), o)
	if err != nil || res.Status != Disabled {
		t.Fatalf("%+v %v", res, err)
	}
	if r, _ := Write(ParityDraft, "x", "p.md", o); r.Status != Disabled {
		t.Errorf("write %+v", r)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
	if r, _ := Generate(GateReport, fullRecord(), o); r.Status != Written {
		t.Errorf("enabled kind: %+v", r)
	}
}

func TestWriteRejectsEscapingPath(t *testing.T) {
	if _, err := Write(GateReport, "x", "../out.md", Options{Root: t.TempDir()}); err == nil {
		t.Error("expected error")
	}
}

func TestGateReportCostSection(t *testing.T) {
	usd := 12.345
	rec := fullRecord()
	rec.Cost = &changerecord.CostSummary{Sessions: 4, SessionIDs: []string{"s-aa", "s-bb"}, TokensIn: 1000, TokensOut: 200, CacheRead: 5000,
		CacheCreation: 300, ToolCalls: 42, SpanSeconds: 3725, APIEquivalentUSD: &usd,
		WindowNote: "partial window", UnknownNote: "spend before the ledger horizon is unknown"}
	got, _, err := Render(GateReport, rec, Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"## Cost", "Sessions: s-aa, s-bb", "| Sessions | 4 |", "| Tokens in | 1000 |", "| Tokens out | 200 |",
		"| Cache read | 5000 |", "| Cache creation | 300 |", "| Tool calls | 42 |", "| Span | 1h2m5s |",
		"| API-equivalent estimate, not billed | $12.35 |", "Window: partial window",
		"Unknown spend: spend before the ledger horizon is unknown"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	rec.Cost.APIEquivalentUSD = nil
	got, _, _ = Render(GateReport, rec, Options{Root: t.TempDir()})
	if strings.Contains(got, "$") {
		t.Errorf("no estimate expected:\n%s", got)
	}
}
