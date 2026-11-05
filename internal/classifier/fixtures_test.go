package classifier_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/classifier"
)

// strictParse mirrors the verifier contract parse in cmd/datawatch
// (extractJSON, then json.Unmarshal into VerificationResult). That function is
// in package main and cannot be imported.
func strictParse(s string) error {
	s = strings.TrimSpace(s)
	if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	var v autonomous.VerificationResult
	return json.Unmarshal([]byte(s), &v)
}

func TestFixturesAreLabelledSynthetic(t *testing.T) {
	for _, name := range []string{classifier.FixturePrompt, classifier.FixtureUnparseableReply, classifier.FixtureSystemOne, classifier.FixtureLogprobs} {
		var m map[string]any
		if err := classifier.LoadFixture(name, &m); err != nil {
			t.Fatal(err)
		}
		if l, _ := m["_fixture"].(string); !strings.HasPrefix(l, "SYNTHETIC") {
			t.Errorf("%s: missing SYNTHETIC label", name)
		}
	}
	if err := classifier.LoadFixture("missing.json", &struct{}{}); err == nil {
		t.Error("missing fixture must error")
	}
}

func TestLoadFixtureBadJSON(t *testing.T) {
	if err := classifier.LoadFixture(classifier.FixturePrompt, &[]int{}); err == nil {
		t.Error("decoding into the wrong type must error")
	}
}

// The unparseable reply fails the strict verifier contract. The existing
// behaviour (pinned in cmd/datawatch/bl410_failure_classification_test.go) is
// that a written-but-unparseable verifier reply is the terminal
// "invalid-response" category, NOT "infra"; ClassifyInfraText does not treat
// it as an infra-retryable failure either. Only the classifier's own failures
// are infra (see FailureCategory).
func TestReplayUnparseableReplyExistingBehaviour(t *testing.T) {
	var r classifier.VerifierReply
	if err := classifier.LoadFixture(classifier.FixtureUnparseableReply, &r); err != nil {
		t.Fatal(err)
	}
	if err := strictParse(r.Reply); err == nil {
		t.Fatal("fixture reply must not parse under the strict verifier contract")
	}
	if class, ok := autonomous.ClassifyInfraText(r.Reply); ok {
		t.Errorf("unparseable reply text classified as infra class %q; existing parser leaves it unclassified", class)
	}
	src, err := os.ReadFile("../../cmd/datawatch/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"invalid-response"`) || !strings.Contains(string(src), "its response could not be parsed as a verdict: %v") {
		t.Error("main.go no longer stamps unparseable verifier replies as invalid-response; revisit this replay test")
	}
}

func TestReplayNativeAndLogprobsAgree(t *testing.T) {
	var p classifier.Prompt
	var n classifier.SystemOneResponse
	var l classifier.LogprobsResponse
	for name, v := range map[string]any{classifier.FixturePrompt: &p, classifier.FixtureSystemOne: &n, classifier.FixtureLogprobs: &l} {
		if err := classifier.LoadFixture(name, v); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.HasSuffix(p.Prompt, "Answer:") {
		t.Error("prompt must end with the option set")
	}
	a := n.Questions["implements"]
	if a.Choice != "A" || a.Score == nil || n.Usage.OutputTokens != 0 {
		t.Errorf("native fixture shape wrong: %+v usage=%+v", a, n.Usage)
	}
	sum := 0.0
	for _, pr := range a.Probabilities {
		sum += pr
	}
	if sum < 0.999 || sum > 1.001 {
		t.Errorf("probabilities sum %v", sum)
	}
	if !l.HasLogprobs() {
		t.Fatal("logprobs fixture has no logprobs")
	}
	top := l.Choices[0].Logprobs.Content[0].TopLogprobs
	if top[0].Token != a.Choice {
		t.Errorf("logprobs argmax %q != native choice %q", top[0].Token, a.Choice)
	}
}

func TestHasLogprobsFalseCases(t *testing.T) {
	var r classifier.LogprobsResponse
	if r.HasLogprobs() {
		t.Error("empty response")
	}
	r.Choices = []classifier.LogprobsChoice{{}}
	if r.HasLogprobs() {
		t.Error("nil logprobs")
	}
}
