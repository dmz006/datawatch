package autonomous

import (
	"reflect"
	"testing"
)

func TestOverlapMatcherNormalisation(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "lower-casing removes case distinctions",
			text: "FastRed BROWN car",
			want: []string{"brown", "car", "fastred"},
		},
		{
			name: "punctuation splits tokens",
			text: "hello, world! 42-miles (long)",
			want: []string{"hello", "long", "miles", "world"},
		},
		{
			name: "stopwords are dropped",
			text: "the report of the annual and or review",
			want: []string{"annual", "report", "review"},
		},
		{
			name: "short tokens are dropped",
			text: "go hi a 3 ab abc",
			want: []string{"abc"},
		},
		{
			name: "duplicates collapse to one distinct term",
			text: "car car CAR cars",
			want: []string{"car", "cars"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeOverlapTerms(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d terms %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for _, term := range tc.want {
				if _, ok := got[term]; !ok {
					t.Errorf("missing %q in %v", term, got)
				}
			}
		})
	}
}

func TestOverlapMatcherScoreArithmetic(t *testing.T) {
	result := MatchTermOverlap(0,
		"alpha beta gamma delta",
		"alpha beta gamma epsilon")
	if result.Score != 3.0/4.0 {
		t.Fatalf("score = %v, want %v", result.Score, 3.0/4.0)
	}
	if !result.Matched {
		t.Fatal("expected a match at zero threshold with three shared terms")
	}

	// The denominator is the smaller set.
	result = MatchTermOverlap(0,
		"alpha beta gamma",
		"alpha beta gamma delta epsilon")
	if result.Score != 1.0 {
		t.Fatalf("score = %v, want 1", result.Score)
	}

	// One shared term out of two in the smaller set.
	result = MatchTermOverlap(0, "copper zinc", "copper lead")
	if result.Score != 0.5 {
		t.Fatalf("score = %v, want 0.5", result.Score)
	}
}

func TestOverlapMatcherThresholdBoundary(t *testing.T) {
	// Six distinct terms on each side, three shared: score is exactly 0.5.
	const left = "alpha beta gamma theta phi chi"
	const right = "alpha beta gamma sigma omicron rho"

	atThreshold := MatchTermOverlap(0.5, left, right)
	if atThreshold.Score != 0.5 {
		t.Fatalf("score = %v, want 0.5", atThreshold.Score)
	}
	if !atThreshold.Matched {
		t.Fatal("score exactly at the threshold should match")
	}

	justBelow := MatchTermOverlap(0.51, left, right)
	if justBelow.Matched {
		t.Fatal("score just below the threshold should not match")
	}
}

func TestOverlapMatcherTwoSharedNeverMatch(t *testing.T) {
	result := MatchTermOverlap(0, "copper zinc", "copper zinc")
	if result.Score != 1.0 {
		t.Fatalf("score = %v, want 1", result.Score)
	}
	if result.Matched {
		t.Fatal("two shared terms must never match even at score 1")
	}
}

func TestOverlapMatcherEmptyInput(t *testing.T) {
	cases := []struct {
		name  string
		left  string
		right string
	}{
		{name: "both empty", left: "", right: ""},
		{name: "left empty", left: "", right: "alpha beta gamma"},
		{name: "right empty", left: "alpha beta gamma", right: ""},
		{name: "only stopwords on one side", left: "the and of", right: "alpha beta gamma"},
		{name: "only short tokens on one side", left: "go hi a", right: "alpha beta gamma"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := MatchTermOverlap(0, tc.left, tc.right)
			if result.Matched {
				t.Fatal("empty-input pair must never match")
			}
			if result.Score != 0 {
				t.Fatalf("score = %v, want 0", result.Score)
			}
			if result.Shared != nil {
				t.Fatalf("shared = %v, want nil", result.Shared)
			}
		})
	}
}

func TestOverlapMatcherSharedSortedAndTruncated(t *testing.T) {
	result := MatchTermOverlap(0,
		"golf delta yank zeta alpha bravo echo charlie foxtrot",
		"yankee golf delta yankee alpha bravo echo charlie")

	want := []string{"alpha", "bravo", "charlie", "delta", "echo"}
	if !reflect.DeepEqual(result.Shared, want) {
		t.Fatalf("shared = %v, want %v (sorted, truncated to five)", result.Shared, want)
	}

	// Fewer than five shared terms are returned whole and sorted.
	result = MatchTermOverlap(0, "zeta alpha charlie", "alpha zeta charlie omega")
	want = []string{"alpha", "charlie", "zeta"}
	if !reflect.DeepEqual(result.Shared, want) {
		t.Fatalf("shared = %v, want %v", result.Shared, want)
	}
}
