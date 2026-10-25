package config

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

// NormalizeJudgePolicy returns the judge backend list with blank and
// duplicate entries dropped and the order kept. An empty result means the
// judges run on each unit's guardrail backend.
func NormalizeJudgePolicy(in []string) []string {
	return NormalizeVerifierPolicyBackends(in)
}
