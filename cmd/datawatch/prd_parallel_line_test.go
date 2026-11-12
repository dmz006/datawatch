package main

import "testing"

func TestPRDParallelLine(t *testing.T) {
	cases := map[string]string{
		`{"total_tasks":4,"read_only_tasks":2,"speedup":2}`: "parallel: 4 tasks, 2 read only, speedup 2.00x",
		`{"total_tasks":0}`: "",
		`{}`:                "",
		`not json`:          "",
	}
	for in, want := range cases {
		if got := prdParallelLine([]byte(in)); got != want {
			t.Errorf("prdParallelLine(%s) = %q, want %q", in, got, want)
		}
	}
}
