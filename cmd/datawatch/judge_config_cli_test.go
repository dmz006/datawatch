package main

import (
	"strings"
	"testing"
)

func TestJudgeConfigCLI_FlagsBuildBody(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	if err := cmd.ParseFlags([]string{"--judge-policy", "claude-code, qwen", "--judge-timeout-seconds", "900"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	list, _ := body["judge_policy"].([]string)
	if strings.Join(list, ",") != "claude-code,qwen" || body["judge_timeout_seconds"] != 900 {
		t.Fatalf("body = %v", body)
	}

	// An explicit empty --judge-policy clears the list (the judges follow the verifier routing policy).
	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--judge-policy", ""})
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if cleared, ok := body["judge_policy"].([]string); !ok || len(cleared) != 0 {
		t.Fatalf("explicit empty judge-policy must send an empty list, got %v", body["judge_policy"])
	}

	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags(nil)
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil || len(body) != 0 {
		t.Fatalf("unset judge flags must add nothing: %v %v", body, err)
	}

	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--judge-timeout-seconds", "-1"})
	if err := applyAutonomousJudgeFlags(cmd, map[string]any{}); err == nil {
		t.Fatal("negative --judge-timeout-seconds accepted")
	}

	help := newAutonomousConfigSetCmd().Long + newAutonomousConfigSetCmd().UsageString()
	for _, f := range []string{"judge-policy", "judge-timeout-seconds"} {
		if !strings.Contains(help, f) {
			t.Errorf("config-set help lacks --%s", f)
		}
	}
}
