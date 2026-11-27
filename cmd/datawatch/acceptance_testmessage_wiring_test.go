//go:build !windows

package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// say posts one chat message to the running daemon's own test route, so the
// router is the one the daemon wired.
func (d *startupDaemon) say(method, path, body string) string {
	d.t.Helper()
	req, err := http.NewRequest(method, d.baseURL+path, strings.NewReader(body))
	if err != nil {
		d.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := d.client()
	client.Timeout = 0
	resp, err := client.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		d.t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		d.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, out)
	}
	return string(out)
}

// The chat command follows the live setting on the daemon's test route as it
// does on a real channel: absent while off, answered in report mode.
func TestDaemonStart_TestMessageRouteHasTheAcceptanceSetting(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real daemon")
	}
	d := startStartupDaemon(t, "")
	d.release()
	d.waitFor("a health answer", d.healthy)
	const send = `{"text":"acceptance list"}`

	if got := d.say(http.MethodPost, "/api/test/message", send); strings.Contains(got, "done-when") {
		t.Fatalf("off: the word was read as a command: %s", got)
	}
	d.say(http.MethodPut, "/api/config", `{"acceptance.mode":"report"}`)
	if got := d.say(http.MethodPost, "/api/test/message", send); !strings.Contains(got, "no done-when lists") {
		t.Fatalf("report mode: the command was not answered: %s", got)
	}
	d.say(http.MethodPut, "/api/config", `{"acceptance.mode":"off"}`)
	if got := d.say(http.MethodPost, "/api/test/message", send); strings.Contains(got, "done-when") {
		t.Fatalf("off again: the word was read as a command: %s", got)
	}
}
