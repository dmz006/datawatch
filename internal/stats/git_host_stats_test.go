// Tests for the SystemStats.GitHosts field.

package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGitHostStatsCallbackPopulatesSystemStats(t *testing.T) {
	c := NewCollector("")
	fixed := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC) // fake clock value
	c.SetGitHostStatsFunc(func(s *SystemStats) {
		s.GitHosts = &GitHostStats{
			Configured:    2,
			WindowSeconds: 3600,
			Operations:    map[string]int{"push": 3, "pull_request": 1, "merge": 0, "tag": 1, "ci_read": 4},
			Failed:        2,
			LastSuccessAt: fixed,
		}
	})
	c.collect()
	got := c.Latest().GitHosts
	if got == nil || got.Configured != 2 || got.Failed != 2 || got.Operations["ci_read"] != 4 || !got.LastSuccessAt.Equal(fixed) {
		t.Fatalf("GitHosts = %+v", got)
	}
	b, err := json.Marshal(c.Latest())
	if err != nil || !strings.Contains(string(b), `"git_hosts"`) {
		t.Fatalf("json missing git_hosts: %v %s", err, b)
	}
}

func TestGitHostStatsOmittedWithoutCallback(t *testing.T) {
	c := NewCollector("")
	c.collect()
	b, _ := json.Marshal(c.Latest())
	if strings.Contains(string(b), `"git_hosts"`) {
		t.Fatal("git_hosts should be omitted")
	}
}
