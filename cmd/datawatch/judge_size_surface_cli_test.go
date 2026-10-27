package main

import (
	"strings"
	"testing"
)

func TestJudgeSizeSurfaceCLIBodyHasValueOnlyWhenFlagSet(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags(nil)
	body := map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["judge_size_threshold_lines"]; ok {
		t.Fatalf("an omitted flag must not send the key: %v", body)
	}

	cmd = newAutonomousConfigSetCmd()
	if err := cmd.ParseFlags([]string{"--judge-size-threshold-lines", "250"}); err != nil {
		t.Fatal(err)
	}
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if body["judge_size_threshold_lines"] != 250 || len(body) != 1 {
		t.Fatalf("body = %v", body)
	}

	// 0 turns the rule off and is still sent when given.
	cmd = newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--judge-size-threshold-lines", "0"})
	body = map[string]any{}
	if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
		t.Fatal(err)
	}
	if v, ok := body["judge_size_threshold_lines"]; !ok || v != 0 {
		t.Fatalf("explicit 0 must be sent, got %v", body)
	}
}

func TestJudgeSizeSurfaceCLIRejectsNegative(t *testing.T) {
	cmd := newAutonomousConfigSetCmd()
	_ = cmd.ParseFlags([]string{"--judge-size-threshold-lines", "-1"})
	if err := applyAutonomousJudgeFlags(cmd, map[string]any{}); err == nil {
		t.Fatal("negative --judge-size-threshold-lines accepted")
	}
}

func TestJudgeSizeSurfaceCLIHelpListsFlagAndKey(t *testing.T) {
	c := newAutonomousConfigSetCmd()
	help := c.Long + c.UsageString()
	for _, f := range []string{"judge-size-threshold-lines", "judge_size_threshold_lines"} {
		if !strings.Contains(help, f) {
			t.Errorf("config-set help lacks %s", f)
		}
	}
}
