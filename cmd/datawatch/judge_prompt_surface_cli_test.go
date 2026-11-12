package main

import (
	"strings"
	"testing"
)

var judgePromptCLIKeys = []struct {
	flag, key string
	val, max  int
}{
	{"judge-prompt-file-threshold-lines", "judge_prompt_file_threshold_lines", 250, 100000},
	{"judge-delivery-wait-per-thousand-lines-seconds", "judge_delivery_wait_per_thousand_lines_seconds", 45, 3600},
}

func applyJudgeFlag(t *testing.T, flag string, val string) (map[string]any, error) {
	t.Helper()
	cmd := newAutonomousConfigSetCmd()
	if err := cmd.ParseFlags([]string{"--" + flag, val}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	err := applyAutonomousJudgeFlags(cmd, body)
	return body, err
}

func TestJudgePromptSurfaceCLISetAndReadBack(t *testing.T) {
	for _, k := range judgePromptCLIKeys {
		cmd := newAutonomousConfigSetCmd()
		_ = cmd.ParseFlags(nil)
		body := map[string]any{}
		if err := applyAutonomousJudgeFlags(cmd, body); err != nil {
			t.Fatal(err)
		}
		if _, ok := body[k.key]; ok {
			t.Fatalf("%s: omitted flag must not send the key: %v", k.key, body)
		}
		for _, v := range []int{k.val, 0, k.max} {
			body, err := applyJudgeFlag(t, k.flag, itoa(v))
			if err != nil {
				t.Fatalf("%s=%d: %v", k.key, v, err)
			}
			if got, ok := body[k.key]; !ok || got != v || len(body) != 1 {
				t.Fatalf("%s=%d: body = %v", k.key, v, body)
			}
		}
	}
}

func TestJudgePromptSurfaceCLIRejectsOutOfRange(t *testing.T) {
	for _, k := range judgePromptCLIKeys {
		for _, v := range []int{-1, k.max + 1} {
			if _, err := applyJudgeFlag(t, k.flag, itoa(v)); err == nil {
				t.Errorf("--%s %d accepted", k.flag, v)
			}
		}
	}
}

func TestJudgePromptSurfaceCLIHelpListsFlagAndKey(t *testing.T) {
	c := newAutonomousConfigSetCmd()
	help := c.Long + c.UsageString()
	for _, k := range judgePromptCLIKeys {
		for _, f := range []string{k.flag, k.key} {
			if !strings.Contains(help, f) {
				t.Errorf("config-set help lacks %s", f)
			}
		}
	}
}
