package usagecalc

import "time"

// laneLiveWindow is how far before now a reading still counts as taken at the clock.
const laneLiveWindow = 60 * time.Second

// LaneStamper decides the Claude lane count and its source for a reading taken at at, when
// the keeper stores it at now. lanes is nil when the count is unknown.
type LaneStamper func(at time.Time, now time.Time) (lanes *int, source string)

// LaneLookup answers what the lane count was at an earlier moment; *LaneHistory does.
type LaneLookup interface {
	Lookup(t time.Time) (count int, ok bool)
}

// NewLaneStamper builds the keeper's stamper. A reading within 60 seconds before now, or
// after it, is live and takes the ledger count now. An earlier reading takes the history's
// count and is recorded, or, when the history does not cover it, the ledger count now and
// is current. When the counter reports ok false there is no count to take: nil lanes and
// the unavailable source. A nil history covers nothing.
func NewLaneStamper(counter LaneCounter, history LaneLookup) LaneStamper {
	ledgerNow := func() *int {
		if counter == nil {
			return nil
		}
		n, ok := counter.Lanes()
		if !ok {
			return nil
		}
		return &n
	}
	return func(at time.Time, now time.Time) (*int, string) {
		source := LanesLive
		if now.Sub(at) > laneLiveWindow {
			if history != nil {
				if n, ok := history.Lookup(at); ok {
					return &n, LanesRecorded
				}
			}
			source = LanesCurrent
		}
		n := ledgerNow()
		if n == nil {
			return nil, LanesUnavailable
		}
		return n, source
	}
}
