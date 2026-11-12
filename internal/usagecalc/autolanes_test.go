package usagecalc

import (
	"fmt"
	"strings"
	"testing"
)

func TestAutoLanesClampsAtOne(t *testing.T) {
	// A reading of zero lanes, with no special items, still targets one lane.
	d := DecideAutoLanes(0, 0, 8, 4)
	if d.Target != 1 {
		t.Errorf("target = %d, want 1", d.Target)
	}
	// Negative inputs settle at one lane as well; the target is never zero.
	if d = DecideAutoLanes(0, -3, 8, 0); d.Target != 1 {
		t.Errorf("negative reading target = %d, want 1", d.Target)
	}
	// A smaller current cap is reported as a raise, never an equal cap of zero.
	if d.Direction != AutoLanesUp {
		t.Errorf("direction = %q, want %q", d.Direction, AutoLanesUp)
	}
}

func TestAutoLanesClampsAtOperatorMaximum(t *testing.T) {
	d := DecideAutoLanes(5, 9, 6, 2)
	if d.Target != 6 {
		t.Errorf("target = %d, want the operator maximum 6", d.Target)
	}
	if d.Direction != AutoLanesUp {
		t.Errorf("direction = %q, want %q", d.Direction, AutoLanesUp)
	}
	// The target never sits above the operator maximum, whatever the reading says.
	if d = DecideAutoLanes(99, 99, 4, 4); d.Target != 4 {
		t.Errorf("huge reading target = %d, want 4", d.Target)
	}
}

func TestAutoLanesSpecialItemsRaiseTargetByOne(t *testing.T) {
	const (
		suggested   = 2
		operatorMax = 10
	)
	prev := 0
	for n := 0; n < 4; n++ {
		d := DecideAutoLanes(suggested, n, operatorMax, 100)
		want := suggested + n
		if d.Target != want {
			t.Fatalf("special items %d: target = %d, want %d", n, d.Target, want)
		}
		if prev != 0 && d.Target != prev+1 {
			t.Errorf("special items %d: target = %d, previous was %d and each item raises it by one", n, d.Target, prev)
		}
		if d.Direction != AutoLanesDown {
			t.Errorf("special items %d: direction = %q, want %q against cap 100", n, d.Direction, AutoLanesDown)
		}
		prev = d.Target
	}
}

func TestAutoLanesReportsUnchanged(t *testing.T) {
	d := DecideAutoLanes(3, 1, 8, 4)
	if d.Target != 4 {
		t.Fatalf("target = %d, want 4", d.Target)
	}
	if d.Direction != AutoLanesUnchanged {
		t.Errorf("direction = %q, want %q when the target equals the current cap", d.Direction, AutoLanesUnchanged)
	}
	// Both movement directions, against a reading that fits the operator maximum.
	if up := DecideAutoLanes(5, 2, 10, 4); up.Direction != AutoLanesUp {
		t.Errorf("direction = %q, want %q", up.Direction, AutoLanesUp)
	}
	if down := DecideAutoLanes(5, 2, 10, 9); down.Direction != AutoLanesDown {
		t.Errorf("direction = %q, want %q", down.Direction, AutoLanesDown)
	}
}

func TestAutoLanesReasonNamesTheReading(t *testing.T) {
	const (
		suggested   = 7
		special     = 2
		operatorMax = 20
	)
	d := DecideAutoLanes(suggested, special, operatorMax, 3)
	wantReading := fmt.Sprintf("%d lane(s) for the active policy", suggested)
	if !strings.Contains(d.Reason, wantReading) {
		t.Errorf("reason does not name the reading: %q", d.Reason)
	}
	if want := fmt.Sprintf("%d open special item(s)", special); !strings.Contains(d.Reason, want) {
		t.Errorf("reason does not name the special items: %q", d.Reason)
	}
	if !strings.Contains(d.Reason, fmt.Sprintf("operator maximum of %d", operatorMax)) {
		t.Errorf("reason does not name the operator maximum: %q", d.Reason)
	}
}

func TestAutoLanesTargetNeverZero(t *testing.T) {
	for _, max := range []int{0, -4, 100} {
		for _, special := range []int{0, -6, 3} {
			d := DecideAutoLanes(0, special, max, 9)
			if d.Target < 1 {
				t.Errorf("maximum %d, items %d: target = %d, want at least 1", max, special, d.Target)
			}
		}
	}
}
