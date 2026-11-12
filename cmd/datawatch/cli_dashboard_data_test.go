package main

import (
	"strings"
	"testing"
)

func runDashData(t *testing.T, c *captureDaemon, args ...string) (string, error) {
	t.Helper()
	withCaptureServer(t, c)
	return captureStdoutErr(t, func() error {
		cmd := newDashboardCmd()
		cmd.SetArgs(args)
		return cmd.Execute()
	})
}

func TestDashboardWorkMapCLI(t *testing.T) {
	c := &captureDaemon{replyBody: `{"nodes":[{"id":"p1"}],"running":[]}`}
	out, err := runDashData(t, c, "work-map")
	if err != nil {
		t.Fatalf("%v (%s)", err, out)
	}
	if c.gotMethod != "GET" || c.gotPath != "/api/dashboard/work-map" {
		t.Errorf("request = %s %s", c.gotMethod, c.gotPath)
	}
	if !strings.Contains(out, `"p1"`) {
		t.Errorf("body missing:\n%s", out)
	}
}

func TestDashboardLanePulseCLIWindow(t *testing.T) {
	c := &captureDaemon{replyBody: `{"lanes":[]}`}
	if out, err := runDashData(t, c, "lane-pulse", "--window", "12h"); err != nil {
		t.Fatalf("%v (%s)", err, out)
	}
	if c.gotPath != "/api/dashboard/lane-pulse" || c.gotQuery != "window=12h" {
		t.Errorf("got %s?%s", c.gotPath, c.gotQuery)
	}
}

func TestDashboardSeriesCLIAllSix(t *testing.T) {
	for _, name := range dashboardSeriesNames {
		c := &captureDaemon{replyBody: `{"points":[]}`}
		if out, err := runDashData(t, c, "series", name, "--days", "7"); err != nil {
			t.Fatalf("%s: %v (%s)", name, err, out)
		}
		if c.gotPath != "/api/dashboard/series/"+name || c.gotQuery != "days=7" {
			t.Errorf("%s: got %s?%s", name, c.gotPath, c.gotQuery)
		}
	}
}

func TestDashboardSeriesCLIRejectsUnknown(t *testing.T) {
	c := &captureDaemon{replyBody: `{}`}
	_, err := runDashData(t, c, "series", "nope")
	if err == nil || !strings.Contains(err.Error(), "unknown series") {
		t.Fatalf("want unknown series error, got %v", err)
	}
	if c.gotPath != "" {
		t.Errorf("unknown series reached the daemon: %s", c.gotPath)
	}
}
