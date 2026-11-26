package server

import (
	"strings"

	"github.com/dmz006/datawatch/internal/agents"
	"github.com/dmz006/datawatch/internal/git"
)

// workerPushSettings are the push settings a container worker must obey: the
// spawn's own push-on-complete override when it has one, else the daemon's,
// and the daemon's secret scan settings. An override that is not a known
// policy is passed on as it is, which the worker refuses. Without a
// configuration nothing is sent, and the worker then pushes nothing.
func (s *Server) workerPushSettings(override string) *agents.BootstrapPush {
	if s.cfg == nil {
		return nil
	}
	policy := s.cfg.Git.EffectivePushOnComplete()
	if o := strings.TrimSpace(override); o != "" {
		policy = o
		if p, ok := git.ParsePolicy(o); ok {
			policy = p
		}
	}
	allow := s.cfg.Git.EffectiveSecretScanAllowList()
	return &agents.BootstrapPush{
		Policy:                 policy,
		SecretScanMode:         s.cfg.Git.EffectiveSecretScanMode(),
		SecretScanDetectors:    s.cfg.Git.EffectiveSecretScanDetectors(),
		SecretScanAllowPaths:   allow.Paths,
		SecretScanFingerprints: allow.Fingerprints,
	}
}
