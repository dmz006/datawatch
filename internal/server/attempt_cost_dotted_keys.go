package server

// Per-kind dotted keys for the first-attempt table.
//
// `datawatch config set autonomous.first_attempt.<kind>.backend <value>` and
// `... .model <value>` name one field of one kind's entry. The table is
// stored as a whole, so handlePutConfig folds each dotted key into the
// autonomous.first_attempt object before the shared attempt-cost validator
// runs. The rest of the table keeps its stored values, and a refused value
// is rejected with the same message the whole-table key gives.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

const attemptCostDottedPrefix = "autonomous.first_attempt."

// expandAttemptCostDottedKeys rewrites every autonomous.first_attempt.<kind>.<field>
// key in patch into one autonomous.first_attempt object built over stored (or
// over the whole-table value when the patch also carries one). It returns an
// error naming the key for an unknown field or a value that is not a string;
// an unknown kind is left for ValidateAttemptCostSettings to name.
func expandAttemptCostDottedKeys(patch map[string]interface{}, stored map[string]config.FirstAttemptChoice) error {
	var dotted []string
	for k := range patch {
		if strings.HasPrefix(k, attemptCostDottedPrefix) {
			dotted = append(dotted, k)
		}
	}
	if len(dotted) == 0 {
		return nil
	}
	sort.Strings(dotted)
	table := cloneAttemptChoices(stored)
	if raw, ok := patch["autonomous.first_attempt"]; ok {
		fa, shapeOK := parseFirstAttempt(raw)
		if !shapeOK {
			return fmt.Errorf("autonomous.first_attempt: expected an object mapping a task kind (implementation, docs, note, final_gate) to {backend, model}")
		}
		table = fa
	}
	for _, k := range dotted {
		parts := strings.Split(strings.TrimPrefix(k, attemptCostDottedPrefix), ".")
		if len(parts) != 2 || (parts[1] != "backend" && parts[1] != "model") {
			return fmt.Errorf("%s: unknown key; use autonomous.first_attempt.<kind>.backend or autonomous.first_attempt.<kind>.model", k)
		}
		value, isString := patch[k].(string)
		if !isString {
			return fmt.Errorf("%s: expected a string", k)
		}
		kind := parts[0]
		choice := table[kind]
		if parts[1] == "backend" {
			choice.Backend = value
		} else {
			choice.Model = value
		}
		table[kind] = choice
		delete(patch, k)
	}
	out := make(map[string]interface{}, len(table))
	for kind, c := range table {
		out[kind] = map[string]interface{}{"backend": c.Backend, "model": c.Model}
	}
	patch["autonomous.first_attempt"] = out
	return nil
}
