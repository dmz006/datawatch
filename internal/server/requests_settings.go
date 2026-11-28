// The six request settings (delivery job 4). Owner: part B. All are read live.
// For a time, 0 means the default; for a reminder limit, 0 means no reminders.
//
// GET /api/config reports every one at its value in force under
// "delivery_requests". A key that has been set is also reported under
// "delivery", beside the delivery check's own keys.

package server

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
)

const (
	requestTakeByKey       = "delivery.request_take_by_seconds"
	requestDueKey          = "delivery.request_due_seconds"
	requestDueByKindKey    = "delivery.request_due_seconds_by_kind"
	requestIntervalKey     = "delivery.reminder_interval_seconds"
	requestSessionLimitKey = "delivery.reminders_per_session_hour"
	requestHostLimitKey    = "delivery.reminders_per_host_hour"
)

// maxRequestSeconds is the largest time that still is a duration.
const maxRequestSeconds = int64(math.MaxInt64 / int64(time.Second))

// requestWholeKeys are the whole-number settings and their largest values.
var requestWholeKeys = []struct {
	key string
	max int64
}{
	{requestTakeByKey, maxRequestSeconds}, {requestDueKey, maxRequestSeconds}, {requestIntervalKey, maxRequestSeconds},
	{requestSessionLimitKey, math.MaxInt32}, {requestHostLimitKey, math.MaxInt32},
}

// requestWhole reads a whole number from 0 to max that an int holds: where int
// is 32 bits, a larger number is refused whatever max says.
func requestWhole(v interface{}, max int64) (int, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, false
		}
		f = float64(n)
	default:
		return 0, false
	}
	if f != math.Trunc(f) || f < 0 || f > float64(max) || f > float64(math.MaxInt) {
		return 0, false
	}
	return int(f), true
}

// requestDueByKind reads the per-kind due times: an object, the same as text,
// or kind=seconds pairs. A kind at 0 is left out; empty clears the table.
func requestDueByKind(v interface{}) (map[string]int, error) {
	var raw map[string]interface{}
	switch x := v.(type) {
	case map[string]interface{}:
		raw = x
	case string:
		text := strings.TrimSpace(x)
		if strings.HasPrefix(text, "{") {
			if err := json.Unmarshal([]byte(text), &raw); err != nil {
				return nil, fmt.Errorf("not a JSON object")
			}
			break
		}
		raw = map[string]interface{}{}
		for _, pair := range strings.Split(text, ",") {
			if pair = strings.TrimSpace(pair); pair == "" {
				continue
			}
			kind, secs, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("give kind=seconds pairs")
			}
			raw[strings.TrimSpace(kind)] = secs
		}
	default:
		return nil, fmt.Errorf("give an object of kind to seconds")
	}
	out := map[string]int{}
	for kind, value := range raw {
		if _, known := delivery.RuleFor(delivery.RequestKind(kind)); !known {
			return nil, fmt.Errorf("unknown request kind %q", kind)
		}
		n, ok := requestWhole(value, maxRequestSeconds)
		if !ok {
			return nil, fmt.Errorf("%s: seconds are a whole number, 0 or more", kind)
		}
		if n > 0 {
			out[kind] = n
		}
	}
	return out, nil
}

// firstRequestSettingError refuses a bad value before anything in the same PUT
// is applied.
func firstRequestSettingError(patch map[string]interface{}) error {
	for _, k := range requestWholeKeys {
		if v, ok := patch[k.key]; ok {
			if _, good := requestWhole(v, k.max); !good {
				return fmt.Errorf("%s: must be a whole number, 0 or more and at most %d", k.key, k.max)
			}
		}
	}
	if v, ok := patch[requestDueByKindKey]; ok {
		if _, err := requestDueByKind(v); err != nil {
			return fmt.Errorf("%s: %w", requestDueByKindKey, err)
		}
	}
	return nil
}

// applyRequestSetting stores one request setting; a bad value changes nothing.
func applyRequestSetting(cfg *config.Config, key string, v interface{}) {
	if key == requestDueByKindKey {
		if m, err := requestDueByKind(v); err == nil {
			if len(m) == 0 {
				m = nil
			}
			cfg.Delivery.RequestDueSecondsByKind = m
		}
		return
	}
	i := slices.IndexFunc(requestWholeKeys, func(k struct {
		key string
		max int64
	}) bool {
		return k.key == key
	})
	if i < 0 {
		return
	}
	n, ok := requestWhole(v, requestWholeKeys[i].max)
	if !ok {
		return
	}
	switch key {
	case requestTakeByKey:
		cfg.Delivery.RequestTakeBySeconds = n
	case requestDueKey:
		cfg.Delivery.RequestDueSeconds = n
	case requestIntervalKey:
		cfg.Delivery.ReminderIntervalSeconds = n
	case requestSessionLimitKey:
		cfg.Delivery.RemindersPerSessionHour = &n
	case requestHostLimitKey:
		cfg.Delivery.RemindersPerHostHour = &n
	}
}

// requestSettingsSurface is every request setting at its value in force.
func requestSettingsSurface(c config.DeliveryConfig) map[string]interface{} {
	seconds := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return v
	}
	limit := func(v *int, def int) int {
		if v == nil {
			return def
		}
		return max(*v, 0)
	}
	byKind := map[string]interface{}{}
	for kind, s := range c.RequestDueSecondsByKind {
		if s > 0 {
			byKind[kind] = s
		}
	}
	return map[string]interface{}{
		"request_take_by_seconds":     seconds(c.RequestTakeBySeconds, config.DefaultRequestTakeBySeconds),
		"request_due_seconds":         seconds(c.RequestDueSeconds, config.DefaultRequestDueSeconds),
		"request_due_seconds_by_kind": byKind,
		"reminder_interval_seconds":   seconds(c.ReminderIntervalSeconds, config.DefaultReminderIntervalSeconds),
		"reminders_per_session_hour":  limit(c.RemindersPerSessionHour, config.DefaultRemindersPerSessionHour),
		"reminders_per_host_hour":     limit(c.RemindersPerHostHour, config.DefaultRemindersPerHostHour),
	}
}

// addSetRequestSettings puts the settings that have been set into the
// "delivery" block.
func addSetRequestSettings(block map[string]interface{}, c config.DeliveryConfig) {
	set := map[string]bool{
		"request_take_by_seconds": c.RequestTakeBySeconds > 0, "request_due_seconds": c.RequestDueSeconds > 0,
		"request_due_seconds_by_kind": len(c.RequestDueSecondsByKind) > 0, "reminder_interval_seconds": c.ReminderIntervalSeconds > 0,
		"reminders_per_session_hour": c.RemindersPerSessionHour != nil, "reminders_per_host_hour": c.RemindersPerHostHour != nil,
	}
	for name, v := range requestSettingsSurface(c) {
		if set[name] {
			block[name] = v
		}
	}
}
