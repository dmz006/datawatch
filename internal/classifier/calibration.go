package classifier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Fingerprint identifies what a calibration is valid for. Thresholds fitted for
// one model, prompt, chat template, quantization and node are not valid for any
// other combination.
type Fingerprint struct {
	Model        string `json:"model"`
	Quantization string `json:"quantization"`
	Node         string `json:"node"`
	// PromptHash and TemplateHash are SHA-256 hex digests of the prompt text
	// and the chat template.
	PromptHash   string `json:"prompt_hash"`
	TemplateHash string `json:"template_hash"`
}

// NewFingerprint hashes the prompt and chat template into a Fingerprint.
func NewFingerprint(model, quantization, node, prompt, chatTemplate string) Fingerprint {
	return Fingerprint{Model: model, Quantization: quantization, Node: node,
		PromptHash: digest(prompt), TemplateHash: digest(chatTemplate)}
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// Hash is the digest of every fingerprint field.
func (f Fingerprint) Hash() string {
	b, _ := json.Marshal(f) //nolint:errcheck // plain struct of strings
	return digest(string(b))
}

// slot is the key under which a record is stored: everything except the prompt
// and template hashes, so a changed prompt or template finds the old record
// and is reported stale rather than silently missing.
func (f Fingerprint) slot() string {
	return digest(f.Model + "\x00" + f.Quantization + "\x00" + f.Node)
}

// Sample is one labelled calibration item: the classifier's probability for
// the pass option and the scripted ground truth.
type Sample struct {
	ID       string  `json:"id"`
	PassProb float64 `json:"pass_prob"`
	GroundOK bool    `json:"ground_ok"`
}

// Metrics scores a threshold on one sample set.
type Metrics struct {
	N              int     `json:"n"`
	FalsePass      int     `json:"false_pass"`       // passed, ground truth fails
	FalseBlock     int     `json:"false_block"`      // blocked, ground truth passes
	FalsePassRate  float64 `json:"false_pass_rate"`  // over ground-truth failures
	FalseBlockRate float64 `json:"false_block_rate"` // over ground-truth passes
}

// Record is a stored calibration.
type Record struct {
	Fingerprint     Fingerprint `json:"fingerprint"`
	FingerprintHash string      `json:"fingerprint_hash"`
	// Threshold: pass when the pass-option probability is at or above it.
	Threshold float64   `json:"threshold"`
	Fit       Metrics   `json:"fit"`
	Test      Metrics   `json:"test"`
	CreatedAt time.Time `json:"created_at"`
}

// StaleError is returned when no calibration exists for the fingerprint or the
// stored one was fitted for a different prompt or chat template. The caller
// blocks with the "infra" category until the combination is re-calibrated.
type StaleError struct {
	Fingerprint Fingerprint
	Reason      string
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("classifier: calibration for model %q node %q is stale: %s", e.Fingerprint.Model, e.Fingerprint.Node, e.Reason)
}

// IsStale reports whether err is (or wraps) a StaleError.
func IsStale(err error) bool {
	var se *StaleError
	return errors.As(err, &se)
}

// CheckFresh returns a StaleError unless rec was fitted for exactly fp.
func CheckFresh(rec *Record, fp Fingerprint) error {
	if rec == nil {
		return &StaleError{fp, "no calibration record"}
	}
	if rec.Fingerprint != fp || rec.FingerprintHash != fp.Hash() {
		return &StaleError{fp, "fingerprint changed (model, prompt, chat template, quantization or node)"}
	}
	return nil
}

// SplitSamples deterministically divides samples into a fit set and a test
// set. The sets are disjoint by construction: each sample's side depends only
// on a hash of the seed and its ID. Duplicate IDs are an error because they
// could land the same item on both sides' statistics.
func SplitSamples(samples []Sample, cfg CalibrationConfig) (fit, test []Sample, err error) {
	cfg = cfg.withDefaults()
	seen := map[string]bool{}
	cut := uint64(cfg.TestFraction * 10000)
	for _, s := range samples {
		if seen[s.ID] {
			return nil, nil, fmt.Errorf("duplicate sample id %q", s.ID)
		}
		seen[s.ID] = true
		h := sha256.Sum256([]byte(cfg.SplitSeed + "\x00" + s.ID))
		v := (uint64(h[0])<<24 | uint64(h[1])<<16 | uint64(h[2])<<8 | uint64(h[3])) % 10000
		if v < cut {
			test = append(test, s)
		} else {
			fit = append(fit, s)
		}
	}
	return fit, test, nil
}

// Evaluate scores a threshold on samples.
func Evaluate(samples []Sample, threshold float64) Metrics {
	m := Metrics{N: len(samples)}
	var neg, pos int
	for _, s := range samples {
		pass := s.PassProb >= threshold
		if s.GroundOK {
			pos++
			if !pass {
				m.FalseBlock++
			}
		} else {
			neg++
			if pass {
				m.FalsePass++
			}
		}
	}
	if neg > 0 {
		m.FalsePassRate = float64(m.FalsePass) / float64(neg)
	}
	if pos > 0 {
		m.FalseBlockRate = float64(m.FalseBlock) / float64(pos)
	}
	return m
}

// FitThreshold picks the threshold that minimises FalsePassWeight*falsePasses +
// falseBlocks on samples. Candidates are the observed scores plus one value
// above the maximum (block everything). Ties go to the higher (stricter)
// threshold, so the result is deterministic and never looser than necessary.
func FitThreshold(samples []Sample, cfg CalibrationConfig) (float64, error) {
	cfg = cfg.withDefaults()
	if len(samples) == 0 {
		return 0, fmt.Errorf("no samples to fit")
	}
	cands := make([]float64, 0, len(samples)+1)
	maxv := math.Inf(-1)
	for _, s := range samples {
		if math.IsNaN(s.PassProb) || s.PassProb < 0 || s.PassProb > 1 {
			return 0, fmt.Errorf("sample %q: pass probability %v outside [0,1]", s.ID, s.PassProb)
		}
		cands = append(cands, s.PassProb)
		maxv = math.Max(maxv, s.PassProb)
	}
	cands = append(cands, math.Nextafter(maxv, math.Inf(1)))
	sort.Float64s(cands)
	best, bestCost := 0.0, math.Inf(1)
	for _, t := range cands {
		m := Evaluate(samples, t)
		cost := cfg.FalsePassWeight*float64(m.FalsePass) + float64(m.FalseBlock)
		if cost <= bestCost { // ascending order: later (stricter) wins ties
			best, bestCost = t, cost
		}
	}
	return best, nil
}

// Calibrate splits samples, fits a threshold on the fit set only and scores it
// on the test set only. It refuses a fit set smaller than MinFitSamples or an
// empty test set.
func Calibrate(samples []Sample, fp Fingerprint, cfg CalibrationConfig, now time.Time) (*Record, error) {
	cfg = cfg.withDefaults()
	fit, test, err := SplitSamples(samples, cfg)
	if err != nil {
		return nil, err
	}
	if len(fit) < cfg.MinFitSamples {
		return nil, fmt.Errorf("fit set has %d samples, need at least %d", len(fit), cfg.MinFitSamples)
	}
	if len(test) == 0 {
		return nil, fmt.Errorf("test set is empty")
	}
	t, err := FitThreshold(fit, cfg)
	if err != nil {
		return nil, err
	}
	return &Record{Fingerprint: fp, FingerprintHash: fp.Hash(), Threshold: t,
		Fit: Evaluate(fit, t), Test: Evaluate(test, t), CreatedAt: now.UTC()}, nil
}

// Pass reports whether passProb clears the calibrated threshold. A probability
// below it is a block; callers never turn a block back into a pass.
func (r *Record) Pass(passProb float64) bool { return passProb >= r.Threshold }

// FileStore keeps calibration records as JSON files, one per
// (model, quantization, node).
type FileStore struct{ Dir string }

func (s FileStore) path(fp Fingerprint) string {
	return filepath.Join(s.Dir, "calibration-"+fp.slot()+".json")
}

// Save writes rec, replacing any record for the same (model, quantization, node).
func (s FileStore) Save(rec *Record) error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	p := s.path(rec.Fingerprint)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Load returns the stored record for fp's slot, or nil when none exists.
func (s FileStore) Load(fp Fingerprint) (*Record, error) {
	rec, err := s.loadFile(s.path(fp))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return rec, err
}

// loadFile reads one record file. A missing file returns an error wrapping
// os.ErrNotExist.
func (s FileStore) loadFile(path string) (*Record, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- path is built from a digest or listed under Dir
	if err != nil {
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, fmt.Errorf("calibration record: %w", err)
	}
	return &rec, nil
}

// Require returns the stored record only when it is fresh for fp; otherwise a
// StaleError that forces re-calibration. A read error is returned as is.
func (s FileStore) Require(fp Fingerprint) (*Record, error) {
	rec, err := s.Load(fp)
	if err != nil {
		return nil, err
	}
	if err := CheckFresh(rec, fp); err != nil {
		return nil, err
	}
	return rec, nil
}
