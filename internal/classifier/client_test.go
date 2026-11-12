package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type stubGate struct {
	err      error
	acquired int
	released int
}

func (g *stubGate) Acquire(ctx context.Context, node, model string) (func(), error) {
	if g.err != nil {
		return nil, g.err
	}
	g.acquired++
	return func() { g.released++ }, nil
}

func fixtureBody(t *testing.T, name string) []byte {
	t.Helper()
	b, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestClient(t *testing.T, h http.HandlerFunc, cfg ClientConfig) (*Client, *stubGate, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	g := &stubGate{}
	c := NewClient(srv.Client(), func(string) (string, error) { return srv.URL, nil }, g, nil, cfg)
	return c, g, srv
}

func fixtureReq(t *testing.T, mode Path) Request {
	t.Helper()
	var p Prompt
	if err := LoadFixture(FixturePrompt, &p); err != nil {
		t.Fatal(err)
	}
	return Request{Node: "n1", Model: "example-chat-model", Mode: mode, Stem: "Does the diff implement the spec?",
		Options: []string{"the diff implements the spec", "the diff does not implement the spec"}}
}

func TestBuildPrompt(t *testing.T) {
	got := BuildPrompt("Q?\n", []string{"yes", "no", "maybe"})
	want := "Q?\nA) yes\nB) no\nC) maybe\nAnswer:"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestNativeFixture(t *testing.T) {
	var gotReq SystemOneRequest
	c, g, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		// Re-key the fixture's question to the client's question name.
		var fx SystemOneResponse
		_ = json.Unmarshal(fixtureBody(t, FixtureSystemOne), &fx)
		out := SystemOneResponse{Model: fx.Model, Questions: map[string]SystemOneAnswer{nativeQuestion: fx.Questions["implements"]}}
		_ = json.NewEncoder(w).Encode(out)
	}, ClientConfig{})
	res, err := c.Classify(context.Background(), fixtureReq(t, PathNative))
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen != "A" || res.Mode != PathNative || res.Route != (Route{"n1", "example-chat-model"}) {
		t.Fatalf("%+v", res)
	}
	if math.Abs(res.Probabilities["A"]-0.9312) > 1e-9 || math.Abs(res.Probabilities["B"]-0.0688) > 1e-9 {
		t.Fatalf("%v", res.Probabilities)
	}
	if len(gotReq.Questions) != 1 || strings.Join(gotReq.Questions[0].Options, "") != "AB" || !strings.HasSuffix(gotReq.Prompt, "Answer:") {
		t.Fatalf("request %+v", gotReq)
	}
	if g.acquired != 1 || g.released != 1 {
		t.Fatalf("gate %+v", g)
	}
}

func TestNativeFailures(t *testing.T) {
	cases := map[string]string{
		"no question":     `{"questions":{}}`,
		"missing option":  `{"questions":{"verdict":{"choice":"A","probabilities":{"A":0.9}}}}`,
		"negative":        `{"questions":{"verdict":{"choice":"A","probabilities":{"A":-1,"B":2}}}}`,
		"bad json":        `{`,
		"abstain margin":  `{"questions":{"verdict":{"choice":"A","probabilities":{"A":0.52,"B":0.48}}}}`,
		"abstain low top": `{"questions":{"verdict":{"choice":"A","probabilities":{"A":0.3,"B":0.1}}}}`,
	}
	want := map[string]InfraKind{"abstain margin": KindAbstain, "abstain low top": KindAbstain}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }, ClientConfig{})
			_, err := c.Classify(context.Background(), fixtureReq(t, PathNative))
			k, ok := IsInfraError(err)
			if !ok {
				t.Fatalf("err %v", err)
			}
			w := want[name]
			if w == "" {
				w = KindMissingScores
			}
			if k != w {
				t.Fatalf("kind %s want %s", k, w)
			}
			if FailureCategory(err) != "infra" {
				t.Fatal("category")
			}
		})
	}
}

func TestGenericFixtureAndRequestShape(t *testing.T) {
	var body map[string]any
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write(fixtureBody(t, FixtureLogprobs))
	}, ClientConfig{})
	req := fixtureReq(t, PathLogprobs)
	req.Model = "Qwen3-8B-Q4"
	res, err := c.Classify(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen != "A" || res.Mode != PathLogprobs {
		t.Fatalf("%+v", res)
	}
	pa, pb := math.Exp(-0.0712), math.Exp(-2.6741)
	if math.Abs(res.Probabilities["A"]-pa/(pa+pb)) > 1e-9 {
		t.Fatalf("%v", res.Probabilities)
	}
	if body["logprobs"] != true || body["max_tokens"].(float64) != 1 || body["top_logprobs"].(float64) != DefaultTopLogprobs {
		t.Fatalf("body %v", body)
	}
	kw, _ := body["chat_template_kwargs"].(map[string]any)
	if kw["enable_thinking"] != false {
		t.Fatalf("thinking not disabled: %v", body)
	}
}

func TestGenericNoThinkingKwargForOtherModels(t *testing.T) {
	var body map[string]any
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write(fixtureBody(t, FixtureLogprobs))
	}, ClientConfig{})
	if _, err := c.Classify(context.Background(), fixtureReq(t, PathLogprobs)); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["chat_template_kwargs"]; ok {
		t.Fatalf("unexpected kwargs %v", body)
	}
}

func genericResp(tokens ...TokenLogprob) string {
	r := map[string]any{"choices": []map[string]any{{"logprobs": map[string]any{
		"content": []map[string]any{{"token": "x", "logprob": 0, "top_logprobs": tokens}}}}}}
	b, _ := json.Marshal(r)
	return string(b)
}

func TestGenericAliasesAreSummed(t *testing.T) {
	// A appears as "A", " A" and "a." (0.3 each = 0.9 total); B once at 0.05.
	body := genericResp(
		TokenLogprob{Token: "A", Logprob: math.Log(0.3)},
		TokenLogprob{Token: " A", Logprob: math.Log(0.3)},
		TokenLogprob{Token: "▁a.", Logprob: math.Log(0.3)},
		TokenLogprob{Token: "B", Logprob: math.Log(0.05)},
		TokenLogprob{Token: "the", Logprob: math.Log(0.05)},
	)
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }, ClientConfig{})
	res, err := c.Classify(context.Background(), fixtureReq(t, PathLogprobs))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(res.Probabilities["A"]-0.9/0.95) > 1e-9 {
		t.Fatalf("%v", res.Probabilities)
	}
}

func TestGenericFailures(t *testing.T) {
	cases := map[string]struct {
		body string
		kind InfraKind
	}{
		"no logprobs":   {`{"choices":[{"message":{"content":"A"}}]}`, KindMissingScores},
		"bad json":      {`nope`, KindMissingScores},
		"no option tok": {genericResp(TokenLogprob{Token: "the", Logprob: -0.1}), KindMissingScores},
		"low mass":      {genericResp(TokenLogprob{Token: "A", Logprob: math.Log(0.2)}, TokenLogprob{Token: "the", Logprob: math.Log(0.7)}), KindMissingScores},
		"abstain":       {genericResp(TokenLogprob{Token: "A", Logprob: math.Log(0.5)}, TokenLogprob{Token: "B", Logprob: math.Log(0.45)}), KindAbstain},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.body) }, ClientConfig{})
			_, err := c.Classify(context.Background(), fixtureReq(t, PathLogprobs))
			if k, ok := IsInfraError(err); !ok || k != tc.kind {
				t.Fatalf("err %v", err)
			}
		})
	}
}

func TestTransportStatusAndTimeout(t *testing.T) {
	c, _, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, ClientConfig{})
	_, err := c.Classify(context.Background(), fixtureReq(t, PathNative))
	if k, _ := IsInfraError(err); k != KindTransport {
		t.Fatalf("err %v", err)
	}

	done := make(chan struct{})
	c, _, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { <-done }, ClientConfig{Timeout: 50 * time.Millisecond})
	defer close(done)
	_, err = c.Classify(context.Background(), fixtureReq(t, PathNative))
	if k, _ := IsInfraError(err); k != KindTimeout {
		t.Fatalf("err %v", err)
	}
	srv.CloseClientConnections()

	// A dead server is a transport error.
	c2 := NewClient(nil, func(string) (string, error) { return "http://127.0.0.1:1", nil }, &stubGate{}, nil, ClientConfig{Timeout: time.Second})
	_, err = c2.Classify(context.Background(), fixtureReq(t, PathNative))
	if k, ok := IsInfraError(err); !ok || (k != KindTransport && k != KindTimeout) {
		t.Fatalf("err %v", err)
	}
	// Unresolvable node.
	c3 := NewClient(nil, func(string) (string, error) { return "", errors.New("no node") }, &stubGate{}, nil, ClientConfig{})
	_, err = c3.Classify(context.Background(), fixtureReq(t, PathNative))
	if k, _ := IsInfraError(err); k != KindTransport {
		t.Fatalf("err %v", err)
	}
}

func TestBadRequestsAndGate(t *testing.T) {
	c, g, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("must not call") }, ClientConfig{})
	for _, r := range []Request{
		{Node: "n", Model: "m", Mode: PathNative, Stem: "q", Options: []string{"only"}},
		{Node: "n", Model: "m", Mode: PathNative, Stem: " ", Options: []string{"a", "b"}},
		{Node: "n", Model: "m", Mode: "weird", Stem: "q", Options: []string{"a", "b"}},
	} {
		_, err := c.Classify(context.Background(), r)
		if k, _ := IsInfraError(err); k != KindBadRequest {
			t.Fatalf("err %v", err)
		}
	}
	g.err = errors.New("pool full")
	_, err := c.Classify(context.Background(), fixtureReq(t, PathNative))
	var ge *GateError
	if !errors.As(err, &ge) || FailureCategory(err) != "infra" {
		t.Fatalf("err %v", err)
	}
	nogate := NewClient(nil, func(string) (string, error) { return "http://x", nil }, nil, nil, ClientConfig{})
	if _, err := nogate.Classify(context.Background(), fixtureReq(t, PathNative)); !errors.As(err, &ge) {
		t.Fatalf("err %v", err)
	}
}

func TestCapabilityRequiredBeforeCall(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(404) // probe says unsupported
	}))
	defer srv.Close()
	base := func(string) (string, error) { return srv.URL, nil }
	p := NewProber(srv.Client(), base, Config{}, nil)
	c := NewClient(srv.Client(), base, &stubGate{}, p, ClientConfig{})
	_, err := c.Classify(context.Background(), fixtureReq(t, PathNative))
	if !IsCapabilityError(err) || FailureCategory(err) != "infra" {
		t.Fatalf("err %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls %d: only the probe should have run", calls)
	}
}

func TestRotationAveragesAndMapsBack(t *testing.T) {
	// A biased server always puts 0.7 on letter A and 0.2 on B, regardless of
	// content. With rotation on, original options average out to 0.45 each, so
	// the call abstains instead of returning a position artefact.
	calls := 0
	h := func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, genericResp(TokenLogprob{Token: "A", Logprob: math.Log(0.7)}, TokenLogprob{Token: "B", Logprob: math.Log(0.2)}))
	}
	c, _, _ := newTestClient(t, h, ClientConfig{RotateOptions: true})
	_, err := c.Classify(context.Background(), fixtureReq(t, PathLogprobs))
	if k, _ := IsInfraError(err); k != KindAbstain {
		t.Fatalf("err %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}

	// A content-following server: the text "implements the spec" gets 0.9.
	h = func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		lines := strings.Split(b.Messages[0].Content, "\n")
		var a, bb float64
		if strings.HasPrefix(lines[1], "A) the diff implements") {
			a, bb = 0.9, 0.05
		} else {
			a, bb = 0.05, 0.9
		}
		_, _ = io.WriteString(w, genericResp(TokenLogprob{Token: "A", Logprob: math.Log(a)}, TokenLogprob{Token: "B", Logprob: math.Log(bb)}))
	}
	c, _, _ = newTestClient(t, h, ClientConfig{RotateOptions: true})
	res, err := c.Classify(context.Background(), fixtureReq(t, PathLogprobs))
	if err != nil {
		t.Fatal(err)
	}
	if res.Chosen != "A" || math.Abs(res.Probabilities["A"]-0.9/0.95) > 1e-9 {
		t.Fatalf("%+v", res)
	}
	// Rotation is off by default.
	if (ClientConfig{}).withDefaults().RotateOptions {
		t.Fatal("rotation must default off")
	}
}

func TestNormalizeTokenAndRank(t *testing.T) {
	for in, want := range map[string]string{"A": "A", " b": "B", "(C)": "C", "Ġd": "D", "AB": "", "": "", "1.": "1"} {
		if got := NormalizeToken(in); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	best, top, second := rank(map[string]float64{"A": 0.4, "B": 0.4, "C": 0.2})
	if best != "A" || top != 0.4 || second != 0.4 {
		t.Fatalf("%s %v %v", best, top, second)
	}
	e := &InfraError{Kind: KindAbstain, Node: "n", Model: "m"}
	if !strings.Contains(e.Error(), "abstain") || errors.Unwrap(e) != nil {
		t.Fatal(e)
	}
	if _, ok := IsInfraError(fmt.Errorf("x")); ok {
		t.Fatal("not infra")
	}
	if got := (ClientConfig{}).withDefaults(); got.Timeout != DefaultClientTimeout || got.AbstainMargin != DefaultAbstainMargin ||
		got.MinProbability != DefaultMinProbability || got.MinOptionMass != DefaultMinOptionMass || got.TopLogprobs != DefaultTopLogprobs {
		t.Fatalf("%+v", got)
	}
}
