package server

import (
	"math"
	"strconv"
	"testing"
)

// A request time is either stored as the number given or refused. Where int is
// 32 bits, a time above its range is refused.
func TestRequestSecondsStayInRange(t *testing.T) {
	for _, v := range []interface{}{math.NaN(), math.Inf(1), math.Inf(-1), -1.0, 1.5, 1e300, -1, float64(maxRequestSeconds) + 1, "-1", "1.5", "9223372036854775807", "9223372036854775808"} {
		if n, ok := requestWhole(v, maxRequestSeconds); ok {
			t.Errorf("%T %v: accepted as %d", v, v, n)
		}
	}
	for _, want := range []int64{0, 1, math.MaxInt32 - 1, math.MaxInt32, math.MaxInt32 + 1, 1 << 32, maxRequestSeconds} {
		fits := want <= int64(math.MaxInt)
		for form, v := range map[string]interface{}{"number": float64(want), "text": strconv.FormatInt(want, 10)} {
			n, ok := requestWhole(v, maxRequestSeconds)
			if ok != fits || (ok && int64(n) != want) {
				t.Errorf("%s %d: got %d, accepted=%v; want accepted=%v and the same number", form, want, n, ok, fits)
			}
		}
		for _, key := range []string{requestTakeByKey, requestDueKey, requestIntervalKey} {
			if err := firstRequestSettingError(map[string]interface{}{key: float64(want)}); (err == nil) != fits {
				t.Errorf("%s=%d: error %v, want refused=%v", key, want, err, !fits)
			}
		}
		byKind, err := requestDueByKind("ask=" + strconv.FormatInt(want, 10))
		switch {
		case !fits && err == nil:
			t.Errorf("by kind %d: accepted as %v", want, byKind)
		case fits && (err != nil || int64(byKind["ask"]) != want):
			t.Errorf("by kind %d: got %v, %v", want, byKind, err)
		}
	}
}
