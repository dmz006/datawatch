package main

import (
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// progressSnapshotSettings projects the autonomous.progress config onto the
// values a progress snapshot needs. The daemon calls it for every snapshot, so a
// config change applies without a restart.
func progressSnapshotSettings(cfg *config.Config) autonomous.ProgressSnapshotSettings {
	p := cfg.Autonomous.Progress
	since, _ := p.SinceTime()
	var projects map[string]string
	if len(cfg.Projects) > 0 {
		projects = make(map[string]string, len(cfg.Projects))
		for name, e := range cfg.Projects {
			projects[name] = e.Dir
		}
	}
	return autonomous.ProgressSnapshotSettings{
		Projects:      projects,
		RetentionDays: p.EffectiveSnapshotRetentionDays(),
		Lanes:         p.EffectiveLanes(),
		GapHours:      p.EffectiveInactiveGapHours(),
		Since:         since,
		BacklogPath:   p.ResolveBacklogPath(cfg.Autonomous.WorktreeBaseRepo),
	}
}
