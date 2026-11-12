package autonomous

// effectivePlanning returns the planning backend and model that apply to a
// decompose session before the PRD's own decomposition_profile and
// decomposition_model are considered. The Automation's planning fields sit
// between those PRD fields and the global planning config:
//
//  1. auto.PlanningBackend set: that backend with auto.PlanningModel. The model
//     may be empty; it is never paired with a different level's backend.
//  2. only auto.PlanningModel set: the global planning backend with that model.
//  3. otherwise (or a nil auto): the global planning backend and model.
//
// The Automation's execution and verification defaults play no part here.
func effectivePlanning(auto *Automation, cfg Config) (backend, model string) {
	if auto != nil {
		if auto.PlanningBackend != "" {
			return auto.PlanningBackend, auto.PlanningModel
		}
		if auto.PlanningModel != "" {
			return cfg.PlanningBackend, auto.PlanningModel
		}
	}
	return cfg.PlanningBackend, cfg.PlanningModel
}
