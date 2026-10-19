// prd-set-llm / prd-set-task-llm --guardrail-backend / --guardrail-model.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func runSetLLMCmd(t *testing.T, c *cobra.Command, argv []string) map[string]string {
	t.Helper()
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := writeReloadTestConfig(tmp+"/.datawatch/config.yaml", "server:\n  port: "+u.Port()+"\n"); err != nil {
		t.Fatalf("write config: %v", err)
	}
	c.SetArgs(argv)
	c.SetOut(io.Discard)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return got
}

func TestCLISetLLM_LegacyBodyUnchanged(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--backend", "opencode"})
	want := map[string]string{"backend": "opencode", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetLLM_GuardrailOnly(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--guardrail-backend", "ollama", "--guardrail-model", ""})
	want := map[string]string{"guardrail_backend": "ollama", "guardrail_model": "", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetLLM_GuardrailWithLegacy(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetLLMCmd(), []string{"p1", "--effort", "high", "--guardrail-model", "m1"})
	want := map[string]string{"effort": "high", "guardrail_model": "m1", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetTaskLLM_LegacyBodyUnchanged(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskLLMCmd(), []string{"p1", "--task", "t1", "--model", "m"})
	want := map[string]string{"task_id": "t1", "backend": "", "effort": "", "model": "m", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}

func TestCLISetTaskLLM_GuardrailOnly(t *testing.T) {
	got := runSetLLMCmd(t, newAutonomousPRDSetTaskLLMCmd(), []string{"p1", "--task", "t1", "--guardrail-backend", "ollama"})
	want := map[string]string{"task_id": "t1", "guardrail_backend": "ollama", "actor": "operator"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body=%v want %v", got, want)
	}
}
