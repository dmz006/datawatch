// notify_events_test.go — tests for the notify_events list validation,
// default set and normalization in notify_events.go.

package autonomous

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestValidateNotifyEvents_Accepts(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
	}{
		{"nil list", nil},
		{"empty list", []string{}},
		{"default set", DefaultNotifyEvents()},
		{"opt-in session kinds", []string{KindTaskSessionStarted, KindTaskSessionCleared}},
		{"task status kinds", []string{TaskStatusKind(TaskCompleted), TaskStatusKind(TaskFailed)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateNotifyEvents(tc.kinds); err != nil {
				t.Fatalf("ValidateNotifyEvents(%v) = %v; want nil", tc.kinds, err)
			}
		})
	}
}

func TestValidateNotifyEvents_RejectsEmptyEntry(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
		index int
	}{
		{"first entry empty", []string{""}, 0},
		{"second entry whitespace only", []string{KindTaskSessionStarted, "   "}, 1},
		{"second entry mixed whitespace", []string{KindTaskSessionStarted, " \t\n "}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNotifyEvents(tc.kinds)
			if err == nil {
				t.Fatalf("ValidateNotifyEvents(%v) = nil; want error", tc.kinds)
			}
			want := fmt.Sprintf("notify event kind %d is empty", tc.index)
			if err.Error() != want {
				t.Fatalf("error = %q; want %q", err.Error(), want)
			}
		})
	}
}

func TestValidateNotifyEvents_RejectsUnknownListsValidKinds(t *testing.T) {
	cases := []struct {
		name  string
		kinds []string
	}{
		{"prd.updated is excluded", []string{KindPRDUpdated}},
		{"random string", []string{"bogus.kind"}},
		{"automation kind is not a known kind", []string{AutomationStatusKind(AutomationCompleted)}},
		{"unknown after a known kind reports the first problem",
			[]string{KindTaskSessionStarted, "no.such.kind"}},
	}
	validList := strings.Join(ValidNotifyEventKinds(), ", ")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNotifyEvents(tc.kinds)
			if err == nil {
				t.Fatalf("ValidateNotifyEvents(%v) = nil; want error", tc.kinds)
			}
			var bad string
			for _, k := range tc.kinds {
				if strings.TrimSpace(k) == "" {
					continue
				}
				inValid := false
				for _, v := range ValidNotifyEventKinds() {
					if v == k {
						inValid = true
						break
					}
				}
				if !inValid {
					bad = k
					break
				}
			}
			want := fmt.Sprintf("unknown notify event kind %q; valid kinds: %s", bad, validList)
			if err.Error() != want {
				t.Fatalf("error = %q;\nwant    %q", err.Error(), want)
			}
		})
	}
}

func TestValidNotifyEventKinds(t *testing.T) {
	got := ValidNotifyEventKinds()

	if !sort.StringsAreSorted(got) {
		t.Errorf("kinds are not sorted: %v", got)
	}
	for _, k := range got {
		if k == KindPRDUpdated {
			t.Errorf("kinds must not include %q (internal refresh signal)", KindPRDUpdated)
		}
	}
	if len(got) != len(KnownEventKinds)-1 {
		t.Errorf("len(kinds) = %d; want %d (all known kinds minus prd.updated)",
			len(got), len(KnownEventKinds)-1)
	}

	contains := func(k string) bool {
		for _, g := range got {
			if g == k {
				return true
			}
		}
		return false
	}
	for _, k := range DefaultNotifyEvents() {
		if !contains(k) {
			t.Errorf("kinds are missing default kind %q", k)
		}
	}
	for _, k := range []string{KindTaskSessionStarted, KindTaskSessionCleared} {
		if !contains(k) {
			t.Errorf("kinds are missing opt-in session kind %q", k)
		}
	}
	if contains(KindPRDUpdated) {
		t.Errorf("kinds must not include %q", KindPRDUpdated)
	}
}

func TestDefaultNotifyEvents(t *testing.T) {
	want := []string{
		PRDStatusKind(PRDNeedsReview),
		PRDStatusKind(PRDBlocked),
		PRDStatusKind(PRDFailed),
		PRDStatusKind(PRDCompleted),
		PRDStatusKind(PRDCancelled),
	}
	got := DefaultNotifyEvents()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultNotifyEvents() = %v; want exactly %v in this order", got, want)
	}
	for _, k := range got {
		if k == KindTaskSessionStarted || k == KindTaskSessionCleared {
			t.Errorf("session kinds must be opt-in only, not in the default set: %q", k)
		}
	}

	// Each call returns a fresh copy.
	a := DefaultNotifyEvents()
	b := DefaultNotifyEvents()
	a[0] = "corrupted"
	if b[0] == "corrupted" || reflect.DeepEqual(a, b) {
		t.Fatalf("DefaultNotifyEvents() must return a fresh slice each call: a=%v b=%v", a, b)
	}
}

func TestNormalizeNotifyEvents(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil stays nil", nil, nil},
		{"empty stays empty", []string{}, nil},
		{"only blanks drop to nil", []string{"", "   "}, nil},
		{"trims entries", []string{"  " + KindTaskSessionStarted, KindTaskSessionCleared + " "},
			[]string{KindTaskSessionStarted, KindTaskSessionCleared}},
		{"drops duplicates keeping first-seen order",
			[]string{KindTaskSessionCleared, KindTaskSessionStarted, KindTaskSessionCleared},
			[]string{KindTaskSessionCleared, KindTaskSessionStarted}},
		{"duplicates via padding collapse",
			[]string{KindTaskSessionStarted, "  " + KindTaskSessionStarted + " "},
			[]string{KindTaskSessionStarted}},
		{"default set passes through unchanged", DefaultNotifyEvents(), DefaultNotifyEvents()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeNotifyEvents(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("NormalizeNotifyEvents(%v) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
