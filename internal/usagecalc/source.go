package usagecalc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Kinds of reading source. Only the typed source is built; the others are planned.
const (
	// SourceTyped is a reading the operator typed in; the request supplies it.
	SourceTyped = "typed"
	// SourcePasted is a reading parsed from pasted usage-screen text. Planned, not built.
	SourcePasted = "pasted"
	// SourceAccount is a reading fetched from an account or API source. Planned, not built.
	SourceAccount = "account"
)

// ErrSourceNotBuilt is returned by the constructor of a planned source kind; the REST
// layer answers 501 for it.
var ErrSourceNotBuilt = errors.New("usage reading source is not built yet")

// ErrUnknownSource is returned for a source kind that is neither built nor planned.
var ErrUnknownSource = errors.New("unknown usage reading source")

// ReadingSource produces a Reading. Every source yields the same record, so the store and
// the calculator do not care where a reading came from.
type ReadingSource interface {
	Name() string
	Read(ctx context.Context) (Reading, error)
}

// TypedSource returns the reading the request supplied.
type TypedSource struct {
	reading Reading
	now     func() time.Time
}

// NewTypedSource wraps a request-supplied reading. A reading without a time is dated by
// the injected clock when it is read.
func NewTypedSource(r Reading, now func() time.Time) *TypedSource {
	return &TypedSource{reading: copyReading(r), now: now}
}

// Name is SourceTyped.
func (t *TypedSource) Name() string { return SourceTyped }

// Read returns a copy of the reading marked as typed. It does not validate the figures;
// the store does that on Append.
func (t *TypedSource) Read(ctx context.Context) (Reading, error) {
	if err := ctx.Err(); err != nil {
		return Reading{}, err
	}
	r := copyReading(t.reading)
	r.Source = SourceTyped
	if r.At.IsZero() && t.now != nil {
		r.At = t.now()
	}
	return r, nil
}

// NewPastedSource is the planned source for pasted usage-screen text.
func NewPastedSource(text string) (ReadingSource, error) {
	return nil, fmt.Errorf("%w: %s", ErrSourceNotBuilt, SourcePasted)
}

// NewAccountSource is the planned source for an account or API reading.
func NewAccountSource() (ReadingSource, error) {
	return nil, fmt.Errorf("%w: %s", ErrSourceNotBuilt, SourceAccount)
}

// NewSource builds the source named by kind. For the typed kind r is the reading to
// return; the planned kinds return ErrSourceNotBuilt and any other kind ErrUnknownSource.
func NewSource(kind string, r Reading, now func() time.Time) (ReadingSource, error) {
	switch kind {
	case SourceTyped:
		return NewTypedSource(r, now), nil
	case SourcePasted:
		return NewPastedSource("")
	case SourceAccount:
		return NewAccountSource()
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, kind)
	}
}
