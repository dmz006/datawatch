package classifier

import (
	"fmt"
	"math"
)

// BiasItem is one fixture item scored under every cyclic rotation of its
// options. ByRotation[k][p] is the probability the classifier gave the letter
// at position p when option i sat at position (i+k)%n.
type BiasItem struct {
	ID         string      `json:"id"`
	ByRotation [][]float64 `json:"by_rotation"`
}

// BiasReport says whether option rotation is needed on the generic path.
type BiasReport struct {
	Items int `json:"items"`
	// PositionMass is the mean probability per letter position over all items
	// and rotations. With every rotation present each option visits each
	// position once, so an unbiased classifier gives exactly 1/n everywhere.
	PositionMass []float64 `json:"position_mass"`
	// PositionBias is the largest deviation of PositionMass from 1/n.
	PositionBias float64 `json:"position_bias"`
	// FlipRate is the share of items whose winning option changes across rotations.
	FlipRate float64 `json:"flip_rate"`
	// NeedsRotation is true when either measure reaches its threshold.
	NeedsRotation bool `json:"needs_rotation"`
}

// MeasurePositionBias computes the position-bias score on fixtures. It does not
// change any setting: rotation stays off unless classifier.rotate_options is set.
func MeasurePositionBias(items []BiasItem, cfg CalibrationConfig) (*BiasReport, error) {
	cfg = cfg.withDefaults()
	if len(items) == 0 {
		return nil, fmt.Errorf("no bias items")
	}
	n := len(items[0].ByRotation)
	if n < 2 {
		return nil, fmt.Errorf("item %q: need at least 2 rotations", items[0].ID)
	}
	mass := make([]float64, n)
	flips := 0
	for _, it := range items {
		if len(it.ByRotation) != n {
			return nil, fmt.Errorf("item %q: %d rotations, want %d", it.ID, len(it.ByRotation), n)
		}
		winners := map[int]bool{}
		for k, row := range it.ByRotation {
			if len(row) != n {
				return nil, fmt.Errorf("item %q rotation %d: %d scores, want %d", it.ID, k, len(row), n)
			}
			bestI, bestP := 0, math.Inf(-1)
			for i := 0; i < n; i++ {
				p := row[(i+k)%n]
				if math.IsNaN(p) || p < 0 {
					return nil, fmt.Errorf("item %q rotation %d: invalid probability", it.ID, k)
				}
				if p > bestP {
					bestI, bestP = i, p
				}
			}
			winners[bestI] = true
			for p, v := range row {
				mass[p] += v
			}
		}
		if len(winners) > 1 {
			flips++
		}
	}
	rep := &BiasReport{Items: len(items), PositionMass: mass, FlipRate: float64(flips) / float64(len(items))}
	for p := range mass {
		mass[p] /= float64(len(items) * n)
		rep.PositionBias = math.Max(rep.PositionBias, math.Abs(mass[p]-1/float64(n)))
	}
	rep.NeedsRotation = rep.PositionBias >= cfg.RotationBiasThreshold || rep.FlipRate >= cfg.RotationFlipThreshold
	return rep, nil
}
