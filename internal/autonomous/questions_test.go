package autonomous

import (
	"errors"
	"strings"
	"testing"
	"unicode"
)

// sanity: DefaultQuestionLabels is exported and stable.
var _ = DefaultQuestionLabels

// baseQuestion returns a valid minimal question so individual tests flip
// exactly one rule at a time.
func baseQuestion() *Question {
	return &Question{
		ID:        "q1",
		Context:   QuestionContextNone,
		Text:      "Which release approach should we take?",
		Options:   []QuestionOption{{ID: "a", Label: "Ship now"}, {ID: "b", Label: "Wait"}},
		Default:   "a",
		CreatedAt: nil,
	}
}

func TestQuestionValidation_New(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(q *Question)
		check      func(t *testing.T, q *Question)
		noQuestion bool
		wantErr    bool
		wantMsg    string
	}{
		{name: "valid minimal"},
		{
			name: "empty label defaults to decision",
			check: func(t *testing.T, q *Question) {
				if q.Label != "decision" {
					t.Fatalf("label = %q, want decision", q.Label)
				}
			},
		},
		{name: "custom label accepted", mutate: setLabel("release plan")},
		{name: "custom label at 64 characters accepted", mutate: setLabel(strings.Repeat("x", 64))},
		{name: "label over 64 characters rejected", mutate: setLabel(strings.Repeat("x", 65)), wantErr: true, wantMsg: "label is 65 characters"},
		{name: "label with control character rejected", mutate: setLabel("line\nbreak"), wantErr: true, wantMsg: "control characters"},
		{name: "empty risk defaults to normal", check: func(t *testing.T, q *Question) {
			if q.Risk != QuestionRiskNormal {
				t.Fatalf("risk = %q, want normal", q.Risk)
			}
		}},
		{name: "risk low accepted", mutate: setRisk("low"), check: func(t *testing.T, q *Question) {
			if q.Risk != QuestionRiskLow {
				t.Fatalf("risk = %q, want low", q.Risk)
			}
		}},
		{name: "risk unknown rejected", mutate: setRisk("urgent"), wantErr: true, wantMsg: `risk is "urgent"`},
		{name: "empty text rejected", mutate: func(q *Question) { q.Text = "" }, wantErr: true, wantMsg: "text is required"},
		{name: "whitespace text rejected", mutate: func(q *Question) { q.Text = "   " }, wantErr: true, wantMsg: "text is required"},
		{name: "text at cap accepted", mutate: setText(strings.Repeat("x", questionTextMaxBytes))},
		{name: "text over cap rejected", mutate: setText(strings.Repeat("x", questionTextMaxBytes+1)), wantErr: true, wantMsg: "text is 8193 bytes"},
		{name: "one option rejected", mutate: func(q *Question) { q.Options = q.Options[:1] }, wantErr: true, wantMsg: "at least two options are required"},
		{name: "twenty options accepted", mutate: func(q *Question) { q.Options = twentyOptions(); q.Default = "a" }},
		{name: "twenty one options rejected", mutate: func(q *Question) {
			q.Options = append(twentyOptions(), QuestionOption{ID: "z", Label: "Z"})
			q.Default = "a"
		}, wantErr: true, wantMsg: "exceed the cap"},
		{name: "duplicated option id rejected", mutate: func(q *Question) { q.Options[1].ID = "a" }, wantErr: true, wantMsg: "option id \"a\" is repeated"},
		{name: "empty option id rejected", mutate: func(q *Question) { q.Options[0].ID = "" }, wantErr: true, wantMsg: "empty id"},
		{name: "duplicated option label rejected", mutate: func(q *Question) { q.Options[1].Label = "Ship now" }, wantErr: true, wantMsg: "option label \"Ship now\" is repeated"},
		{name: "empty option label rejected", mutate: func(q *Question) { q.Options[1].Label = "" }, wantErr: true, wantMsg: "empty label"},
		{name: "option label over cap rejected", mutate: func(q *Question) { q.Options[0].Label = strings.Repeat("y", questionOptionLabelMaxBytes+1) }, wantErr: true, wantMsg: "option label is 201 bytes"},
		{name: "tradeoffs over cap rejected", mutate: func(q *Question) { q.Options[0].Tradeoffs = strings.Repeat("t", questionTradeoffsMaxBytes+1) }, wantErr: true, wantMsg: "tradeoffs are 1001 bytes"},
		{name: "empty default rejected", mutate: func(q *Question) { q.Default = "" }, wantErr: true, wantMsg: "default is required"},
		{name: "unknown default option rejected", mutate: func(q *Question) { q.Default = "nope" }, wantErr: true, wantMsg: `default "nope"`},
		{name: "hold is a valid default", mutate: func(q *Question) { q.Default = "hold" }},
		{name: "recommendation to existing option accepted", mutate: func(q *Question) { q.Recommendation = &QuestionRecommendation{OptionID: "b"} }},
		{name: "recommendation to unknown option rejected", mutate: func(q *Question) { q.Recommendation = &QuestionRecommendation{OptionID: "nope"} }, wantErr: true, wantMsg: `option "nope"`},
		{name: "recommendation rationale over cap rejected", mutate: func(q *Question) {
			q.Recommendation = &QuestionRecommendation{OptionID: "a", Rationale: strings.Repeat("r", questionRationaleMaxBytes+1)}
		}, wantErr: true, wantMsg: "rationale is 1001 bytes"},
		{name: "context prd with prd id accepted"},
		{name: "context prd without prd id rejected", mutate: func(q *Question) { q.Context = QuestionContextPrd; q.PRDID = "" }, wantErr: true, wantMsg: "context prd needs prd_id"},
		{name: "context task with task id accepted", mutate: func(q *Question) { q.Context = QuestionContextTask; q.TaskID = "t1" }},
		{name: "context task without task id rejected", mutate: func(q *Question) { q.Context = QuestionContextTask }, wantErr: true, wantMsg: "context task needs task_id"},
		{name: "context proposal with path accepted", mutate: func(q *Question) { q.Context = QuestionContextProposal; q.ProposalPath = "docs/plans/plan.md" }},
		{name: "context proposal without path rejected", mutate: func(q *Question) { q.Context = QuestionContextProposal }, wantErr: true, wantMsg: "context proposal needs proposal_path"},
		{name: "context registry row with key accepted", mutate: func(q *Question) { q.Context = QuestionContextRegistryRow; q.RegistryRowKey = "key" }},
		{name: "context registry row without key rejected", mutate: func(q *Question) { q.Context = QuestionContextRegistryRow }, wantErr: true, wantMsg: "context registry_row needs registry_row_key"},
		{name: "context none with prd id rejected", mutate: func(q *Question) { q.PRDID = "p1" }, wantErr: true, wantMsg: "context none must not carry links"},
		{name: "context none with task id rejected", mutate: func(q *Question) { q.TaskID = "t1" }, wantErr: true, wantMsg: "context none must not carry links"},
		{name: "context none with proposal path rejected", mutate: func(q *Question) { q.ProposalPath = "docs/plans/plan.md" }, wantErr: true, wantMsg: "context none must not carry links"},
		{name: "context unknown rejected", mutate: func(q *Question) { q.Context = "somewhere" }, wantErr: true, wantMsg: `context is "somewhere"`},
		{name: "relative proposal path accepted", mutate: func(q *Question) { q.Context = QuestionContextProposal; q.ProposalPath = "docs/plans/plan.md" }},
		{name: "nested relative proposal path accepted", mutate: func(q *Question) { q.Context = QuestionContextProposal; q.ProposalPath = "a/b/c.md" }},
		{name: "absolute proposal path rejected", mutate: func(q *Question) { q.ProposalPath = "/etc/passwd" }, wantErr: true, wantMsg: "must be repository-relative"},
		{name: "proposal path with dotdot segment rejected", mutate: func(q *Question) { q.ProposalPath = "docs/../../secrets.md" }, wantErr: true, wantMsg: "'..' segment"},
		{name: "proposal path with safe dot segment accepted", mutate: func(q *Question) { q.Context = QuestionContextProposal; q.ProposalPath = "docs/.plans/x.md" }},
		{name: "state in input is ignored", mutate: func(q *Question) { q.State = QuestionWithdrawn }},
		{name: "nil question rejected", wantErr: true, wantMsg: "question is nil", noQuestion: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var q *Question
			if !tc.noQuestion {
				q = baseQuestion()
				if tc.mutate != nil {
					tc.mutate(q)
				}
			}
			err := ValidateNewQuestion(q)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ValidateNewQuestion() = nil, want error %s", tc.wantMsg)
				}
				if !errors.Is(err, ErrInvalidQuestion) {
					t.Fatalf("error %v does not wrap ErrInvalidQuestion", err)
				}
				if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
					t.Fatalf("error %q does not contain %q", err, tc.wantMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateNewQuestion() = %v, want nil", err)
			}
			if tc.check != nil {
				tc.check(t, q)
			}
		})
	}
}

func setLabel(label string) func(*Question) {
	return func(q *Question) { q.Label = label }
}

func setRisk(risk string) func(*Question) {
	return func(q *Question) { q.Risk = risk }
}

func setText(text string) func(*Question) {
	return func(q *Question) { q.Text = text }
}

func twentyOptions() []QuestionOption {
	var opts []QuestionOption
	for i := 0; i < questionOptionsMax; i++ {
		opts = append(opts, QuestionOption{ID: string(rune('a' + i)), Label: "Option " + string(rune('a'+i))})
	}
	return opts
}

func TestQuestionValidation_NewLabelNormalization(t *testing.T) {
	// Control characters are rejected regardless of position.
	for _, bad := range []string{"a\vb", "tab\there", "\x00one"} {
		var found bool
		for _, r := range bad {
			if unicode.IsControl(r) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("test helper: %q has no control character", bad)
		}
		q := baseQuestion()
		q.Label = bad
		if !errors.Is(ValidateNewQuestion(q), ErrInvalidQuestion) {
			t.Fatalf("label %q: want ErrInvalidQuestion, got nil", bad)
		}
	}
	// Non-ASCII labels are allowed up to the 64-character cap.
	q := baseQuestion()
	q.Label = strings.Repeat("\u6b63", 64)
	if err := ValidateNewQuestion(q); err != nil {
		t.Fatalf("64 non-ASCII characters: %v", err)
	}
	q = baseQuestion()
	q.Label = strings.Repeat("\u6b63", 65)
	if !errors.Is(ValidateNewQuestion(q), ErrInvalidQuestion) {
		t.Fatal("65 non-ASCII characters: want error, got nil")
	}
}

var answerOptions = []QuestionOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}

func TestQuestionValidation_Answer(t *testing.T) {
	cases := []struct {
		name     string
		state    QuestionState
		current  string
		answer   Answer
		wantErr  error
		wantWrap bool
	}{
		{
			name:    "open option only accepted",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a"},
			wantErr: nil,
		},
		{
			name:    "open text only accepted",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", Quote: "ship it"},
			wantErr: nil,
		},
		{
			name:    "open option and text accepted",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "b", Quote: "and here is why"},
			wantErr: nil,
		},
		{
			name:    "open whitespace-only quote with no option rejected",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", Quote: "   \t  "},
			wantErr: ErrAnswerEmpty,
		},
		{
			name:    "open whitespace-only quote with option accepted",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a", Quote: "   "},
			wantErr: nil,
		},
		{
			name:    "open unknown option rejected",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "zz"},
			wantErr: ErrUnknownOption,
		},
		{
			name:    "open supersedes rejected",
			state:   QuestionOpen,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a", Supersedes: "old1"},
			wantErr: ErrSupersedesWrongTarget,
		},
		{
			name:    "answered supersedes current accepted",
			state:   QuestionAnswered,
			current: "old1",
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "b", Supersedes: "old1"},
			wantErr: nil,
		},
		{
			name:    "answered without supersedes rejected",
			state:   QuestionAnswered,
			current: "old1",
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a"},
			wantErr: ErrAnswerNeedsSupersedes,
		},
		{
			name:    "answered supersedes wrong target rejected",
			state:   QuestionAnswered,
			current: "old1",
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a", Supersedes: "someone-else"},
			wantErr: ErrSupersedesWrongTarget,
		},
		{
			name:    "withdrawn accepts no answer",
			state:   QuestionWithdrawn,
			current: "old1",
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a"},
			wantErr: ErrQuestionNotOpen,
		},
		{
			name:    "expired accepts no answer",
			state:   QuestionExpired,
			answer:  Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a"},
			wantErr: ErrQuestionNotOpen,
		},
		{
			name:     "quote over cap rejected",
			state:    QuestionOpen,
			answer:   Answer{ID: "n1", QuestionID: "q1", Quote: strings.Repeat("x", answerQuoteMaxBytes+1)},
			wantErr:  ErrAnswerEmpty,
			wantWrap: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckAnswerAllowed(tc.state, tc.current, tc.answer, answerOptions)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckAnswerAllowed() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckAnswerAllowed() = nil, want wrapped %v", tc.wantErr)
			}
			if tc.wantWrap {
				if !strings.Contains(err.Error(), "the cap is") {
					t.Fatalf("error %q does not mention the cap", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("CheckAnswerAllowed() = %v, want wrapped %v", err, tc.wantErr)
			}
		})
	}
}

func TestQuestionValidation_AnswerQuotePreserved(t *testing.T) {
	quote := "   keep   "
	if strings.TrimSpace(quote) == quote {
		t.Fatal("test helper: expected a padded quote")
	}
	a := Answer{ID: "n1", QuestionID: "q1", ChosenOptionID: "a", Quote: quote}
	if err := CheckAnswerAllowed(QuestionOpen, "", a, answerOptions); err != nil {
		t.Fatalf("padded quote with option: %v", err)
	}
	if a.Quote != quote {
		t.Fatalf("stored quote changed: %q", a.Quote)
	}
}

func TestQuestionValidation_QuoteHash(t *testing.T) {
	// Lowercase hex of the SHA-256 of the exact bytes.
	if got := QuoteSHA256("hello"); got != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("QuoteSHA256(\"hello\") = %s", got)
	}
	if got := QuoteSHA256(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("QuoteSHA256(\"\") = %s", got)
	}
	for _, quote := range []string{
		"  leading and trailing  ",
		"line one\r\nline two",
		"héllo wörld ✓ émoji",
		"tab\there",
	} {
		// A byte-identical quote must hash identically in either copy.
		other := quote[:len(quote)/2] + quote[len(quote)/2:]
		if got, want := QuoteSHA256(quote), QuoteSHA256(other); got != want {
			t.Fatalf("QuoteSHA256 is not stable for %q: %s != %s", quote, got, want)
		}
	}
	// Differing bytes must not collide.
	if QuoteSHA256("a ") == QuoteSHA256(" a") {
		t.Fatal("QuoteSHA256 collides for byte-different quotes")
	}
	if QuoteSHA256("\r\n") == QuoteSHA256("\n\r") {
		t.Fatal("QuoteSHA256 collides for byte-different quotes")
	}
}

func validGithubToken() string {
	return "ghp_" + strings.Repeat("a", 36)
}

func TestQuestionValidation_SecretScan(t *testing.T) {
	detected := []struct {
		name    string
		text    string
		wantPat string
	}{
		{name: "private key block", text: "the key was -----BEGIN RSA PRIVATE KEY----- MIIE...", wantPat: "private key"},
		{name: "aws key id", text: "the key id is AKIAIOSFODNN7EXAMPLE", wantPat: "aws key id"},
		{name: "aws secret key assignment", text: "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", wantPat: "aws secret key"},
		{name: "github token", text: "use " + validGithubToken() + " for the push", wantPat: "github token"},
		{name: "sk style key", text: "the api key is sk-" + strings.Repeat("Ab0", 14), wantPat: "api key"},
		{name: "google api key", text: "web key AIza" + strings.Repeat("SyA0", 9) + "abcd", wantPat: "google api key"},
		{name: "bearer token", text: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig", wantPat: "bearer token"},
		{name: "token assignment", text: "deploy token: abcdefgh123456", wantPat: "secret assignment"},
		{name: "password assignment", text: "password=SuperSecret123", wantPat: "secret assignment"},
	}
	for _, tc := range detected {
		t.Run(tc.name, func(t *testing.T) {
			pat, found := ScanAnswerForSecret(tc.text)
			if !found {
				t.Fatalf("ScanAnswerForSecret(%q) not found", tc.text)
			}
			if pat != tc.wantPat {
				t.Fatalf("pattern = %q, want %q", pat, tc.wantPat)
			}
		})
	}

	allowed := []struct {
		name string
		text string
	}{
		{name: "ordinary prose", text: "Ship now. The risk is low and the plan is sound."},
		{name: "secret reference by name", text: "The credentials live in the store; use ${secret:db-password} at runtime."},
		{name: "short value not secret-looking", text: "password: x"},
	}
	for _, tc := range allowed {
		t.Run(tc.name, func(t *testing.T) {
			pat, found := ScanAnswerForSecret(tc.text)
			if found {
				t.Fatalf("ScanAnswerForSecret(%q) = (%q, true), want not found", tc.text, pat)
			}
		})
	}
}

func TestQuestionValidation_SecretRefusal(t *testing.T) {
	secret := validGithubToken()
	pat, found := ScanAnswerForSecret("please save " + secret)
	if !found {
		t.Fatal("expected the token to be detected")
	}
	msg := SecretRefusalMessage(pat)
	for _, want := range []string{
		"not stored",
		"Resend it without the secret",
		"datawatch secrets store",
		"${secret:name}",
		"filesystem",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal message does not contain %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, secret) {
		t.Fatalf("refusal message contains the matched secret:\n%s", msg)
	}
	// The message reports the rule name, not the text.
	if !strings.Contains(msg, pat) {
		t.Fatalf("refusal message does not name the rule %q:\n%s", pat, msg)
	}
}

func TestQuestionValidation_ErrorsAreTyped(t *testing.T) {
	// Every sentinel error is independent and usable with errors.Is.
	sentinels := []error{
		ErrQuestionNotOpen,
		ErrAnswerNeedsSupersedes,
		ErrSupersedesWrongTarget,
		ErrAnswerEmpty,
		ErrUnknownOption,
		ErrSecretInAnswer,
		ErrInvalidQuestion,
	}
	seen := map[error]bool{}
	for _, s := range sentinels {
		if seen[s] {
			t.Fatalf("duplicate sentinel %v", s)
		}
		seen[s] = true
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("errors.Is(%v, %v) = true, want independent sentinels", a, b)
			}
		}
	}
	// ErrSecretInAnswer carries a stable message for callers that wrap it.
	if !strings.Contains(ErrSecretInAnswer.Error(), "secret") {
		t.Fatalf("unexpected ErrSecretInAnswer message %q", ErrSecretInAnswer)
	}
}
