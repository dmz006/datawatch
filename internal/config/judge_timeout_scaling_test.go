package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJudgeTimeoutPerThousandLines_Config(t *testing.T) {
	if got := DefaultConfig().Autonomous.JudgeTimeoutPerThousandLinesSeconds; got != 600 {
		t.Fatalf("default = %d, want 600", got)
	}
	cfg := loadYAML(t, "autonomous:\n  judge_timeout_per_thousand_lines_seconds: 0\n")
	if got := saveAndReload(t, cfg).Autonomous.JudgeTimeoutPerThousandLinesSeconds; got != 0 {
		t.Fatalf("explicit 0 round trip = %d, want 0", got)
	}
	if NormalizeJudgeTimeoutPerThousandLines(0) != 0 {
		t.Fatal("0 must stay 0")
	}
}

func TestJudgeScaledTimeoutSeconds(t *testing.T) {
	cases := []struct{ base, per, lines, thr, want int }{
		{1800, 600, 400, 400, 1800},
		{1800, 600, 50, 400, 1800},
		{1800, 600, 1556, 400, 1800 + 600*1556/1000},
		{1800, 600, 100000, 400, 7200},
		{1800, 0, 1556, 400, 1800},
		{1800, 600, 1556, 0, 1800},
	}
	for _, c := range cases {
		if got := JudgeScaledTimeoutSeconds(c.base, c.per, c.lines, c.thr); got != c.want {
			t.Errorf("%+v = %d", c, got)
		}
	}
}

func TestJudgeTimeoutPerThousandLines_NegativeRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("autonomous:\n  judge_timeout_per_thousand_lines_seconds: -1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "judge_timeout_per_thousand_lines_seconds") {
		t.Fatalf("Load error = %v, want one naming the key", err)
	}
}
