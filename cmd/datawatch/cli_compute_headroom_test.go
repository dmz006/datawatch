// CLI tests for `datawatch compute headroom` and `--verifier-slots`,
// against a captureDaemon httptest server (see cli_autonomous_subscriptions_test.go).

package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestComputeNodeUpdateVerifierSlotsFlag(t *testing.T) {
	c := &captureDaemon{replyBody: `{"ok":true}`}
	withCaptureServer(t, c)

	cmd := newComputeNodeAddCmd(true)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"gpu-a", "--verifier-slots", "4"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("compute node update --verifier-slots 4: %v", err)
	}
	if c.gotMethod != http.MethodPut || c.gotPath != "/api/compute/nodes/gpu-a" {
		t.Errorf("got %s %s, want PUT /api/compute/nodes/gpu-a", c.gotMethod, c.gotPath)
	}
	body := c.body(t)
	if got := body["verifier_slots"]; got != float64(4) {
		t.Errorf("body verifier_slots = %v, want 4", got)
	}
}

func TestComputeHeadroomMeasureRefusal(t *testing.T) {
	refusal := `{"error":"verifier cap 4 on node gpu-a refused: measured free 2048 MB, need 2560 MB, shortfall 512 MB","node":"gpu-a","free_mb":2048,"shortfall_mb":512}`
	c := &captureDaemon{replyCode: http.StatusConflict, replyBody: refusal}
	withCaptureServer(t, c)

	cmd := newComputeHeadroomMeasureCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"gpu-a"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("want error on 409, got nil")
	}
	// daemonJSONBody returns "HTTP 409: <raw body>"
	msg := err.Error()
	for _, want := range []string{"409", "gpu-a", "2048", "512", "shortfall"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if c.gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", c.gotMethod)
	}
	if c.gotPath != "/api/compute/nodes/gpu-a/headroom" {
		t.Errorf("path = %s, want /api/compute/nodes/gpu-a/headroom", c.gotPath)
	}
}

func TestComputeHeadroomShowSuccess(t *testing.T) {
	body := `{"node":"gpu-a","free_mb":8192,"verifier_slots":2,"ok":true,"measured_at":"2026-11-01T00:00:00Z"}`
	c := &captureDaemon{replyBody: body}
	withCaptureServer(t, c)

	cmd := newComputeHeadroomShowCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"gpu-a"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("compute headroom show: %v", err)
	}
	if c.gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", c.gotMethod)
	}
	if c.gotPath != "/api/compute/nodes/gpu-a/headroom" {
		t.Errorf("path = %s, want /api/compute/nodes/gpu-a/headroom", c.gotPath)
	}
}
