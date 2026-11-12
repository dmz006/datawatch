package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// PolicyRole names who a routing-policy pick is for. The verifier and the
// judges share one routing policy and one selector; the role tells their picks
// apart in the decision log.
type PolicyRole string

const (
	PolicyRoleVerifier PolicyRole = "verifier"
	PolicyRoleJudge    PolicyRole = "judge"
)

// PolicySelection is one routing-policy pick together with the role it was for.
type PolicySelection struct {
	Role    PolicyRole
	Backend string
	Model   string
	Reason  string
}

// ResolvePolicyForRole returns the routing policy in force for a role. The
// verifier role gets verifier unchanged. The judge role gets verifier with its
// backend list replaced by judgeBackends when that list is non-empty; an unset
// or empty judge list resolves to the verifier policy at the moment of the call.
func ResolvePolicyForRole(role PolicyRole, verifier config.VerifierPolicyConfig, judgeBackends []string) config.VerifierPolicyConfig {
	if role == PolicyRoleJudge {
		return verifier.ForJudge(judgeBackends)
	}
	return verifier
}

// SelectBackendForRole picks a backend for the given role. It resolves the
// role's policy (see ResolvePolicyForRole) and runs the shared selector, so the
// ordered backend list and the fixed, prefer-free and balance modes behave the
// same for both roles. judgeBackends is read only for the judge role. The
// returned selection carries the role for the decision log.
func SelectBackendForRole(role PolicyRole, verifier config.VerifierPolicyConfig, judgeBackends, candidates []string, load VerifierLoad, flags VerifierSelectFlags) PolicySelection {
	policy := ResolvePolicyForRole(role, verifier, judgeBackends)
	backend, model, reason := SelectVerifierBackend(policy, candidates, load, flags)
	if role == PolicyRoleJudge && backend == "" {
		reason = strings.Replace(reason, "no eligible verifier backend", "no eligible judge backend", 1)
	}
	return PolicySelection{Role: role, Backend: backend, Model: model, Reason: reason}
}
