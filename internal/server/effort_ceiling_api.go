package server

import "github.com/dmz006/datawatch/internal/autonomous"

// effortCeilingAPI is the optional part of the Automaton manager that the
// effort ceiling routes need. autonomous.API implements it.
type effortCeilingAPI interface {
	SetAutomationEffortCeiling(id string, ceiling map[string]string, actor string, expectedVersion int64) (any, error)
	SetTaskEffortCeiling(prdID, taskID, ceiling, actor string) (any, error)
}

var _ effortCeilingAPI = (*autonomous.API)(nil)
