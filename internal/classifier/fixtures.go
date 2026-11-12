package classifier

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed testdata/*.json
var fixtureFS embed.FS

// Fixture names under testdata/.
const (
	FixturePrompt           = "prompt.json"
	FixtureUnparseableReply = "verifier_reply_unparseable.json"
	FixtureSystemOne        = "systemone_response.json"
	FixtureLogprobs         = "logprobs_response.json"
)

// Prompt is the shared prompt of the native and logprobs fixtures.
type Prompt struct {
	Fixture string   `json:"_fixture"`
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
}

// VerifierReply is a recorded open-model verifier reply.
type VerifierReply struct {
	Fixture string `json:"_fixture"`
	Reply   string `json:"reply"`
}

// LoadFixture decodes testdata/<name> into v. Every fixture carries a
// "_fixture" label stating it is synthetic.
func LoadFixture(name string, v any) error {
	b, err := fixtureFS.ReadFile("testdata/" + name)
	if err != nil {
		return fmt.Errorf("classifier fixture %q: %w", name, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("classifier fixture %q: %w", name, err)
	}
	return nil
}
