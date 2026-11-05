package main

import (
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/classifier"
)

func TestClassifierStatusSource(t *testing.T) {
	dir := t.TempDir()
	src := newClassifierStatusSource(dir, nil)
	if p := src.Probes(); p != nil {
		t.Fatalf("no prober must report no probes, got %v", p)
	}
	if recs, err := src.Calibrations(); err != nil || recs != nil {
		t.Fatalf("empty store = %v %v", recs, err)
	}
	fp := classifier.NewFingerprint("m", "q4", "n1", "p", "t")
	if err := (classifier.FileStore{Dir: filepath.Join(dir, "classifier")}).Save(&classifier.Record{Fingerprint: fp, Threshold: 0.7}); err != nil {
		t.Fatal(err)
	}
	recs, err := src.Calibrations()
	if err != nil || len(recs) != 1 || recs[0].Threshold != 0.7 {
		t.Fatalf("calibrations = %v %v", recs, err)
	}
	withProber := newClassifierStatusSource(dir, classifier.NewProber(nil, nil, classifier.Config{}, nil))
	if p := withProber.Probes(); len(p) != 0 {
		t.Fatalf("fresh prober snapshot = %v", p)
	}
}
