package autonomous

import "github.com/dmz006/datawatch/internal/config"

// SetPlanningRouting replaces the whole planning_routing block on the live
// config. Unlike the JSON merge SetConfig does, it replaces the ladders and
// classes maps outright, so a PUT of the block removes entries it leaves out.
// The caller validates the block first.
func (a *API) SetPlanningRouting(pr config.PlanningRoutingConfig) error {
	cfg := a.M.Config()
	cfg.PlanningRouting = pr
	a.M.SetConfig(cfg)
	return nil
}
