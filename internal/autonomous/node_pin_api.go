package autonomous

// SetTaskNode pins a task's worker to a compute node (empty node clears the
// pin) and announces the update. See Manager.SetTaskNode for the rules.
func (a *API) SetTaskNode(prdID, taskID, node, actor string) (any, error) {
	out, err := a.M.SetTaskNode(prdID, taskID, node, actor)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return out, nil
}

// SetStoryNode pins a story's tasks to a compute node (empty node clears the
// pin) and announces the update. See Manager.SetStoryNode for the rules.
func (a *API) SetStoryNode(prdID, storyID, node, actor string) (any, error) {
	out, err := a.M.SetStoryNode(prdID, storyID, node, actor)
	if err != nil {
		return nil, err
	}
	a.M.EmitPRDUpdate(prdID)
	return out, nil
}
