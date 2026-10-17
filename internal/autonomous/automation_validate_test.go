package autonomous

import (
	"strings"
	"testing"
)

// validateMember builds one member of a test Automation; each outcome edge
// is a member ID, or "" for no edge.
func validateMember(id, prdID, onComplete, onBlocked, onFailed string) AutomationMember {
	return AutomationMember{
		ID:         id,
		PRDID:      prdID,
		OnComplete: onComplete,
		OnBlocked:  onBlocked,
		OnFailed:   onFailed,
	}
}

// newValidationAutomation assembles a named Automation for a single
// validation case.
func newValidationAutomation(entry string, status AutomationStatus, members ...AutomationMember) *Automation {
	return &Automation{
		ID:            "av1",
		Name:          "validation subject",
		Status:        status,
		EntryMemberID: entry,
		Members:       members,
	}
}

// automationValidateCase is one row of the validateAutomation table: build
// the Automation, expect an error containing errSubstring ("" means it must
// validate cleanly), and optionally expect a specific unreachable-member set.
type automationValidateCase struct {
	name            string
	build           func() *Automation
	errSubstring    string
	wantUnreachable []string
}

func TestAutomationValidate(t *testing.T) {
	cases := []automationValidateCase{
		{
			name: "valid linear chain",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "c2", "", ""),
					validateMember("c2", "pc2", "c3", "", ""),
					validateMember("c3", "pc3", "", "", ""),
				)
			},
		},
		{
			name: "valid branch rejoins chain",
			build: func() *Automation {
				// c1 either completes to c2 or is blocked to the fix-up
				// member c3, which then joins the chain at c2.
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "c2", "c3", ""),
					validateMember("c2", "pc2", "", "", ""),
					validateMember("c3", "pc3", "c2", "", ""),
				)
			},
		},
		{
			name: "cycle via on_failed",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "c2", "", ""),
					validateMember("c2", "pc2", "c3", "", ""),
					validateMember("c3", "pc3", "", "", "c1"),
				)
			},
			errSubstring: "cycle",
		},
		{
			name: "self edge",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "", "c1", ""),
				)
			},
			errSubstring: "points to itself",
		},
		{
			name: "unreachable member rejected when active",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "c2", "", ""),
					validateMember("c2", "pc2", "", "", ""),
					validateMember("c9", "pc9", "", "", ""),
				)
			},
			errSubstring: "unreachable",
		},
		{
			name: "unreachable member tolerated in draft",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationDraft,
					validateMember("c1", "pc1", "c2", "", ""),
					validateMember("c2", "pc2", "", "", ""),
					validateMember("c9", "pc9", "", "", ""),
				)
			},
			wantUnreachable: []string{"c9"},
		},
		{
			name: "missing entry member",
			build: func() *Automation {
				a := newValidationAutomation("", AutomationActive,
					validateMember("c1", "pc1", "", "", ""),
				)
				return a
			},
			errSubstring: "entry member",
		},
		{
			name: "edge names unknown member",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "ghost", "", ""),
				)
			},
			errSubstring: "unknown member",
		},
		{
			name: "duplicate member id",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "", "", ""),
					validateMember("c1", "pc2", "", "", ""),
				)
			},
			errSubstring: "duplicate member ID",
		},
		{
			name: "duplicate prd id",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "", "", ""),
					validateMember("c2", "pc1", "", "", ""),
				)
			},
			errSubstring: "member more than once",
		},
		{
			name: "blank name",
			build: func() *Automation {
				a := newValidationAutomation("c1", AutomationActive,
					validateMember("c1", "pc1", "", "", ""),
				)
				a.Name = "   "
				return a
			},
			errSubstring: "blank name",
		},
		{
			name: "invalid status",
			build: func() *Automation {
				return newValidationAutomation("c1", AutomationStatus("zombie"),
					validateMember("c1", "pc1", "", "", ""),
				)
			},
			errSubstring: "unknown status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.build()
			err := validateAutomation(a)
			if tc.errSubstring == "" {
				if err != nil {
					t.Fatalf("valid Automation rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.errSubstring) {
				t.Fatalf("expected error containing %q, got %v", tc.errSubstring, err)
			}
			if tc.wantUnreachable != nil {
				got := automationUnreachable(a)
				if len(got) != len(tc.wantUnreachable) {
					t.Fatalf("automationUnreachable = %v, want %v", got, tc.wantUnreachable)
				}
				for i := range got {
					if got[i] != tc.wantUnreachable[i] {
						t.Fatalf("automationUnreachable = %v, want %v", got, tc.wantUnreachable)
					}
				}
			}
		})
	}
}

func TestAutomationValidateBudgetPolicy(t *testing.T) {
	good := &BudgetPolicy{CeilingType: "dollar", CeilingValue: 10, Window: "automation", Pacing: "none", OnBreach: "pause"}
	with := func(mutate func(*BudgetPolicy)) *BudgetPolicy {
		p := *good
		mutate(&p)
		return &p
	}
	cases := []struct {
		name         string
		policy       *BudgetPolicy
		errSubstring string
	}{
		{"valid policy", good, ""},
		{"bad ceiling type", with(func(p *BudgetPolicy) { p.CeilingType = "cents" }), "ceiling type"},
		{"non-positive value", with(func(p *BudgetPolicy) { p.CeilingValue = 0 }), "positive"},
		{"bad window", with(func(p *BudgetPolicy) { p.Window = "year" }), "window"},
		{"bad on_breach", with(func(p *BudgetPolicy) { p.OnBreach = "alert" }), "on_breach"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newValidationAutomation("c1", AutomationActive,
				validateMember("c1", "pc1", "", "", ""))
			a.BudgetPolicy = tc.policy
			err := validateAutomation(a)
			if tc.errSubstring == "" {
				if err != nil {
					t.Fatalf("valid policy rejected: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.errSubstring) {
				t.Fatalf("expected error containing %q, got %v", tc.errSubstring, err)
			}
		})
	}
}
