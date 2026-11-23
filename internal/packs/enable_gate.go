package packs

import (
	"errors"

	"github.com/dmz006/datawatch/internal/skills"
)

// errNoConnectedRegistry is returned by CheckEnable when no connected skills
// registry source is enabled. The message is shown to the operator verbatim.
var errNoConnectedRegistry = errors.New("packs need at least one connected skills registry source; connect one in the skills registry settings")

// CheckEnable reports whether packs may be enabled given the connected skills
// registry sources. It returns an error when no source in connected is enabled.
// It only inspects the slice it is given; it never reads the skills store, the
// community source, or any registration task.
func CheckEnable(connected []skills.Registry) error {
	for _, r := range connected {
		if r.Enabled {
			return nil
		}
	}
	return errNoConnectedRegistry
}
