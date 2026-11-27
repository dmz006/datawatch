package delivery

import "github.com/dmz006/datawatch/internal/pipeline"

// PipelineCandidates reports interrupted and failed pipelines. A pipeline is
// not resumed.
func PipelineCandidates(runs []pipeline.RunRecord) []Candidate {
	var out []Candidate
	for _, r := range runs {
		how := ""
		switch {
		case r.Interrupted:
			how = "interrupted"
		case r.State == pipeline.StateFailed:
			how = "failed"
		default:
			continue
		}
		// The ID alone can repeat across restarts, so the start time is part.
		out = append(out, Candidate{Kind: KindPipeline, ID: r.ID + "@" + r.StartedAt.UTC().Format("20060102T150405"),
			State: how, Since: r.StartedAt, Problem: how, Invariant: InvFailed, Sticky: true, WaitsOn: WaitsOnOperator})
	}
	return out
}
