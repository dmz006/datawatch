package server

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// hostLoadAlertKeys are the config keys of autonomous.host_load_alert.
var hostLoadAlertKeys = []string{
	"autonomous.host_load_alert.mode",
	"autonomous.host_load_alert.multiple",
	"autonomous.host_load_alert.minutes",
}

// hostLoadAlertFloat reads a number that may arrive as a JSON number or, from
// a chat `configure` command, as a string.
func hostLoadAlertFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// validateHostLoadAlertPatchKey checks one autonomous.host_load_alert.* value.
// A bad value rejects the whole PUT.
func validateHostLoadAlertPatchKey(k string, v interface{}) error {
	switch k {
	case "autonomous.host_load_alert.mode":
		str, ok := v.(string)
		if !ok || str == "" {
			return fmt.Errorf("%s: must be %q, %q or %q", k,
				config.HostLoadAlertModeOff, config.HostLoadAlertModeAlert, config.HostLoadAlertModeReap)
		}
		return config.ValidateHostLoadAlertMode(strings.TrimSpace(str))
	case "autonomous.host_load_alert.multiple":
		f, ok := hostLoadAlertFloat(v)
		if !ok {
			return fmt.Errorf("%s: must be a number", k)
		}
		return config.ValidateHostLoadAlertMultiple(f)
	case "autonomous.host_load_alert.minutes":
		f, ok := hostLoadAlertFloat(v)
		if !ok || f != float64(int(f)) {
			return fmt.Errorf("%s: must be a whole number of minutes", k)
		}
		return config.ValidateHostLoadAlertMinutes(int(f))
	}
	return nil
}

// applyHostLoadAlertPatchKey stores one already-validated value. A value that
// fails the check is left unchanged.
func applyHostLoadAlertPatchKey(c *config.HostLoadAlertConfig, k string, v interface{}) {
	if validateHostLoadAlertPatchKey(k, v) != nil {
		return
	}
	switch k {
	case "autonomous.host_load_alert.mode":
		c.Mode = strings.TrimSpace(v.(string))
	case "autonomous.host_load_alert.multiple":
		f, _ := hostLoadAlertFloat(v)
		c.Multiple = f
	case "autonomous.host_load_alert.minutes":
		f, _ := hostLoadAlertFloat(v)
		c.Minutes = int(f)
	}
}

// hostLoadAlertForDisplay renders the effective values for GET /api/config,
// so an unset field shows its default rather than a zero.
func hostLoadAlertForDisplay(c config.HostLoadAlertConfig) map[string]interface{} {
	return map[string]interface{}{
		"mode":     c.EffectiveMode(),
		"multiple": c.EffectiveMultiple(),
		"minutes":  c.EffectiveMinutes(),
	}
}

// validateHostLoadAlertObject checks the host_load_alert object of a PUT
// /api/autonomous/config body. An absent field is not checked.
func validateHostLoadAlertObject(raw json.RawMessage) error {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("host_load_alert: must be an object with mode, multiple and minutes")
	}
	for field, v := range obj {
		key := "autonomous.host_load_alert." + field
		known := false
		for _, k := range hostLoadAlertKeys {
			known = known || k == key
		}
		if !known {
			return fmt.Errorf("host_load_alert.%s: unknown field (use mode, multiple or minutes)", field)
		}
		if v == nil {
			continue
		}
		if err := validateHostLoadAlertPatchKey(key, v); err != nil {
			return err
		}
	}
	return nil
}
