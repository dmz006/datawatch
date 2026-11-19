package router

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Configure keys of the daily pending-branch alert.
const (
	unlandedAlertEnabledKey   = "autonomous.unlanded_alert_enabled"
	unlandedAlertThresholdKey = "autonomous.unlanded_alert_threshold"
)

// checkUnlandedAlertValue refuses a bad value for either key before it is
// written, matching the REST validation.
func checkUnlandedAlertValue(key, value string) error {
	v := strings.TrimSpace(value)
	switch key {
	case unlandedAlertEnabledKey:
		if _, err := strconv.ParseBool(v); err != nil {
			return fmt.Errorf("%s: must be true or false", key)
		}
	case unlandedAlertThresholdKey:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: must be a whole number >= 0 (default %d)", key, config.DefaultUnlandedAlertThreshold)
		}
		if err := config.ValidateUnlandedAlertThreshold(&n); err != nil {
			return err
		}
	}
	return nil
}
