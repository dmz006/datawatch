// BL410 Phase 0 — autonomousVerify and autonomousGuardrail live inside
// main() and are not unit-testable directly (same constraint
// bl369_prompt_security_test.go / session_judgment_wiring_test.go
// document); source inspection confirms each infra-vs-content
// collapse point is stamped with the right Category, including the
// "default pessimistic, clear on successful parse" discipline the
// unparseable-response branches need (a successfully-parsed real
// judgment must not be left looking like an infra failure just
// because the LLM's own JSON never mentions "category").
package main

import "testing"

func TestBL410_GuardrailSessionError_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:   "guardrail(session): " + serr.Error(),
							VerdictAt: time.Now(),
							Category:  "infra",`) {
		t.Error("autonomousGuardrail: session-error branch must stamp Category:\"infra\"")
	}
}

func TestBL410_GuardrailSessionUnparseable_DefaultsInfraClearsOnParse(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Category:  "infra", // default pessimistic; cleared below on a successful parse
					}
					if uerr := json.Unmarshal([]byte(extractJSON(content)), &gv); uerr == nil {
						gv.Category = "" // a real judgment parsed — content, not infra
					}`) {
		t.Error("autonomousGuardrail: session-unparseable branch must default Category to \"infra\" and clear it only on a successful parse")
	}
}

func TestBL410_GuardrailAskCallFailed_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:   "guardrail: ask call failed: " + reason,
					VerdictAt: time.Now(),
					Category:  "infra",`) {
		t.Error("autonomousGuardrail: ask-call-failed branch must stamp Category:\"infra\"")
	}
}

func TestBL410_GuardrailAskUnparseable_DefaultsInfraClearsOnParse(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Category:  "infra", // default pessimistic; cleared below on a successful parse
			}
			if uerr := json.Unmarshal([]byte(extractJSON(ask.Answer)), &gv); uerr == nil {
				gv.Category = "" // a real judgment parsed — content, not infra
			}`) {
		t.Error("autonomousGuardrail: ask-unparseable branch must default Category to \"infra\" and clear it only on a successful parse")
	}
}

func TestBL410_VerifierGitDiffFailed_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:    fmt.Sprintf("verifier: git diff failed: %v", diffErr),
						VerifiedAt: time.Now(),
						Category:   "infra",`) {
		t.Error("autonomousVerify: git-diff-failed branch must stamp Category:\"infra\"")
	}
}

func TestBL410_VerifierCapacityFailed_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:    "verifier: capacity: " + aerr.Error(),
							VerifiedAt: time.Now(),
							Category:   "infra",`) {
		t.Error("autonomousVerify: capacity-admission-failed branch must stamp Category:\"infra\"")
	}
}

func TestBL410_VerifierSessionError_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:    "verifier(session): " + serr.Error(),
						VerifiedAt: time.Now(),
						Category:   "infra",`) {
		t.Error("autonomousVerify: session-error branch must stamp Category:\"infra\"")
	}
}

func TestBL410_VerifierSessionUnparseable_DefaultsInfraClearsOnParse(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Category:   "infra", // default pessimistic; cleared below on a successful parse
				}
				if uerr := json.Unmarshal([]byte(extractJSON(content)), &vr); uerr == nil {
					vr.Category = "" // a real judgment parsed — content, not infra
				}`) {
		t.Error("autonomousVerify: session-unparseable branch must default Category to \"infra\" and clear it only on a successful parse")
	}
}

func TestBL410_VerifierAskCallFailed_StampedInfra(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Summary:    "verifier: ask call failed: " + reason,
					VerifiedAt: time.Now(),
					Category:   "infra",`) {
		t.Error("autonomousVerify: ask-call-failed branch must stamp Category:\"infra\"")
	}
}

func TestBL410_VerifierAskUnparseable_DefaultsInfraClearsOnParse(t *testing.T) {
	src := readMainGo(t)
	if !contains(src, `Category:   "infra", // default pessimistic; cleared below on a successful parse
			}
			if uerr := json.Unmarshal([]byte(extractJSON(ask.Answer)), &vr); uerr == nil {
				vr.Category = "" // a real judgment parsed — content, not infra
			}`) {
		t.Error("autonomousVerify: ask-unparseable branch must default Category to \"infra\" and clear it only on a successful parse")
	}
}
