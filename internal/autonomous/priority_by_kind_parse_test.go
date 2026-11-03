package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePriorityByKindValue_AcceptedShapes(t *testing.T) {
	type tc struct {
		name string
		in   any
		want map[string]int
	}
	for _, tc := range []tc{
		{"object", map[string]any{"bug": 90, "epic": 50}, map[string]int{"bug": 90, "epic": 50}},
		{"object via any", map[string]any{"bug": 90}, map[string]int{"bug": 90}},
		{"JSON object string", `{"bug":90,"feature":25}`, map[string]int{"bug": 90, "feature": 25}},
		{"pair list", "bug=90,feature=25", map[string]int{"bug": 90, "feature": 25}},
		{"pair list with spaces", " bug = 90 , epic=50 ", map[string]int{"bug": 90, "epic": 50}},
		{"custom kind", "epic=50", map[string]int{"epic": 50}},
		{"clear: empty object", map[string]any{}, map[string]int{}},
		{"clear: empty string", "  ", map[string]int{}},
		{"clear: nil", nil, map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePriorityByKindValue(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("table = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParsePriorityByKindValue_Refused(t *testing.T) {
	type tc struct {
		name string
		in   any
		frag string
	}
	for _, tc := range []tc{
		{"word value", `{"bug":"high"}`, "not a number"},
		{"fraction", `{"bug":1.5}`, "must be an integer"},
		{"negative", `{"bug":-3}`, ">= 0"},
		{"empty kind (pair)", "=90", "expected an object"},
		{"empty kind (object)", map[string]any{"  ": 5}, "empty kind name"},
		{"array", []int{1, 2}, "expected an object"},
		{"integer", 90, "expected an object"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePriorityByKindValue(tc.in)
			if err == nil {
				t.Fatalf("expected a refusal")
			}
			if !strings.Contains(err.Error(), tc.frag) {
				t.Fatalf("error %q should say %q", err, tc.frag)
			}
		})
	}
}

func TestWaiterPrioritySuffix_NamesSource(t *testing.T) {
	if got := waiterPrioritySuffix(90, PrioritySourceExplicit); got != " (priority 90, explicit)" {
		t.Fatalf("explicit = %q", got)
	}
	if got := waiterPrioritySuffix(80, PrioritySourceKind); got != " (priority 80, kind)" {
		t.Fatalf("kind = %q", got)
	}
	if got := waiterPrioritySuffix(0, PrioritySourceDefault); got != " (priority 0, default)" {
		t.Fatalf("default = %q", got)
	}
	if got := waiterPrioritySuffix(60, ""); got != " (priority 60, default)" {
		t.Fatalf("number without source = %q", got)
	}
	if got := waiterPrioritySuffix(0, ""); got != "" {
		t.Fatalf("no priority at all should print nothing, got %q", got)
	}
}
