package config

import (
	"fmt"
	"strings"
)

// JudgeTimeoutDefaultSeconds is the default time limit for one judge run:
// the PRD guardrail, plan-review and other autonomous judge sessions. It is
// 30 minutes, used when autonomous.judge_timeout_seconds is unset or not
// positive. The limit counts the judge run only, not any wait for a capacity
// slot.
const JudgeTimeoutDefaultSeconds = 1800

// NormalizeJudgeTimeoutSeconds returns the effective judge time limit in
// seconds: the configured value when positive, otherwise
// JudgeTimeoutDefaultSeconds.
func NormalizeJudgeTimeoutSeconds(v int) int {
	if v <= 0 {
		return JudgeTimeoutDefaultSeconds
	}
	return v
}

// JudgeSizeThresholdDefaultLines is the default for
// autonomous.judge_size_threshold_lines: a change of more than this many
// changed lines routes the judges size-aware. It is applied by DefaultConfig
// when the key is absent; an explicit 0 turns the rule off and stays 0.
const JudgeSizeThresholdDefaultLines = 400

// JudgePromptFileThresholdDefaultLines is the default for
// autonomous.judge_prompt_file_threshold_lines: a judge prompt longer than
// this many lines is handed to the judge as a file instead of being sent
// inline. It is applied by DefaultConfig when the key is absent; an explicit
// 0 means the full prompt is always sent and stays 0.
const JudgePromptFileThresholdDefaultLines = 300

// JudgeSizeRuleEnabled reports whether the size-aware judge routing rule is
// on for the configured threshold: any positive value enables it, 0 turns it
// off.
func JudgeSizeRuleEnabled(threshold int) bool {
	return threshold > 0
}

// NormalizeJudgePolicy returns the judge backend list with blank and
// duplicate entries dropped and the order kept. An empty result means the
// judges follow the verifier routing policy.
func NormalizeJudgePolicy(in []string) []string {
	return NormalizeVerifierPolicyBackends(in)
}

// JudgeStrongBackend resolves the strong backend a large diff is routed to
// first: the configured value (autonomous.judge_strong_backend) when it is
// not blank, otherwise the last entry of the judge policy policy. Both empty
// resolves to "": there is no strong backend and size routing keeps the
// policy order.
func JudgeStrongBackend(policy []string, strong string) string {
	if s := strings.TrimSpace(strong); s != "" {
		return s
	}
	for i := len(policy) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(policy[i]); s != "" {
			return s
		}
	}
	return ""
}

// JudgeSizeStrongFirst is the judge-policy ordering for a measured diff size:
// when JudgeSizeRuleEnabled(threshold) is on and the changed-line count
// exceeds the threshold, the strong backend (see JudgeStrongBackend) moves
// to the front of the judge policy and the rest keep their relative order;
// below or at the threshold, and with the rule off, the policy order is
// unchanged. A strong backend that is not in the policy does not enter it:
// the order stays unchanged and the decision says why. It returns a fresh
// slice with each backend once (first occurrence wins) — policy is never
// mutated — and the decision sentence the decision log and the review state
// report, so the strong-first choice is made once per run and readable
// later (for example, to say "reroute").
func JudgeSizeStrongFirst(policy []string, lines, threshold int, strong string) (ordered []string, decision string) {
	ordered = make([]string, 0, len(policy))
	seen := make(map[string]struct{}, len(policy))
	for _, b := range policy {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if _, dup := seen[b]; dup {
			continue
		}
		seen[b] = struct{}{}
		ordered = append(ordered, b)
	}

	if threshold <= 0 {
		return ordered, "default order: size routing is off (threshold 0)"
	}
	if lines <= threshold {
		return ordered, fmt.Sprintf("default order: %d changed lines is at or below the threshold of %d", lines, threshold)
	}

	strong = JudgeStrongBackend(ordered, strong)
	if strong == "" {
		return ordered, fmt.Sprintf("default order: %d changed lines is above the threshold of %d but no strong judge backend is configured", lines, threshold)
	}
	if _, ok := seen[strong]; !ok {
		return ordered, fmt.Sprintf("default order: %d changed lines is above the threshold of %d but %s is not in the chain", lines, threshold, strong)
	}
	front := make([]string, 0, len(ordered))
	front = append(front, strong)
	for _, b := range ordered {
		if b != strong {
			front = append(front, b)
		}
	}
	return front, fmt.Sprintf("size rule: %d changed lines is above the threshold of %d, %s tried first", lines, threshold, strong)
}

// ForJudge returns the routing policy the judges use, derived from the verifier
// policy p at call time. A non-empty judge backend list replaces the verifier
// policy's backend list; every other field (mode, Claude session cap, stronger
// verifier) is the verifier policy's own. An empty or blank judge list returns
// p unchanged, so the judges follow the verifier policy and later edits to it
// flow through without any value being copied at save time.
func (p VerifierPolicyConfig) ForJudge(judgeBackends []string) VerifierPolicyConfig {
	list := NormalizeJudgePolicy(judgeBackends)
	if len(list) == 0 {
		return p
	}
	p.Backends = list
	return p
}

// JudgeVerdictCacheEnabledDefault is the effective value of
// autonomous.judge_verdict_cache_enabled when the key is not set: the
// per-commit judge verdict cache is ON by default, so a completed pass
// or block verdict for a (commit sha, judge policy, rules) triple is
// replayed from the cache instead of re-running the judge. Only an
// explicit false turns it off; a timed-out or otherwise unfinished judge
// is never cached regardless.
const JudgeVerdictCacheEnabledDefault = true

// NormalizeJudgeVerdictCacheEnabled resolves the
// autonomous.judge_verdict_cache_enabled flag. A nil (unset) value reads
// JudgeVerdictCacheEnabledDefault (true); a non-nil pointer reads the
// operator's stored value, so an explicit false round-trips as false and
// never silently reverts to the default.
func NormalizeJudgeVerdictCacheEnabled(enabled *bool) bool {
	if enabled == nil {
		return JudgeVerdictCacheEnabledDefault
	}
	return *enabled
}
