// Tests for the worker question marker parser in question_marker.go. They are
// table-driven and need no fakes: the parser is a pure function of one line.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const wqValid = `{"question":"Which store?","options":[{"id":"a","label":"Sqlite","tradeoffs":"simple"},{"id":"b","label":"Bolt"}],` +
	`"recommendation":{"option_id":"a","rationale":"already vendored"},"default":"b","blocked":true,` +
	`"label":"design review","risk":"low"}`

func wqLine(payload string) string { return workerQuestionMarker + " " + payload }

func wqOptions(n int) string {
	opts := make([]string, n)
	for i := range opts {
		opts[i] = fmt.Sprintf(`{"id":"o%d","label":"L%d"}`, i, i)
	}
	return "[" + strings.Join(opts, ",") + "]"
}

func TestWorkerQuestionValidLineFillsEveryField(t *testing.T) {
	line := "● " + wqLine(wqValid)
	kind, sig, reason := classifyWorkerQuestion(line)
	if kind != workerQuestionReal || reason != "" {
		t.Fatalf("kind=%v reason=%q, want real", kind, reason)
	}
	sum := sha256.Sum256([]byte(line))
	want := WorkerQuestionSignal{
		Question: "Which store?",
		Options: []WorkerQuestionOption{
			{ID: "a", Label: "Sqlite", Tradeoffs: "simple"},
			{ID: "b", Label: "Bolt"},
		},
		Recommendation: &WorkerQuestionRecommendation{OptionID: "a", Rationale: "already vendored"},
		Default:        "b",
		Blocked:        true,
		Label:          "design review",
		Risk:           "low",
		LineSHA256:     hex.EncodeToString(sum[:]),
	}
	if !reflect.DeepEqual(sig, want) {
		t.Fatalf("signal = %+v, want %+v", sig, want)
	}
}

func TestWorkerQuestionDefaultsApplied(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    func(WorkerQuestionSignal) bool
	}{
		{"absent default becomes hold", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`,
			func(s WorkerQuestionSignal) bool { return s.Default == "hold" }},
		{"absent label becomes decision", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`,
			func(s WorkerQuestionSignal) bool { return s.Label == "decision" }},
		{"absent risk becomes normal", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`,
			func(s WorkerQuestionSignal) bool { return s.Risk == "normal" }},
		{"absent blocked is false", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`,
			func(s WorkerQuestionSignal) bool { return !s.Blocked && s.Recommendation == nil }},
		{"explicit hold is kept", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}],"default":"hold"}`,
			func(s WorkerQuestionSignal) bool { return s.Default == "hold" }},
		{"option default is kept", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}],"default":"a"}`,
			func(s WorkerQuestionSignal) bool { return s.Default == "a" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, sig, reason := classifyWorkerQuestion(wqLine(tc.payload))
			if kind != workerQuestionReal {
				t.Fatalf("kind=%v reason=%q, want real", kind, reason)
			}
			if !tc.want(sig) {
				t.Fatalf("unexpected signal %+v", sig)
			}
		})
	}
}

func TestWorkerQuestionNotRealLines(t *testing.T) {
	tests := []struct {
		name string
		line string
		want workerQuestionKind
	}{
		{"empty line", "", workerQuestionNone},
		{"plain output", "running tests", workerQuestionNone},
		{"marker mid sentence", `When blocked, print DATAWATCH_QUESTION: {"question":"q"} as one line`, workerQuestionInstruction},
		{"marker mid sentence with brace", `Use DATAWATCH_QUESTION: {"question":"q","options":[]}`, workerQuestionInstruction},
		{"placeholder", "DATAWATCH_QUESTION: <json payload>", workerQuestionInstruction},
		{"placeholder with indicator", "● DATAWATCH_QUESTION: <your question>", workerQuestionInstruction},
		{"placeholder after carriage return", "noise\rDATAWATCH_QUESTION: <json>", workerQuestionInstruction},
		{"marker with no payload", "DATAWATCH_QUESTION:", workerQuestionInstruction},
		{"marker with prose", "DATAWATCH_QUESTION: which store?", workerQuestionInstruction},
		{"shell echo", `echo "DATAWATCH_QUESTION: {}"`, workerQuestionInstruction},
		{"needs-input line", "DATAWATCH_NEEDS_INPUT: which store should I use?", workerQuestionNone},
		{"needs-input with indicator", "● DATAWATCH_NEEDS_INPUT: pick one", workerQuestionNone},
		{"needs-input placeholder", "DATAWATCH_NEEDS_INPUT: <your question>", workerQuestionNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, sig, reason := classifyWorkerQuestion(tc.line)
			if kind != tc.want {
				t.Fatalf("kind=%v, want %v", kind, tc.want)
			}
			if reason != "" || !reflect.DeepEqual(sig, WorkerQuestionSignal{}) {
				t.Fatalf("dropped line must carry no signal or reason: reason=%q sig=%+v", reason, sig)
			}
		})
	}
}

func TestWorkerQuestionRecognitionForms(t *testing.T) {
	payload := `{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`
	tests := []struct {
		name string
		line string
	}{
		{"bare", wqLine(payload)},
		{"no space after colon", workerQuestionMarker + payload},
		{"indicator rune", "● " + wqLine(payload)},
		{"ansi wrapped", "\x1b[1m" + wqLine(payload) + "\x1b[0m"},
		{"carriage return segment", "partial redraw\r" + wqLine(payload)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, sig, reason := classifyWorkerQuestion(tc.line)
			if kind != workerQuestionReal || reason != "" {
				t.Fatalf("kind=%v reason=%q, want real", kind, reason)
			}
			if sig.Question != "q" {
				t.Fatalf("question = %q", sig.Question)
			}
		})
	}
}

func TestWorkerQuestionRefusals(t *testing.T) {
	twoOpts := `[{"id":"a","label":"A"},{"id":"b","label":"B"}]`
	tests := []struct {
		name    string
		payload string
		reason  string
	}{
		{"malformed json", `{"question": "q", "options": [`, workerQuestionReasonMalformed},
		{"not json object", `{not json}`, workerQuestionReasonMalformed},
		{"trailing data", `{"question":"q","options":` + twoOpts + `} extra`, workerQuestionReasonMalformed},
		{"two objects", `{"question":"q","options":` + twoOpts + `}{}`, workerQuestionReasonMalformed},
		{"unknown key", `{"question":"q","options":` + twoOpts + `,"bogus":1}`, workerQuestionReasonMalformed},
		{"question wrong type", `{"question":7,"options":` + twoOpts + `}`, workerQuestionReasonMalformed},
		{"blocked wrong type", `{"question":"q","options":` + twoOpts + `,"blocked":"yes"}`, workerQuestionReasonMalformed},
		{"option missing id", `{"question":"q","options":[{"label":"A"},{"id":"b","label":"B"}]}`, workerQuestionReasonMalformed},
		{"option missing label", `{"question":"q","options":[{"id":"a"},{"id":"b","label":"B"}]}`, workerQuestionReasonMalformed},
		{"missing question", `{"options":` + twoOpts + `}`, workerQuestionReasonEmptyQuestion},
		{"empty question", `{"question":"","options":` + twoOpts + `}`, workerQuestionReasonEmptyQuestion},
		{"blank question", `{"question":"   ","options":` + twoOpts + `}`, workerQuestionReasonEmptyQuestion},
		{"no options", `{"question":"q"}`, workerQuestionReasonTooFewOptions},
		{"one option", `{"question":"q","options":[{"id":"a","label":"A"}]}`, workerQuestionReasonTooFewOptions},
		{"duplicate option id", `{"question":"q","options":[{"id":"a","label":"A"},{"id":"a","label":"B"}]}`, workerQuestionReasonDuplicateOption},
		{"unknown label", `{"question":"q","options":` + twoOpts + `,"label":"chitchat"}`, workerQuestionReasonInvalidLabel},
		{"unknown risk", `{"question":"q","options":` + twoOpts + `,"risk":"high"}`, workerQuestionReasonInvalidRisk},
		{"default not an option", `{"question":"q","options":` + twoOpts + `,"default":"z"}`, workerQuestionReasonInvalidDefault},
		{"empty default", `{"question":"q","options":` + twoOpts + `,"default":""}`, workerQuestionReasonInvalidDefault},
		{"recommendation not an option", `{"question":"q","options":` + twoOpts + `,"recommendation":{"option_id":"z"}}`, workerQuestionReasonInvalidRecommended},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, sig, reason := classifyWorkerQuestion(wqLine(tc.payload))
			if kind != workerQuestionRefused {
				t.Fatalf("kind=%v, want refused", kind)
			}
			if reason != tc.reason {
				t.Fatalf("reason=%q, want %q", reason, tc.reason)
			}
			if !reflect.DeepEqual(sig, WorkerQuestionSignal{}) {
				t.Fatalf("refused line must carry no signal: %+v", sig)
			}
		})
	}
}

func TestWorkerQuestionCapsRefused(t *testing.T) {
	twoOpts := `[{"id":"a","label":"A"},{"id":"b","label":"B"}]`
	tests := []struct {
		name    string
		payload string
	}{
		{"question over cap", `{"question":"` + strings.Repeat("q", workerQuestionTextMaxBytes+1) + `","options":` + twoOpts + `}`},
		{"too many options", `{"question":"q","options":` + wqOptions(workerQuestionOptionsMax+1) + `}`},
		{"option label over cap", `{"question":"q","options":[{"id":"a","label":"` + strings.Repeat("l", workerQuestionOptionLabelMaxBytes+1) + `"},{"id":"b","label":"B"}]}`},
		{"tradeoffs over cap", `{"question":"q","options":[{"id":"a","label":"A","tradeoffs":"` + strings.Repeat("t", workerQuestionTradeoffsMaxBytes+1) + `"},{"id":"b","label":"B"}]}`},
		{"rationale over cap", `{"question":"q","options":` + twoOpts + `,"recommendation":{"option_id":"a","rationale":"` + strings.Repeat("r", workerQuestionRationaleMaxBytes+1) + `"}}`},
		{"whole line over cap", `{"question":"q","options":` + twoOpts + `,"default":"` + strings.Repeat("d", workerQuestionLineMaxBytes) + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, _, reason := classifyWorkerQuestion(wqLine(tc.payload))
			if kind != workerQuestionRefused || reason != workerQuestionReasonTooLong {
				t.Fatalf("kind=%v reason=%q, want refused too_long", kind, reason)
			}
		})
	}
}

func TestWorkerQuestionCapsAcceptedAtLimit(t *testing.T) {
	payload := `{"question":"` + strings.Repeat("q", workerQuestionTextMaxBytes) + `","options":` + wqOptions(workerQuestionOptionsMax) + `}`
	if kind, _, reason := classifyWorkerQuestion(wqLine(payload)); kind != workerQuestionReal {
		t.Fatalf("at-cap question: kind=%v reason=%q, want real", kind, reason)
	}
	payload = `{"question":"q","options":[{"id":"a","label":"` + strings.Repeat("l", workerQuestionOptionLabelMaxBytes) +
		`","tradeoffs":"` + strings.Repeat("t", workerQuestionTradeoffsMaxBytes) + `"},{"id":"b","label":"B"}],` +
		`"recommendation":{"option_id":"a","rationale":"` + strings.Repeat("r", workerQuestionRationaleMaxBytes) + `"}}`
	if kind, _, reason := classifyWorkerQuestion(wqLine(payload)); kind != workerQuestionReal {
		t.Fatalf("at-cap fields: kind=%v reason=%q, want real", kind, reason)
	}
}

func TestWorkerQuestionReasonIsNeverLineText(t *testing.T) {
	secret := "hunter2-secret-token"
	line := wqLine(`{"question":"` + secret + `","options":[{"id":"a","label":"A"}]}`)
	kind, _, reason := classifyWorkerQuestion(line)
	if kind != workerQuestionRefused {
		t.Fatalf("kind=%v, want refused", kind)
	}
	if strings.Contains(reason, secret) || strings.ContainsAny(reason, " {}\"") {
		t.Fatalf("reason %q must be a bare category", reason)
	}
}

func TestWorkerQuestionLineHashIsOfExactBytes(t *testing.T) {
	a := wqLine(`{"question":"q","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`)
	b := a + " "
	_, sa, _ := classifyWorkerQuestion(a)
	_, sb, _ := classifyWorkerQuestion(b)
	sum := sha256.Sum256([]byte(a))
	if sa.LineSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash = %q, want sha256 of the line", sa.LineSHA256)
	}
	if sa.LineSHA256 == sb.LineSHA256 {
		t.Fatal("a different byte sequence must hash differently")
	}
}
