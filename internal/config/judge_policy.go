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
// judges follow the verifier routing policy.
func NormalizeJudgePolicy(in []string) []string {
	return NormalizeVerifierPolicyBackends(in)
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
