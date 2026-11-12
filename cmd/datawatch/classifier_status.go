package main

import (
	"path/filepath"

	"github.com/dmz006/datawatch/internal/classifier"
)

// classifierStatusSource reads the classifier's live state for the status
// surfaces: the prober's cached capability results and the calibration records
// under <data dir>/classifier. A nil prober reports no probes (nothing has
// probed yet), never a fabricated state.
type classifierStatusSource struct {
	prober *classifier.Prober
	store  classifier.FileStore
}

// newClassifierStatusSource builds the source for the daemon's data directory.
func newClassifierStatusSource(dataDir string, prober *classifier.Prober) *classifierStatusSource {
	return &classifierStatusSource{prober: prober, store: classifier.FileStore{Dir: filepath.Join(dataDir, "classifier")}}
}

func (c *classifierStatusSource) Probes() []classifier.ProbeStatus {
	if c.prober == nil {
		return nil
	}
	return c.prober.Snapshot()
}

func (c *classifierStatusSource) Calibrations() ([]*classifier.Record, error) {
	return c.store.List()
}
