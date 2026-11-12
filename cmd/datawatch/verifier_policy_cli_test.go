// Verifier routing CLI: the config-set flags, verifier-policy / verifier-policy-set,
// and the per-Automaton and per-task override commands.

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type verifierPolicyCall struct {
	method, path string
	body         map[string]any
}

// runVerifierPolicyCmd runs c against a fake daemon that answers reply and
// records the request it received.
func runVerifierPolicyCmd(t *testing.T, c *cobra.Command, argv []string, reply string) verifierPolicyCall {
	t.Helper()
	var got verifierPolicyCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got.method, got.path = r.Method, r.URL.Path
		_ = json.Unmarshal(raw, &got.body)
		_, _ = w.Write([]byte(reply))
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

func TestVerifierPolicyCLI_ConfigSetFlagsBuildBody(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	err := cmd.ParseFlags([]string{
		"--verifier-policy-mode", "prefer-free",
		"--verifier-policy-backends", "qwen, claude-code",
		"--verifier-policy-claude-session-cap", "6",
		"--verifier-policy-claude-cap-window-minutes", "0",
		"--verifier-policy-force-stronger-on-retry",
		"--verifier-policy-final-gate-stronger=false",
		"--verifier-policy-stronger-backend", "claude-code",
		"--verifier-policy-stronger-model", "",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if err := applyVerifierPolicyFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"mode":                      "prefer-free",
		"backends":                  []string{"qwen", "claude-code"},
		"claude_session_cap":        6,
		"claude_cap_window_minutes": 0,
		"force_stronger_on_retry":   true,
		"final_gate_stronger":       false,
		"stronger_backend":          "claude-code",
		"stronger_model":            "",
	}
	if !reflect.DeepEqual(body["verifier_policy"], want) {
		t.Fatalf("verifier_policy = %#v, want %#v", body["verifier_policy"], want)
	}
}

func TestVerifierPolicyCLI_UnsetFlagsAddNothing(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags(nil)
	body := map[string]any{}
	if err := applyVerifierPolicyFlags(cmd, body); err != nil || len(body) != 0 {
		t.Fatalf("unset flags must add nothing: %v %v", body, err)
	}
}

func TestVerifierPolicyCLI_NegativeNumberRejected(t *testing.T) {
	for _, flag := range []string{"--verifier-policy-claude-session-cap", "--verifier-policy-claude-cap-window-minutes"} {
		cmd := newAutonomousConfigSetCmd()
		_ = cmd.ParseFlags([]string{flag, "-3"})
		if err := applyVerifierPolicyFlags(cmd, map[string]any{}); err == nil {
			t.Errorf("%s -3 accepted", flag)
		}
	}
}

func TestVerifierPolicyCLI_FlagsMergeIntoJSONObject(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--verifier-policy-mode", "balance"})
	body := map[string]any{"verifier_policy": map[string]any{"claude_session_cap": float64(4), "mode": "fixed"}}
	if err := applyVerifierPolicyFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"claude_session_cap": float64(4), "mode": "balance"}
	if !reflect.DeepEqual(body["verifier_policy"], want) {
		t.Fatalf("verifier_policy = %#v, want %#v", body["verifier_policy"], want)
	}
}

func TestVerifierPolicyCLI_ConfigSetHelpNamesEveryFlag(t *testing.T) {
	help := newAutonomousConfigSetCmd().Long + newAutonomousConfigSetCmd().UsageString()
	for _, d := range verifierPolicyFlagDefs {
		if !strings.Contains(help, "--"+d.flag) {
			t.Errorf("config-set help lacks --%s", d.flag)
		}
		if !strings.Contains(help, "verifier_policy."+d.field) {
			t.Errorf("config-set help lacks verifier_policy.%s", d.field)
		}
	}
}

func TestVerifierPolicyCLI_SetSendsPut(t *testing.T) {
	got := runVerifierPolicyCmd(t, newAutonomousVerifierPolicySetCmd(),
		[]string{"--verifier-policy-mode", "claude-only", "--verifier-policy-backends", ""}, `{}`)
	if got.method != http.MethodPut || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	want := map[string]any{"verifier_policy": map[string]any{"mode": "claude-only", "backends": []any{}}}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCLI_SetNeedsAFlag(t *testing.T) {
	cmd := newAutonomousVerifierPolicySetCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("verifier-policy-set with no flag succeeded")
	}
}

func TestVerifierPolicyCLI_ShowReadsConfig(t *testing.T) {
	got := runVerifierPolicyCmd(t, newAutonomousVerifierPolicyCmd(), nil,
		`{"enabled":true,"verifier_policy":{"mode":"balance"}}`)
	if got.method != http.MethodGet || got.path != "/api/autonomous/config" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
}

func TestVerifierPolicyCLI_PRDOverrideOnlySetFlags(t *testing.T) {
	got := runVerifierPolicyCmd(t, newAutonomousPRDSetVerifierPolicyCmd(),
		[]string{"p1", "--mode", "gpu-only"}, `{}`)
	if got.path != "/api/autonomous/prds/p1/set_verifier_policy" {
		t.Fatalf("path = %s", got.path)
	}
	want := map[string]any{"mode": "gpu-only", "actor": "operator"}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCLI_TaskOverrideEmptyClears(t *testing.T) {
	got := runVerifierPolicyCmd(t, newAutonomousPRDSetTaskVerifierPolicyCmd(),
		[]string{"p1", "--task", "t1", "--mode", "", "--backends", "qwen,claude-code"}, `{}`)
	if got.path != "/api/autonomous/prds/p1/set_task_verifier_policy" {
		t.Fatalf("path = %s", got.path)
	}
	want := map[string]any{"task_id": "t1", "mode": "", "backends": []any{"qwen", "claude-code"}, "actor": "operator"}
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("body = %#v, want %#v", got.body, want)
	}
}

func TestVerifierPolicyCLI_TaskOverrideNeedsTask(t *testing.T) {
	cmd := newAutonomousPRDSetTaskVerifierPolicyCmd()
	cmd.SetArgs([]string{"p1", "--mode", "balance"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Fatal("task override without --task succeeded")
	}
}

func TestVerifierPolicyCLI_CommandsRegistered(t *testing.T) {
	root := newAutonomousCmd()
	for _, name := range []string{"verifier-policy", "verifier-policy-set", "prd-set-verifier-policy", "prd-set-task-verifier-policy"} {
		found := false
		for _, c := range root.Commands() {
			if c.Name() == name {
				found = true
			}
		}
		if !found {
			t.Errorf("autonomous %s is not registered", name)
		}
	}
}
