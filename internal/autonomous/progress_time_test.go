package autonomous

import (
	"math"
	"strings"
	"testing"
	"time"
)

func ptAt(t *testing.T, s string) time.Time {
	t.Helper()
	v, ok := ParseProgressTime(s)
	if !ok {
		t.Fatalf("parse %q failed", s)
	}
	return v
}

func ptNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestProgressTimeParseForms(t *testing.T) {
	base := time.Date(2026, 10, 10, 12, 30, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"2026-10-10T12:30:00Z":           base,
		"2026-10-10T12:30:00+00:00":      base,
		"2026-10-10T08:30:00-04:00":      base,
		"2026-10-10T14:30:00+02:00":      base,
		"2026-10-10T12:30:00":            base,
		"2026-10-10T12:30:00.250Z":       base.Add(250 * time.Millisecond),
		"2026-10-10T12:30:00.123456":     base.Add(123456 * time.Microsecond),
		"2026-10-10T08:30:00.5-04:00":    base.Add(500 * time.Millisecond),
		"2026-10-10T12:30:00.123456789Z": base.Add(123456789 * time.Nanosecond),
	}
	for in, want := range cases {
		got, ok := ParseProgressTime(in)
		if !ok || !got.Equal(want) {
			t.Errorf("ParseProgressTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "   ", "not a time", "2026-13-40T00:00:00Z", "yesterday"} {
		if _, ok := ParseProgressTime(bad); ok {
			t.Errorf("ParseProgressTime(%q) accepted", bad)
		}
	}
}

func TestProgressTimeHours(t *testing.T) {
	a := ptAt(t, "2026-10-10T00:00:00Z")
	ptNear(t, "Hours", Hours(a, a.Add(90*time.Minute)), 1.5)
	ptNear(t, "Hours back", Hours(a.Add(time.Hour), a), -1)
}

func TestProgressTimeInferGapBoundary(t *testing.T) {
	t0 := ptAt(t, "2026-10-10T00:00:00Z")
	gap := 3 * time.Hour
	evs := []time.Time{t0, t0.Add(gap), t0.Add(gap + gap - time.Second)}
	ws := InferInactiveWindows(evs, t0, gap)
	if len(ws) != 1 {
		t.Fatalf("got %d windows, want 1 (exactly-gap counts, just under does not): %+v", len(ws), ws)
	}
	w := ws[0]
	if !w.Start.Equal(t0) || !w.End.Equal(t0.Add(gap)) || w.Kind != "inactive" || !w.Inferred {
		t.Fatalf("unexpected window %+v", w)
	}
}

func TestProgressTimeInferSortsAndIgnoresBeforeSince(t *testing.T) {
	since := ptAt(t, "2026-10-10T00:00:00Z")
	evs := []time.Time{
		since.Add(10 * time.Hour),
		since.Add(-20 * time.Hour), // before since: ignored, would otherwise open a window
		since.Add(time.Hour),
		since, // exactly since is kept
	}
	ws := InferInactiveWindows(evs, since, 3*time.Hour)
	if len(ws) != 1 {
		t.Fatalf("got %d windows, want 1: %+v", len(ws), ws)
	}
	if !ws[0].Start.Equal(since.Add(time.Hour)) || !ws[0].End.Equal(since.Add(10*time.Hour)) {
		t.Fatalf("unexpected window %+v", ws[0])
	}
	if got := InferInactiveWindows(nil, since, time.Hour); len(got) != 0 {
		t.Fatalf("nil events gave %v", got)
	}
}

func TestProgressTimeParseTSV(t *testing.T) {
	in := strings.Join([]string{
		"# comment\tline\tfoo",
		"",
		"2026-10-11T00:00:00Z\t2026-10-11T06:00:00Z\tdesign\toperator thinking",
		"2026-10-12T00:00:00Z\t2026-10-12T02:00:00Z\tbuild",
		"2026-10-13T00:00:00Z\t2026-10-13T02:00:00Z",
		"bad\t2026-10-13T02:00:00Z\tdesign\tx",
		"2026-10-14T00:00:00Z\tbad\tdesign\tx",
		"2026-10-15T05:00:00Z\t2026-10-15T05:00:00Z\tdesign\tzero length",
		"2026-10-16T05:00:00Z\t2026-10-16T01:00:00Z\tdesign\tnegative",
		"2026-10-17T00:00:00+00:00\t2026-10-17T01:00:00+00:00\tbuild\twin\r",
	}, "\n")
	ws, err := ParseWindowsTSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 {
		t.Fatalf("got %d windows, want 3: %+v", len(ws), ws)
	}
	if ws[0].Kind != "design" || ws[0].Reason != "operator thinking" || ws[0].Inferred {
		t.Fatalf("first window %+v", ws[0])
	}
	if ws[1].Kind != "build" || ws[1].Reason != "" {
		t.Fatalf("second window %+v", ws[1])
	}
	if ws[2].Reason != "win" {
		t.Fatalf("third window %+v", ws[2])
	}
}

func TestProgressTimeMergeOverlapExplicitAndInferred(t *testing.T) {
	d := ptAt(t, "2026-10-10T00:00:00Z")
	h := func(n float64) time.Time { return d.Add(time.Duration(n * float64(time.Hour))) }
	ws := []InactiveWindow{
		{Start: h(10), End: h(20), Kind: "design"},
		{Start: h(2), End: h(8), Kind: "inactive", Inferred: true},
		{Start: h(5), End: h(12), Kind: "inactive", Inferred: true}, // overlaps both
		{Start: h(30), End: h(31), Kind: "build"},
		{Start: h(31), End: h(33), Kind: "build"}, // touches the previous one
		{Start: h(40), End: h(41), Kind: "build"},
	}
	m := MergeWindows(ws)
	if len(m) != 3 {
		t.Fatalf("got %d merged windows, want 3: %+v", len(m), m)
	}
	if !m[0].Start.Equal(h(2)) || !m[0].End.Equal(h(20)) || m[0].Kind != "inactive" || !m[0].Inferred {
		t.Fatalf("first merged window %+v", m[0])
	}
	if !m[1].Start.Equal(h(30)) || !m[1].End.Equal(h(33)) {
		t.Fatalf("second merged window %+v", m[1])
	}
	if ws[0].Start != h(10) {
		t.Fatalf("input was reordered")
	}
	ptNear(t, "ExcludedHours", ExcludedHours(ws), 18+3+1)
	if got := MergeWindows(nil); len(got) != 0 {
		t.Fatalf("nil merge gave %v", got)
	}
	ptNear(t, "ExcludedHours empty", ExcludedHours(nil), 0)
}

func TestProgressTimeActiveHoursCountsOverlapOnce(t *testing.T) {
	d := ptAt(t, "2026-10-10T00:00:00Z")
	h := func(n float64) time.Time { return d.Add(time.Duration(n * float64(time.Hour))) }
	ws := []InactiveWindow{
		{Start: h(10), End: h(20), Kind: "design"},
		{Start: h(15), End: h(25), Kind: "inactive", Inferred: true},
	}
	// 100 h range, union 10..25 = 15 h excluded (the script would take 20).
	ptNear(t, "ActiveHours", ActiveHours(d, h(100), ws), 85)
}

func TestProgressTimeActiveHoursClipsAtEdges(t *testing.T) {
	d := ptAt(t, "2026-10-10T00:00:00Z")
	h := func(n float64) time.Time { return d.Add(time.Duration(n * float64(time.Hour))) }
	ws := []InactiveWindow{
		{Start: h(-5), End: h(4)},    // 4 h inside the range
		{Start: h(46), End: h(60)},   // 2 h inside the range
		{Start: h(100), End: h(110)}, // outside
	}
	ptNear(t, "ActiveHours", ActiveHours(d, h(48), ws), 48-4-2)
	ptNear(t, "no windows", ActiveHours(d, h(48), nil), 48)
}

func TestProgressTimeActiveHoursFloor(t *testing.T) {
	d := ptAt(t, "2026-10-10T00:00:00Z")
	ws := []InactiveWindow{{Start: d, End: d.Add(10 * time.Hour)}}
	ptNear(t, "fully excluded", ActiveHours(d, d.Add(10*time.Hour), ws), 0.1)
	ptNear(t, "tiny range", ActiveHours(d, d.Add(time.Minute), nil), 0.1)
	ptNear(t, "reversed range", ActiveHours(d.Add(time.Hour), d, nil), 0.1)
}

func TestProgressTimeMedian(t *testing.T) {
	if _, ok := Median(nil); ok {
		t.Fatal("empty median reported ok")
	}
	in := []float64{9, 1, 5}
	if v, ok := Median(in); !ok || v != 5 {
		t.Fatalf("odd median = %v, %v", v, ok)
	}
	if in[0] != 9 {
		t.Fatalf("input was sorted in place: %v", in)
	}
	// Even count: the upper middle value, as the status script does.
	if v, _ := Median([]float64{4, 1, 3, 2}); v != 3 {
		t.Fatalf("even median = %v, want 3", v)
	}
	if v, _ := Median([]float64{7}); v != 7 {
		t.Fatalf("single median = %v", v)
	}
}
