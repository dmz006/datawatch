package main

import (
	"net/http"
	"strings"
	"testing"
)

func laneUtilBody(lanes, mix, fill string) string {
	if mix == "" {
		mix = `[]`
	}
	if fill == "" {
		fill = `{"mode": "shadow", "modes": {}, "decisions": []}`
	}
	return `{"window_seconds": 86400, "bucket_count": 24, "lanes": [` + lanes + `], "backend_mix": ` + mix +
		`, "lane_fill": ` + fill + `, "plan_usage": {"plan_points": 100, "points": 12.5, "tokens": 3400, "percent_of_plan": 12.5}}`
}

const (
	laneUtilPlanLine = "Plan usage: 12.5 of 100.0 points (12.5% of plan), 3400 tokens\n"
	laneUtilHead     = "Lane utilization: window 24h00m, 24 buckets\n"
	bar0             = "░░░░░░░░░░░░░░░░░░░░"
	bar100           = "████████████████████"
)

func TestLaneUtilizationAllIdle(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, laneUtilBody(
		`{"id":"gpu-1","busy_share":0,"runs":[],"queue_wait":{"count":0}}`, "", ""))
	out, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/lane-utilization" || d.query != "" {
		t.Errorf("path=%s query=%q", d.path, d.query)
	}
	want := laneUtilHead +
		"gpu-1      " + bar0 + "   0% busy  runs 0, no queue wait\n" +
		"Backend mix: none\n" +
		"Lane fill mode: shadow\nNo decisions yet.\n" + laneUtilPlanLine
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestLaneUtilizationAllBusy(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneUtilBody(
		`{"id":"claude-1","busy_share":1,"runs":[{"prd_id":"p1"},{"prd_id":"p2"}],"queue_wait":{"count":2,"mean_seconds":192}}`,
		`[{"backend":"claude-code","busy_seconds":86400,"share":1}]`,
		`{"mode":"live","modes":{},"decisions":[{"at":"2026-10-24T12:01:03Z","lane_id":"claude-1","action":"start_prd","prd_id":"p2","reason":"lane is idle","executed":true}]}`))
	out, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := laneUtilHead +
		"claude-1   " + bar100 + " 100% busy  runs 2, mean queue wait 3m12s\n" +
		"Backend mix:\n  claude-code    100%  24h00m busy\n" +
		"Lane fill mode: live\nNewest decisions:\n  12:01:03 claude-1 start_prd p2: lane is idle [executed]\n" + laneUtilPlanLine
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestLaneUtilizationFlaggedLane(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneUtilBody(
		`{"id":"gpu-2","busy_share":0.25,"runs":[{}],"queue_wait":{"count":1,"mean_seconds":45},"idle_with_eligible_work":true,"idle_with_eligible_seconds":3900,"first_eligible_item":"p9"}`, "", ""))
	out, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "gpu-2      █████░░░░░░░░░░░░░░░  25% busy  [idle with eligible work 1h05m, first p9]  runs 1, mean queue wait 45s\n"
	if !strings.Contains(out, want) {
		t.Errorf("missing flagged line %q in:\n%s", want, out)
	}
}

func TestLaneUtilizationEmptyLanes(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, laneUtilBody("", "", ""))
	out, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := laneUtilHead + "No lanes.\nBackend mix: none\nLane fill mode: shadow\nNo decisions yet.\n" + laneUtilPlanLine
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
	if strings.Contains(out, "$") {
		t.Errorf("output must never show dollars:\n%s", out)
	}
}

func TestLaneUtilizationFlagsSentAndJSON(t *testing.T) {
	body := laneUtilBody("", "", "")
	d := newLaneFillCLIDaemon(t, http.StatusOK, body)
	out, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), []string{"--window", "7d", "--bucket", "12", "--decisions", "5", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if d.query != "bucket=12&decisions=5&window=7d" {
		t.Errorf("query=%q", d.query)
	}
	if out != body {
		t.Errorf("--json must print the body untouched, got %q", out)
	}
}

func TestLaneUtilizationBadFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"--window", "banana"}, {"--window", "30m"}, {"--window", "31d"}, {"--window", "0d"},
		{"--bucket", "0"}, {"--bucket", "289"}, {"--decisions", "0"}, {"--decisions", "21"},
	} {
		d := newLaneFillCLIDaemon(t, http.StatusOK, laneUtilBody("", "", ""))
		if _, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), argv); err == nil {
			t.Errorf("%v: expected an error", argv)
		}
		if d.calls != 0 {
			t.Errorf("%v: nothing may be sent, saw %d calls", argv, d.calls)
		}
	}
}

func TestLaneUtilizationServerError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusBadRequest, "bad request: bucket must be a whole number from 1 to 288")
	_, err := runLaneFillCmd(t, newAutonomousLaneUtilizationCmd(), nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), "bucket must be") {
		t.Errorf("expected the daemon refusal to pass through, got %v", err)
	}
}
