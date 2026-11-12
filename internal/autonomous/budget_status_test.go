package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAutonomousStatusIncludesBudgets(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), nil)

	// Unwired: the field is present and is an empty array, never null.
	raw, err := json.Marshal(m.Status())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"budgets":[]`) {
		t.Fatalf("unwired status must carry an empty budgets array: %s", raw)
	}

	// A wired function returning nil still marshals as an array.
	m.SetBudgetsStatus(func() []BudgetStatus { return nil })
	raw, _ = json.Marshal(m.Status())
	if !strings.Contains(string(raw), `"budgets":[]`) {
		t.Fatalf("nil budgets must marshal as an array: %s", raw)
	}

	end := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	m.SetBudgetsStatus(func() []BudgetStatus {
		return []BudgetStatus{{LLM: "claude", CeilingUSD: 10, SpendUSD: 4, RemainingUSD: 6, Window: "rolling", WindowHours: 5, WindowEnd: end}}
	})
	st := m.Status()
	if len(st.Budgets) != 1 || st.Budgets[0].LLM != "claude" || st.Budgets[0].SpendUSD != 4 {
		t.Fatalf("budgets = %+v", st.Budgets)
	}
	raw, _ = json.Marshal(st)
	var body struct {
		Budgets []struct {
			LLM        string  `json:"llm"`
			CeilingUSD float64 `json:"ceiling_usd"`
		} `json:"budgets"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Budgets) != 1 || body.Budgets[0].LLM != "claude" || body.Budgets[0].CeilingUSD != 10 {
		t.Fatalf("marshalled budgets = %s", raw)
	}

	// Clearing the setter returns to the empty array.
	m.SetBudgetsStatus(nil)
	if got := m.Status().Budgets; got == nil || len(got) != 0 {
		t.Fatalf("cleared budgets = %#v", got)
	}
}
