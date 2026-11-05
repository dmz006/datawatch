package classifier

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// synthSamples builds a deterministic, separable-with-overlap sample set.
func synthSamples(n int) []Sample {
	out := make([]Sample, n)
	for i := range out {
		ok := i%2 == 0
		p := 0.15 + 0.4*float64(i%7)/6 // 0.15..0.55 for failures
		if ok {
			p = 0.5 + 0.49*float64(i%11)/10 // 0.5..0.99 for passes
		}
		out[i] = Sample{ID: fmt.Sprintf("s%03d", i), PassProb: p, GroundOK: ok}
	}
	return out
}

func TestSplitIsDeterministicDisjointAndComplete(t *testing.T) {
	s := synthSamples(200)
	f1, t1, err := SplitSamples(s, CalibrationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	f2, t2, _ := SplitSamples(s, CalibrationConfig{})
	if len(f1) != len(f2) || len(t1) != len(t2) {
		t.Fatal("not deterministic")
	}
	for i := range t1 {
		if t1[i].ID != t2[i].ID {
			t.Fatal("not deterministic")
		}
	}
	if len(f1)+len(t1) != 200 {
		t.Fatal("lost samples")
	}
	in := map[string]bool{}
	for _, x := range f1 {
		in[x.ID] = true
	}
	for _, x := range t1 {
		if in[x.ID] {
			t.Fatalf("%s on both sides", x.ID)
		}
	}
	if frac := float64(len(t1)) / 200; math.Abs(frac-0.30) > 0.1 {
		t.Fatalf("test fraction %v", frac)
	}
	// Order independence: reversing input gives the same membership.
	rev := make([]Sample, len(s))
	for i := range s {
		rev[len(s)-1-i] = s[i]
	}
	_, t3, _ := SplitSamples(rev, CalibrationConfig{})
	if len(t3) != len(t1) {
		t.Fatal("split depends on order")
	}
	// A different seed gives a different split.
	_, t4, _ := SplitSamples(s, CalibrationConfig{SplitSeed: "other"})
	same := len(t4) == len(t1)
	for i := 0; same && i < len(t1); i++ {
		same = t1[i].ID == t4[i].ID
	}
	if same {
		t.Fatal("seed ignored")
	}
	if _, _, err := SplitSamples([]Sample{{ID: "x"}, {ID: "x"}}, CalibrationConfig{}); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestFitThresholdWeightsFalsePass(t *testing.T) {
	s := []Sample{
		{"a", 0.95, true}, {"b", 0.9, true}, {"c", 0.85, true}, {"d", 0.8, false}, {"e", 0.4, false}, {"f", 0.3, false},
	}
	// Weight 5: blocking one pass (0.85 or lower) is cheaper than a false pass at 0.8.
	th, err := FitThreshold(s, CalibrationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if th != 0.85 {
		t.Fatalf("threshold %v", th)
	}
	m := Evaluate(s, th)
	if m.FalsePass != 0 || m.FalseBlock != 0 {
		t.Fatalf("%+v", m)
	}
	// All-fail ground truth: block everything.
	th, _ = FitThreshold([]Sample{{"x", 0.9, false}, {"y", 0.7, false}}, CalibrationConfig{})
	if th <= 0.9 {
		t.Fatalf("expected block-all threshold, got %v", th)
	}
	if _, err := FitThreshold(nil, CalibrationConfig{}); err == nil {
		t.Fatal("empty accepted")
	}
	if _, err := FitThreshold([]Sample{{"x", 1.5, true}}, CalibrationConfig{}); err == nil {
		t.Fatal("out of range accepted")
	}
	if _, err := FitThreshold([]Sample{{"x", math.NaN(), true}}, CalibrationConfig{}); err == nil {
		t.Fatal("NaN accepted")
	}
}

func TestCalibrateScoresOnlyHeldOutSet(t *testing.T) {
	s := synthSamples(300)
	fp := NewFingerprint("m", "q4", "n1", "prompt", "tmpl")
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	rec, err := Calibrate(s, fp, CalibrationConfig{}, now)
	if err != nil {
		t.Fatal(err)
	}
	fit, test, _ := SplitSamples(s, CalibrationConfig{})
	if rec.Fit.N != len(fit) || rec.Test.N != len(test) {
		t.Fatalf("%+v", rec)
	}
	if rec.Fit != Evaluate(fit, rec.Threshold) || rec.Test != Evaluate(test, rec.Threshold) {
		t.Fatal("metrics not from the right sets")
	}
	if rec.FingerprintHash != fp.Hash() || !rec.CreatedAt.Equal(now) {
		t.Fatalf("%+v", rec)
	}
	if !rec.Pass(rec.Threshold) || rec.Pass(rec.Threshold-0.001) {
		t.Fatal("Pass boundary")
	}
	if _, err := Calibrate(s[:10], fp, CalibrationConfig{}, now); err == nil {
		t.Fatal("tiny fit set accepted")
	}
	if _, err := Calibrate(s, fp, CalibrationConfig{MinFitSamples: 1}, now); err != nil {
		t.Fatal(err)
	}
	// Empty test set: one sample id whose hash lands on fit side is hard to
	// force, so use a seed search.
	var one []Sample
	for i := 0; i < 40; i++ {
		one = append(one, Sample{ID: fmt.Sprintf("only%d", i), PassProb: 0.9, GroundOK: true})
	}
	if _, err := Calibrate(one, fp, CalibrationConfig{TestFraction: 0.0001, MinFitSamples: 1}, now); err == nil {
		t.Fatal("empty test set accepted")
	}
	// Invalid probability propagates.
	bad := append(synthSamples(100), Sample{ID: "bad", PassProb: 2})
	if _, err := Calibrate(bad, fp, CalibrationConfig{}, now); err == nil {
		t.Fatal("bad sample accepted")
	}
	if _, err := Calibrate(append(s, s[0]), fp, CalibrationConfig{}, now); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestFingerprintStalenessAndStore(t *testing.T) {
	dir := t.TempDir()
	st := FileStore{Dir: dir + "/sub"}
	fp := NewFingerprint("m", "q4", "n1", "prompt", "tmpl")
	if _, err := st.Require(fp); !IsStale(err) {
		t.Fatalf("missing record must be stale: %v", err)
	}
	rec, err := Calibrate(synthSamples(300), fp, CalibrationConfig{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := st.Require(fp)
	if err != nil || got.Threshold != rec.Threshold {
		t.Fatalf("%v %+v", err, got)
	}
	for name, changed := range map[string]Fingerprint{
		"prompt":   NewFingerprint("m", "q4", "n1", "prompt v2", "tmpl"),
		"template": NewFingerprint("m", "q4", "n1", "prompt", "tmpl v2"),
	} {
		_, err := st.Require(changed)
		if !IsStale(err) || FailureCategory(err) != "infra" {
			t.Fatalf("%s change not stale: %v", name, err)
		}
	}
	// Model, node and quantization changes land in a different slot: no record.
	for _, changed := range []Fingerprint{
		NewFingerprint("m2", "q4", "n1", "prompt", "tmpl"),
		NewFingerprint("m", "q8", "n1", "prompt", "tmpl"),
		NewFingerprint("m", "q4", "n2", "prompt", "tmpl"),
	} {
		if _, err := st.Require(changed); !IsStale(err) {
			t.Fatalf("expected stale for %+v: %v", changed, err)
		}
		if CheckFresh(rec, changed) == nil {
			t.Fatalf("CheckFresh accepted %+v", changed)
		}
	}
	// Tampered hash is stale.
	bad := *rec
	bad.FingerprintHash = "x"
	if !IsStale(CheckFresh(&bad, fp)) || !IsStale(CheckFresh(nil, fp)) {
		t.Fatal("tamper/nil not stale")
	}
	if CheckFresh(rec, fp) != nil {
		t.Fatal("fresh rejected")
	}
	// Recalibrating for the new prompt replaces the record.
	fp2 := NewFingerprint("m", "q4", "n1", "prompt v2", "tmpl")
	rec2, _ := Calibrate(synthSamples(300), fp2, CalibrationConfig{}, time.Now())
	if err := st.Save(rec2); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Require(fp2); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Require(fp); !IsStale(err) {
		t.Fatal("old fingerprint should be stale after recalibration")
	}
	if (&StaleError{Fingerprint: fp, Reason: "r"}).Error() == "" {
		t.Fatal("empty error")
	}
}

func TestFileStoreErrors(t *testing.T) {
	dir := t.TempDir()
	fp := NewFingerprint("m", "q", "n", "p", "t")
	st := FileStore{Dir: dir}
	if err := writeFile(st.path(fp), "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(fp); err == nil {
		t.Fatal("corrupt record accepted")
	}
	if _, err := st.Require(fp); err == nil || IsStale(err) {
		t.Fatalf("corrupt record: %v", err)
	}
	// Dir is a file: Save fails.
	f := dir + "/file"
	_ = writeFile(f, "x")
	if err := (FileStore{Dir: f}).Save(&Record{Fingerprint: fp}); err == nil {
		t.Fatal("save into file accepted")
	}
	// Load on unreadable path (a directory) is an error, not nil.
	if err := makeDir(st.path(fp) + "d"); err != nil {
		t.Fatal(err)
	}
}

func TestCalibrationConfigDefaults(t *testing.T) {
	c := CalibrationConfig{}.withDefaults()
	if c.TestFraction != DefaultTestFraction || c.SplitSeed != DefaultSplitSeed || c.MinFitSamples != DefaultMinFitSamples ||
		c.FalsePassWeight != DefaultFalsePassWeight || c.RotationBiasThreshold != DefaultRotationBiasThreshold ||
		c.RotationFlipThreshold != DefaultRotationFlipThreshold {
		t.Fatalf("%+v", c)
	}
	if (CalibrationConfig{TestFraction: 2}).withDefaults().TestFraction != DefaultTestFraction {
		t.Fatal("bad fraction not defaulted")
	}
}
