package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/mcp"
	"github.com/dmz006/datawatch/internal/router"
)

type rulesCLIDaemon struct {
	puts  []string
	query string
	putQ  string
}

func newRulesCLIDaemon(t *testing.T) *rulesCLIDaemon {
	t.Helper()
	d := &rulesCLIDaemon{}
	cfg := `{"rules_file":"RULES.md","context_files":["A.md"],"digest_mode":"off"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			raw, _ := io.ReadAll(r.Body)
			d.puts = append(d.puts, string(raw))
			d.putQ = r.URL.RawQuery
			cfg = string(raw)
		} else {
			d.query = r.URL.RawQuery
		}
		_, _ = w.Write([]byte(`{"config":` + cfg + `,"policy":{"rules_file":"RULES.md","context_files":["A.md"],"digest_mode":"off","digest_max_bytes":0}}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRulesContextSettingsCLI_Read(t *testing.T) {
	d := newRulesCLIDaemon(t)
	out, err := runLaneFillCmd(t, newAutonomousRulesContextCmd(), []string{"--prd", "p1", "--project-dir", "/srv/x"})
	if err != nil {
		t.Fatal(err)
	}
	if d.query != "prd=p1&project_dir=%2Fsrv%2Fx" {
		t.Errorf("query = %q", d.query)
	}
	if !strings.Contains(out, "rules_file=RULES.md") || !strings.Contains(out, "context_files=A.md") {
		t.Errorf("output = %q", out)
	}
	raw, err := runLaneFillCmd(t, newAutonomousRulesContextCmd(), []string{"--json"})
	if err != nil || !strings.HasPrefix(raw, `{"config"`) {
		t.Errorf("--json: %q %v", raw, err)
	}
}

func TestRulesContextSettingsCLI_Write(t *testing.T) {
	d := newRulesCLIDaemon(t)
	c := newAutonomousRulesContextCmd()
	if _, err := runLaneFillCmd(t, c, []string{"set", "--digest-mode", "no_tools", "--digest-max-bytes", "4096", "--context-file", "B.md", "--context-file", "C.md"}); err != nil {
		t.Fatal(err)
	}
	if len(d.puts) != 1 {
		t.Fatalf("%d PUTs", len(d.puts))
	}
	for _, want := range []string{`"digest_mode":"no_tools"`, `"digest_max_bytes":4096`, `"B.md"`, `"C.md"`, `"rules_file":"RULES.md"`} {
		if !strings.Contains(d.puts[0], want) {
			t.Errorf("PUT lacks %s: %s", want, d.puts[0])
		}
	}
	if _, err := runLaneFillCmd(t, newAutonomousRulesContextCmd(), []string{"set", "--rules-file", "Z.md", "--project-dir", "/srv/z"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.puts[1], `"project_dir":"/srv/z"`) || d.putQ != "project_dir=%2Fsrv%2Fz" {
		t.Errorf("override: %s ? %s", d.puts[1], d.putQ)
	}
}

func TestRulesContextSettingsCLI_RejectsBadValues(t *testing.T) {
	d := newRulesCLIDaemon(t)
	for _, argv := range [][]string{
		{"set", "--digest-mode", "sometimes"},
		{"set", "--digest-max-bytes", "5"},
		{"set", "--rules-file", "../x"},
		{"set", "--context-file", "/abs"},
		{"set"},
	} {
		if _, err := runLaneFillCmd(t, newAutonomousRulesContextCmd(), argv); err == nil {
			t.Errorf("%v must fail", argv)
		}
	}
	if len(d.puts) != 0 {
		t.Errorf("a rejected value must not write: %v", d.puts)
	}
}

// The rules and context read and write exist on every surface.
func TestRulesContextSettingsCLI_ParityAcrossSurfaces(t *testing.T) {
	cli := map[string]bool{}
	for _, c := range newAutonomousCmd().Commands() {
		cli[c.Name()] = true
	}
	if !cli["rules-context"] {
		t.Error("autonomous rules-context missing from the CLI")
	}
	mcpTools := toSet(mcp.RulesContextToolNames())
	for _, n := range []string{"rules_context_status", "rules_context_set"} {
		if !mcpTools[n] {
			t.Errorf("MCP tool %s missing", n)
		}
	}
	comm := toSet(router.RulesContextCommVerbs())
	if !comm["rules"] || !comm["rules set"] || router.Parse("autonomous rules").Type != router.CmdAutonomous {
		t.Error("comm verbs missing")
	}
}
