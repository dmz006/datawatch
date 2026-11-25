// A launch whose request and worker profile name no effort sends no --effort
// flag, at start and at restart. The built-in default "normal" names nothing.

package session

import (
	"testing"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

func TestUnnamedLaunchCarriesNoEffortFlag(t *testing.T) {
	assertEffort(t, "unnamed-no-default", "", StartOptions{}, "")
	assertEffort(t, "unnamed-builtin-default", "normal", StartOptions{}, "")
	silent := workerprofile.WorkerProfile{Name: "silent"}
	assertEffort(t, "unnamed-profile", "normal", StartOptions{WorkerProfile: silent}, "")
	// Neither word set holds "lots", so the profile names no effort.
	odd := workerprofile.WorkerProfile{Name: "odd", Effort: "lots"}
	assertEffort(t, "unnamed-profile-odd", "normal", StartOptions{WorkerProfile: odd}, "")
}

// Naming "normal" is not the same as naming nothing.
func TestNamedNormalWithBuiltinDefaultIsClaudeMedium(t *testing.T) {
	assertEffort(t, "named-normal", "normal", StartOptions{Effort: "normal"}, "medium")
}

func TestWorkerProfileSessionEffortWordIsMapped(t *testing.T) {
	for word, want := range map[string]string{"quick": "low", "normal": "medium", "thorough": "high"} {
		p := workerprofile.WorkerProfile{Name: "p-" + word, Effort: word}
		assertEffort(t, "profile-word-"+word, "normal", StartOptions{WorkerProfile: p}, want)
	}
}
