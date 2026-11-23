// Plain-text rendering of the dormant features registry, shared by the CLI
// (`datawatch autonomous dormant-features`) and the chat verb. Read-only.

package autonomous

import (
	"encoding/json"
	"io"
	"strconv"
	"text/tabwriter"
)

// DormantFeaturesBody is the body of GET /api/autonomous/dormant-features.
type DormantFeaturesBody struct {
	Features []DormantFeature `json:"features"`
}

// ParseDormantFeatures decodes the dormant features body. A missing features
// list reads as an empty list.
func ParseDormantFeatures(body []byte) (*DormantFeaturesBody, error) {
	var b DormantFeaturesBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, err
	}
	if b.Features == nil {
		b.Features = []DormantFeature{}
	}
	return &b, nil
}

// RenderDormantFeatures writes the registry as a plain table: key, state,
// default, purpose and whether the feature was ever exercised.
func RenderDormantFeatures(w io.Writer, features []DormantFeature) error {
	if len(features) == 0 {
		_, err := io.WriteString(w, "No Automata setting is off or in shadow mode.\n")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = io.WriteString(tw, "KEY\tSTATE\tDEFAULT\tPURPOSE\tEVER EXERCISED\n")
	for _, f := range features {
		ever := "no"
		if f.EverExercised {
			ever = "yes"
			if f.LastExercised != "" {
				ever += " (last " + f.LastExercised + ")"
			}
		}
		_, _ = io.WriteString(tw, f.Key+"\t"+f.State+"\t"+f.Default+"\t"+f.Purpose+"\t"+ever+"\n")
	}
	return tw.Flush()
}

// RenderHousekeepingRepairsBody writes the repairs read view followed by the
// refusal ledger and the finished-work findings, shared by the CLI and the
// chat verb.
func RenderHousekeepingRepairsBody(b io.StringWriter, body *HousekeepingRepairsReadBody) {
	RenderHousekeepingRepairs(b, &body.HousekeepingRepairsView)
	w := func(s string) { _, _ = b.WriteString(s) }
	w("Refusals (" + strconv.Itoa(len(body.Refusals)) + ")\n")
	for _, e := range body.Refusals {
		w("  " + e.Automaton + " " + e.Step + ": " + e.Reason + " (repeats " + strconv.Itoa(e.Repeats) +
			", first " + e.FirstSeen + ", last " + e.LastSeen + ")\n")
	}
	w("Finished work (" + strconv.Itoa(len(body.FinishedWork)) + ")\n")
	for _, f := range body.FinishedWork {
		w("  " + f.Automaton + " " + f.Title + " — status " + f.Status + "; cause: " + f.Cause +
			"; action: " + f.Action + " (" + f.At + ")\n")
	}
}

// HousekeepingRepairsReadBody is the full body of
// GET /api/autonomous/housekeeping-repairs: the repair view plus the refusal
// ledger and the finished-work findings.
type HousekeepingRepairsReadBody struct {
	HousekeepingRepairsView
	Refusals     []RefusalEntry        `json:"refusals"`
	FinishedWork []FinishedWorkFinding `json:"finished_work"`
}

// ParseHousekeepingRepairsBody decodes the full housekeeping repairs body.
// Missing refusals or finished_work lists read as empty lists.
func ParseHousekeepingRepairsBody(body []byte) (*HousekeepingRepairsReadBody, error) {
	var b HousekeepingRepairsReadBody
	if err := json.Unmarshal(body, &b); err != nil {
		return nil, err
	}
	if b.Refusals == nil {
		b.Refusals = []RefusalEntry{}
	}
	if b.FinishedWork == nil {
		b.FinishedWork = []FinishedWorkFinding{}
	}
	return &b, nil
}
