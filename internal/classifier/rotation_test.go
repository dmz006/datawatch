package classifier

import (
	"math"
	"os"
	"testing"
)

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
func makeDir(p string) error      { return os.Mkdir(p, 0o700) }

// biasItems builds n fixture items for 2 options. contentP is the probability
// the model gives the true option when unbiased; posBias shifts mass to letter A.
func biasItems(n int, contentP, posBias float64) []BiasItem {
	items := make([]BiasItem, n)
	for i := range items {
		trueOpt := i % 2 // original option index that is correct
		rows := make([][]float64, 2)
		for k := range rows {
			row := make([]float64, 2)
			// letter position of the true option under rotation k
			pos := (trueOpt + k) % 2
			row[pos], row[1-pos] = contentP, 1-contentP
			// shift mass toward letter A
			shift := posBias * row[1]
			row[0] += shift
			row[1] -= shift
			rows[k] = row
		}
		items[i] = BiasItem{ID: string(rune('a' + i%26)), ByRotation: rows}
	}
	return items
}

func TestMeasurePositionBias(t *testing.T) {
	r, err := MeasurePositionBias(biasItems(20, 0.9, 0), CalibrationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if r.PositionBias > 1e-9 || r.FlipRate != 0 || r.NeedsRotation {
		t.Fatalf("unbiased: %+v", r)
	}
	r, _ = MeasurePositionBias(biasItems(20, 0.9, 0.9), CalibrationConfig{})
	if !r.NeedsRotation || r.PositionBias < 0.1 || r.PositionMass[0] <= r.PositionMass[1] {
		t.Fatalf("biased: %+v", r)
	}
	// Strong bias flips winners across rotations.
	r, _ = MeasurePositionBias(biasItems(20, 0.6, 1), CalibrationConfig{})
	if r.FlipRate == 0 || !r.NeedsRotation {
		t.Fatalf("flips: %+v", r)
	}
	// Mass sums to 1 over positions.
	if s := r.PositionMass[0] + r.PositionMass[1]; math.Abs(s-1) > 1e-9 {
		t.Fatalf("mass %v", s)
	}
}

func TestMeasurePositionBiasRejectsBadInput(t *testing.T) {
	if _, err := MeasurePositionBias(nil, CalibrationConfig{}); err == nil {
		t.Fatal("empty")
	}
	if _, err := MeasurePositionBias([]BiasItem{{ID: "x", ByRotation: [][]float64{{1}}}}, CalibrationConfig{}); err == nil {
		t.Fatal("one rotation")
	}
	ok := BiasItem{ID: "a", ByRotation: [][]float64{{0.9, 0.1}, {0.1, 0.9}}}
	for name, bad := range map[string]BiasItem{
		"rotation count": {ID: "b", ByRotation: [][]float64{{0.9, 0.1}, {0.1, 0.9}, {0.5, 0.5}}},
		"row width":      {ID: "c", ByRotation: [][]float64{{0.9, 0.1}, {0.1}}},
		"negative":       {ID: "d", ByRotation: [][]float64{{0.9, -0.1}, {0.1, 0.9}}},
		"nan":            {ID: "e", ByRotation: [][]float64{{math.NaN(), 0.1}, {0.1, 0.9}}},
	} {
		if _, err := MeasurePositionBias([]BiasItem{ok, bad}, CalibrationConfig{}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestBiasFromFixtureLogprobs(t *testing.T) {
	// The recorded logprobs fixture gives A=0.93/B=0.07 for the original order.
	var lr LogprobsResponse
	if err := LoadFixture(FixtureLogprobs, &lr); err != nil {
		t.Fatal(err)
	}
	probs, mass := ScoreFirstToken(lr.Choices[0].Logprobs.Content[0].TopLogprobs, []string{"A", "B"})
	if mass <= 0.9 {
		t.Fatalf("mass %v", mass)
	}
	pa, pb := probs["A"]/mass, probs["B"]/mass
	// Pair with a mirrored rotation row, as a content-following model would give.
	r, err := MeasurePositionBias([]BiasItem{{ID: "fx", ByRotation: [][]float64{{pa, pb}, {pb, pa}}}}, CalibrationConfig{})
	if err != nil || r.NeedsRotation {
		t.Fatalf("%v %+v", err, r)
	}
}
