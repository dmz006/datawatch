package main

// Planning re-attach (daemon side). After a restart, a planning Automaton whose
// planner session survived is handed to a reader here instead of being reset to
// draft. The reader waits on the session's output with the same completion rules
// as the decompose session it replaces (waitOneShotJSONSession), then hands the
// JSON to the Manager's ingest path, so the decompose completes as if the
// restart had not happened.

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/config"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
)

// planningReattachTimeout bounds how long a re-attached planner may run before
// the reader gives up. It matches the timeout the original decompose used.
const planningReattachTimeout = 60 * time.Minute

// wirePlanningReattach installs the re-attach reader on amgr. It is called once
// at daemon start, next to the other decompose wiring.
func wirePlanningReattach(amgr *autonomouspkg.Manager, cfg *config.Config) {
	amgr.SetPlanningReattachFn(func(prdID, sessionID, outputFile string) {
		go reattachPlanningReader(amgr, cfg, prdID, sessionID, outputFile)
	})
}

// reattachPlanningReader waits for the re-attached planner session sessionID to
// write its plan to outputFile, then ingests it into prdID. outputFile may be
// relative to the PRD's project directory, as the Manager records it. On any
// failure to get output, the Automaton's planning attempt is failed so it does
// not stay in planning.
func reattachPlanningReader(amgr *autonomouspkg.Manager, cfg *config.Config, prdID, sessionID, outputFile string) {
	projectDir := ""
	if prd, ok := amgr.Store().GetPRD(prdID); ok {
		projectDir = prd.ProjectDir
	}
	path := outputFile
	if path != "" && !filepath.IsAbs(path) && projectDir != "" {
		path = filepath.Join(projectDir, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), planningReattachTimeout)
	defer cancel()
	fmt.Printf("[%s] prd=%s re-attached to planner session %s (output=%s)\n", decomposeSessionName, prdID, sessionID, path)
	relocate := oneShotOutputReader(cfg, prdID, decomposeSessionName, projectDir, path, "decompose-output.json", autonomouspkg.RelocateProjectFile)
	content, _, err := waitOneShotJSONSession(ctx, cfg, prdID, decomposeSessionName, "", "", projectDir, path,
		planningReattachTimeout, sessionID, time.Now(), relocate)
	if err != nil {
		fmt.Printf("[%s] prd=%s re-attached planner produced no output: %v\n", decomposeSessionName, prdID, err)
		amgr.FailPlanningReattach(prdID, err)
		return
	}
	if err := amgr.IngestPlannerOutput(prdID, content); err != nil {
		fmt.Printf("[%s] prd=%s re-attached planner output was not ingested: %v\n", decomposeSessionName, prdID, err)
		return
	}
	fmt.Printf("[%s] prd=%s re-attached planner output ingested\n", decomposeSessionName, prdID)
}
