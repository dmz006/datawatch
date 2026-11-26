// Reconciling journalled merge pushes from the daemon: at start, and before
// the supervisor loop lists what is waiting to merge. It never pushes,
// deploys or runs a hook; it records what the remote already holds, so it runs
// whether or not merge runs are switched on.

package server

import (
	"context"
	"log"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/mergepipeline"
)

// ReconcileMergePushesAtStart repairs what an earlier daemon pushed and did
// not finish recording.
func ReconcileMergePushesAtStart(ctx context.Context, mgr *autonomous.Manager, cfg *config.Config) {
	if mgr == nil || cfg == nil {
		return
	}
	mergeRunMu.Lock()
	defer mergeRunMu.Unlock()
	reconcileMergePushes(ctx, mgr, dataDirPath(cfg.DataDir), cfg.Autonomous.MergePipeline.Clone())
}

func reconcileMergePushes(ctx context.Context, mgr *autonomous.Manager, dataDir string, cfg config.MergePipelineConfig) {
	repo := mgr.MergeBaseRepo()
	if repo == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, mergeReconcileTimeout)
	defer cancel()
	runner := newMergeRunner(mgr, cfg, repo, filepath.Base(repo), dataDir, mergepipeline.ExecShipRunner{})
	if _, err := runner.ReconcilePushes(ctx); err != nil {
		log.Printf("[merge-pipeline] reconcile journalled pushes in %s: %v", repo, err)
	}
}
