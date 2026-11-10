// repo_ci_watch_poll_overdue.go — the repo CI watch overdue alert: the
// operator is told once when the watch has gone too long without a
// successful poll of its configured repositories.
//
// The coordinator calls repoCIWatchOverdueCheck on every idle-loop tick,
// beside repoCIWatchPass. A poll is overdue when the last successful poll
// (or, before any success, the instant the watch was first seen active) is
// older than poll_seconds plus alert_minutes. Every threshold comes from
// the Effective* accessors in repo_ci_watch_config.go; the overdue check
// adds no configuration key.
//
//   - Mode off (or no repositories) raises nothing and resets the episode
//     state, so turning the watch back on starts a fresh baseline.
//   - alert_minutes 0 turns the overdue alert off.
//   - One alert per overdue episode. A successful poll (every configured
//     repository polled to completion, see pollRepo) ends the episode, and
//     the next overdue episode alerts again.
package autonomous

import (
	"fmt"
	"strings"
	"time"
)

// repoCIWatchOverdueCheck raises the overdue alert when the watch is
// overdue and has not already alerted for this episode. The alert goes
// through the Coordinator's alert sink, outside c.mu.
func (c *Coordinator) repoCIWatchOverdueCheck() {
	if c == nil || c.m == nil {
		return
	}
	cfg := c.m.Config().Automation.RepoCIWatch
	repos := cfg.EffectiveRepos()
	now := c.now()

	c.mu.Lock()
	if cfg.EffectiveMode() == RepoCIWatchModeOff || len(repos) == 0 {
		c.repoCIWatchArmedAt = time.Time{}
		c.repoCIWatchOverdueAlerted = false
		c.mu.Unlock()
		return
	}
	if c.repoCIWatchArmedAt.IsZero() {
		c.repoCIWatchArmedAt = now
	}
	alertMinutes := cfg.EffectiveAlertMinutes()
	if alertMinutes <= 0 || c.repoCIWatchOverdueAlerted {
		c.mu.Unlock()
		return
	}
	since := c.repoCIWatchLastSuccess
	if since.IsZero() {
		since = c.repoCIWatchArmedAt
	}
	threshold := time.Duration(cfg.EffectivePollSeconds())*time.Second + time.Duration(alertMinutes)*time.Minute
	if now.Sub(since) < threshold {
		c.mu.Unlock()
		return
	}
	c.repoCIWatchOverdueAlerted = true
	last := c.repoCIWatchLastSuccess
	c.mu.Unlock()

	c.alert("warn", repoCIWatchOverdueTitle(), repoCIWatchOverdueBody(repos, last, since))
}

// markRepoCIWatchPollSucceeded records a successful poll pass at now and
// ends any overdue episode, so the next overdue episode alerts again.
func (c *Coordinator) markRepoCIWatchPollSucceeded(now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repoCIWatchLastSuccess = now
	c.repoCIWatchOverdueAlerted = false
}

// repoCIWatchOverdueTitle is the overdue alert's line.
func repoCIWatchOverdueTitle() string {
	return "CI watch: repository poll overdue"
}

// repoCIWatchOverdueBody names the watched repositories and the last
// successful poll (or the instant the watch was armed when none has
// succeeded yet, so the operator sees what the overdue is measured from).
func repoCIWatchOverdueBody(repos []string, lastSuccess, since time.Time) string {
	const layout = "2006-01-02 15:04:05 UTC"
	lastText := "never"
	if !lastSuccess.IsZero() {
		lastText = lastSuccess.UTC().Format(layout)
	}
	return fmt.Sprintf(
		"Automaton has not completed a CI poll of its watched repositories since %s (last successful poll: %s).\nRepositories: %s\nThe watch cannot see new CI runs until a poll succeeds again; check the repository paths, their origin remotes and GitHub access.",
		since.UTC().Format(layout), lastText, strings.Join(repos, ", "),
	)
}
