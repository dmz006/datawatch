package session

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// ErrPriorityNotInteger is the validation error every surface returns for a
// non-integer session priority.
var ErrPriorityNotInteger = errors.New("priority must be an integer")

// ParsePriority converts a priority value as received on a REST body, MCP
// argument or comm-channel token into an int. Whole numbers are accepted in
// any numeric form or as a decimal string; anything else is rejected.
func ParsePriority(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		if n > math.MaxInt32 || n < math.MinInt32 {
			return 0, ErrPriorityNotInteger
		}
		return int(n), nil
	case float64:
		if n != math.Trunc(n) || n > math.MaxInt32 || n < math.MinInt32 {
			return 0, ErrPriorityNotInteger
		}
		return int(n), nil
	case json.Number:
		return ParsePriority(n.String())
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 32)
		if err != nil {
			return 0, ErrPriorityNotInteger
		}
		return int(i), nil
	}
	return 0, ErrPriorityNotInteger
}
