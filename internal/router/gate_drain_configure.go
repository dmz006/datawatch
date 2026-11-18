package router

import "github.com/dmz006/datawatch/internal/config"

// gateDrainKey is the configure key for the minutes a graceful restart waits
// for running final gates before it exits.
const gateDrainKey = "autonomous.gate_drain_minutes"

// checkGateDrainValue refuses a value that is not a whole number of minutes
// >= 0, using the same parser as the REST write.
func checkGateDrainValue(value string) error {
	_, err := config.ParseGateDrainMinutes(value)
	return err
}
