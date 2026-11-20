package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestSupervisorSetupCmdTree(t *testing.T) {
	c := newSupervisorSetupCmd()
	for _, path := range [][]string{{"run"}, {"state", "get"}, {"state", "set"}, {"state", "change"}, {"state", "remove"}, {"correction", "list"}, {"correction", "record"}, {"correction", "promote"}} {
		sub, _, err := c.Find(path)
		if err != nil || sub == nil || sub.Name() != path[len(path)-1] {
			t.Errorf("missing subcommand %v: %v", path, err)
		}
	}
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs([]string{"run", "--help"})
	if err := c.Execute(); err != nil || !strings.Contains(out.String(), "--schedule") {
		t.Fatalf("help: %v %s", err, out.String())
	}
}
