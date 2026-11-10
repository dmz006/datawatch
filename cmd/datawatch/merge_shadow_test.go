package main

import (
	"net/http"
	"strings"
	"testing"
)

const mergeShadowCLIBody = `{"projects":[{"project":"demo","plans":[],"comparisons":[` +
	`{"automaton_id":"a1","release_size":"patch","matched":4,"different":1,"not_compared":0,"differences":[{"field":"version","planned":"1.0.1","landed":"1.0.2","severity":"high"}]},` +
	`{"automaton_id":"a2","release_size":"minor","matched":5,"different":0,"not_compared":1,"differences":[]}],` +
	`"counts":{"patch":{"matched":0,"different":1},"minor":{"matched":1,"different":0},"major":{"matched":0,"different":0},"none":{"matched":0,"different":0}}}]}`

func TestMergeShadowCLI_Status(t *testing.T) {
	d := newLaneFillCLIDaemon(t, http.StatusOK, mergeShadowCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"status"})
	if err != nil {
		t.Fatal(err)
	}
	if d.path != "/api/autonomous/merge/shadow" {
		t.Errorf("path=%s", d.path)
	}
	for _, w := range []string{"patch: matched 0, different 1\n", "minor: matched 1, different 0\n", "none: matched 0, different 0\n"} {
		if !strings.Contains(out, w) {
			t.Errorf("lacks %q:\n%s", w, out)
		}
	}
}

func TestMergeShadowCLI_StatusNone(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, `{"projects":[]}`)
	out, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"status"})
	if err != nil || out != mergeShadowNoComparisonLine+"\n" {
		t.Errorf("err=%v out=%q", err, out)
	}
}

func TestMergeShadowCLI_Compare(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, mergeShadowCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"compare", "a1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"Automaton a1 (patch): matched 4, different 1", `version [high]: planned "1.0.1", landed "1.0.2"`} {
		if !strings.Contains(out, w) {
			t.Errorf("lacks %q:\n%s", w, out)
		}
	}
}

func TestMergeShadowCLI_CompareMissingAndEmpty(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusOK, mergeShadowCLIBody)
	out, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"compare", "zzz"})
	if err != nil || strings.Count(out, "\n") != 1 || !strings.Contains(out, "no merge shadow comparison exists for Automaton zzz") {
		t.Errorf("err=%v out=%q", err, out)
	}
	out, _ = runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"compare", "a2"})
	if !strings.Contains(out, "no differences") {
		t.Errorf("out=%q", out)
	}
}

func TestMergeShadowCLI_DaemonError(t *testing.T) {
	newLaneFillCLIDaemon(t, http.StatusInternalServerError, "boom")
	_, err := runLaneFillCmd(t, newAutonomousMergeShadowCmd(), []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err=%v", err)
	}
}
