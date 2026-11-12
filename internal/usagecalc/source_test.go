package usagecalc

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func nanValue() float64 { return math.NaN() }
func infValue() float64 { return math.Inf(1) }

// fakeSource is a ReadingSource that returns canned results and counts its reads.
type fakeSource struct {
	name    string
	reading Reading
	err     error
	reads   int
}

func (f *fakeSource) Name() string { return f.name }

func (f *fakeSource) Read(ctx context.Context) (Reading, error) {
	f.reads++
	if err := ctx.Err(); err != nil {
		return Reading{}, err
	}
	if f.err != nil {
		return Reading{}, f.err
	}
	return copyReading(f.reading), nil
}

var _ ReadingSource = (*fakeSource)(nil)
var _ ReadingSource = (*TypedSource)(nil)

func TestSourceTypedAndPlanned(t *testing.T) {
	now := func() time.Time { return storeTestNow }
	ctx := context.Background()

	t.Run("typed returns the supplied reading", func(t *testing.T) {
		in := Reading{At: storeTestNow.Add(-time.Hour), Percent: fptr(42), Source: "ignored"}
		src, err := NewSource(SourceTyped, in, now)
		if err != nil {
			t.Fatal(err)
		}
		if src.Name() != "typed" {
			t.Fatalf("name = %q", src.Name())
		}
		got, err := src.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != "typed" || *got.Percent != 42 || !got.At.Equal(in.At) {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("typed without a time uses the clock", func(t *testing.T) {
		got, err := NewTypedSource(Reading{SpendUSD: fptr(9)}, now).Read(ctx)
		if err != nil || !got.At.Equal(storeTestNow) {
			t.Fatalf("got %+v err %v", got, err)
		}
	})

	t.Run("typed does not share memory with the request", func(t *testing.T) {
		p := 10.0
		src := NewTypedSource(Reading{At: storeTestNow, Percent: &p}, now)
		p = 55
		first, _ := src.Read(ctx)
		*first.Percent = 77
		second, _ := src.Read(ctx)
		if *second.Percent != 10 {
			t.Fatalf("percent = %v, want 10", *second.Percent)
		}
	})

	t.Run("typed honours a cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := NewTypedSource(Reading{At: storeTestNow, Percent: fptr(1)}, now).Read(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("planned kinds are not built", func(t *testing.T) {
		if SourcePasted != "pasted" || SourceAccount != "account" {
			t.Fatalf("constants: %q %q", SourcePasted, SourceAccount)
		}
		for _, kind := range []string{SourcePasted, SourceAccount} {
			src, err := NewSource(kind, Reading{}, now)
			if !errors.Is(err, ErrSourceNotBuilt) || src != nil {
				t.Fatalf("%s: src=%v err=%v", kind, src, err)
			}
		}
		if src, err := NewPastedSource("weekly 12% used"); !errors.Is(err, ErrSourceNotBuilt) || src != nil {
			t.Fatalf("pasted constructor: %v %v", src, err)
		}
		if src, err := NewAccountSource(); !errors.Is(err, ErrSourceNotBuilt) || src != nil {
			t.Fatalf("account constructor: %v %v", src, err)
		}
	})

	t.Run("unknown kind", func(t *testing.T) {
		_, err := NewSource("carrier-pigeon", Reading{}, now)
		if !errors.Is(err, ErrUnknownSource) || errors.Is(err, ErrSourceNotBuilt) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSourceFeedsStore(t *testing.T) {
	clock := newFakeClock(storeTestNow)
	s, _ := openTestStore(t, t.TempDir(), clock)
	ctx := context.Background()

	fake := &fakeSource{name: "fake", reading: Reading{At: storeTestNow.Add(-time.Hour), Percent: fptr(31), Source: "fake"}}
	for _, src := range []ReadingSource{
		fake,
		NewTypedSource(Reading{At: storeTestNow, SpendUSD: fptr(12)}, clock.Now),
	} {
		r, err := src.Read(ctx)
		if err != nil {
			t.Fatalf("%s: %v", src.Name(), err)
		}
		if _, err := s.Append(r); err != nil {
			t.Fatalf("%s: Append: %v", src.Name(), err)
		}
	}
	got := s.List(time.Time{})
	if len(got) != 2 || got[0].Source != "fake" || got[1].Source != SourceTyped {
		t.Fatalf("stored readings: %+v", got)
	}
	if fake.reads != 1 {
		t.Fatalf("fake read %d times", fake.reads)
	}

	boom := &fakeSource{name: "broken", err: errors.New("source down")}
	if _, err := boom.Read(ctx); err == nil || boom.reads != 1 {
		t.Fatalf("fake error not returned: %v", err)
	}
}
