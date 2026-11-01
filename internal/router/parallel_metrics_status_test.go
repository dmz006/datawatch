package router

import "testing"

func TestParallelMetricsStatusLine(t *testing.T) {
	cases := map[string]string{
		`{"total_tasks":4,"read_only_tasks":1,"speedup":1.5}`: "parallel: 4 tasks, 1 read only, speedup 1.50x",
		`{"total_tasks":0}`: "",
		`{}`:                "",
		`bad`:               "",
	}
	for in, want := range cases {
		if got := parallelMetricsStatusLine(in); got != want {
			t.Errorf("parallelMetricsStatusLine(%s) = %q, want %q", in, got, want)
		}
	}
}
