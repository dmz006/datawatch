// Tests for the SystemStats.SecretScan field: the
// SetSecretScanStatsFunc callback populates it, the JSON key is
// "secret_scan", and it is absent (nil + omitempty) when the daemon
// never scanned.

package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSecretScanStatsCallbackPopulatesSystemStats(t *testing.T) {
	c := NewCollector("")
	lastRun := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	lastRefusal := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	c.SetSecretScanStatsFunc(func(s *SystemStats) {
		s.SecretScan = &SecretScanStats{
			Runs:          3,
			Findings:      5,
			ShadowReports: 2,
			Refusals:      1,
			LastRunAt:     lastRun,
			LastRefusalAt: lastRefusal,
		}
	})
	c.collect()

	s := c.Latest()
	if s.SecretScan == nil {
		t.Fatal("SecretScan is nil after the callback ran")
	}
	if s.SecretScan.Runs != 3 || s.SecretScan.Findings != 5 ||
		s.SecretScan.ShadowReports != 2 || s.SecretScan.Refusals != 1 {
		t.Errorf("unexpected counter block: %+v", s.SecretScan)
	}
	if !s.SecretScan.LastRunAt.Equal(lastRun) || !s.SecretScan.LastRefusalAt.Equal(lastRefusal) {
		t.Errorf("unexpected timestamps: %+v", s.SecretScan)
	}

	// JSON: the key is exactly "secret_scan" and carries the same tags
	// as the git package's struct.
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"secret_scan":{`,
		`"runs":3`,
		`"findings":5`,
		`"shadow_reports":2`,
		`"refusals":1`,
		`"last_run_at"`,
		`"last_refusal_at"`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("JSON missing %s:\n%s", want, body)
		}
	}
}

func TestSecretScanStatsAbsentWhenNeverScanned(t *testing.T) {
	// No callback at all (a daemon that never wired / never scanned).
	c := NewCollector("")
	c.collect()
	if s := c.Latest(); s.SecretScan != nil {
		t.Fatalf("SecretScan should be nil when the callback is unset: %+v", s.SecretScan)
	}

	// And a callback that has nothing to report leaves it nil, so the
	// key is omitted from the JSON entirely.
	c.SetSecretScanStatsFunc(func(s *SystemStats) {})
	c.collect()
	if s := c.Latest(); s.SecretScan != nil {
		t.Fatalf("SecretScan should stay nil when the callback reports nothing: %+v", s.SecretScan)
	}

	body, err := json.Marshal(c.Latest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(body), "secret_scan") {
		t.Errorf("JSON of a never-scanned daemon must omit secret_scan:\n%s", body)
	}
}
