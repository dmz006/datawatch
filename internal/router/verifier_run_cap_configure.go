// comm `configure autonomous.verifier_max_run_seconds=<n>` refusal path.

package router

import (
	"fmt"
	"strconv"

	"github.com/dmz006/datawatch/internal/config"
)

// verifierRunCapKey is the configure key that caps one verifier run's
// wall-clock length. The value is stored by the REST config setter, which
// validates it through config.ValidateVerifierRunCapSeconds; this file only
// refuses bad input before the value leaves the comm channel.
const verifierRunCapKey = "autonomous.verifier_max_run_seconds"

// checkVerifierRunCapValue accepts a whole, non-negative number of seconds
// (0 = no cap). Anything else is refused with an error that names the key.
func checkVerifierRunCapValue(value string) error {
	n, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be a whole number of seconds (0 = no cap), got %q", verifierRunCapKey, value)
	}
	return config.ValidateVerifierRunCapSeconds(n)
}
