// CLI surface for the per-LLM billing mode: `datawatch config set
// llm.<name>.billing_mode` and `datawatch llm update --billing-mode`. Both use
// the shared validator, so a bad value fails locally with the REST message.

package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigSetLLMBillingMode_Valid(t *testing.T) {
	seen := llmDaemon(t)
	if err := runConfigSet(&config.Config{}, "llm.worker.billing_mode", "allowance"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	calls := seen()
	if len(calls) != 1 || calls[0].method != http.MethodPut || calls[0].path != "/api/config" {
		t.Fatalf("calls = %+v, want one PUT /api/config", calls)
	}
	if calls[0].body["llms.worker.billing_mode"] != "allowance" {
		t.Errorf("body = %v, want llms.worker.billing_mode=allowance", calls[0].body)
	}
}

func TestConfigSetLLMBillingMode_Invalid(t *testing.T) {
	seen := llmDaemon(t)
	err := runConfigSet(&config.Config{}, "llm.worker.billing_mode", "bogus")
	want := config.ValidateBillingMode("bogus")
	if err == nil || !strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("err = %v, want it to contain %q", err, want)
	}
	if len(seen()) != 0 {
		t.Errorf("daemon hit %d times, want 0 (local validation)", len(seen()))
	}
}

func TestLLMUpdateBillingMode_Valid(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(true)
	c.SetArgs([]string{"worker", "--billing-mode", "local"})
	if err := runRunE(t, c); err != nil {
		t.Fatalf("llm update: %v", err)
	}
	calls := seen()
	if len(calls) != 1 || calls[0].body["billing_mode"] != "local" {
		t.Fatalf("calls = %+v, want billing_mode=local forwarded", calls)
	}
}

func TestLLMUpdateBillingMode_Invalid(t *testing.T) {
	seen := llmDaemon(t)
	c := newLLMAddCmd(true)
	c.SetArgs([]string{"worker", "--billing-mode", "bogus"})
	err := runRunE(t, c)
	want := config.ValidateBillingMode("bogus")
	if err == nil || err.Error() != want.Error() {
		t.Fatalf("err = %v, want %q", err, want)
	}
	if len(seen()) != 0 {
		t.Errorf("daemon hit %d times, want 0", len(seen()))
	}
}

func TestBillingModeHelpNamesThreeWords(t *testing.T) {
	texts := map[string]string{
		"config set": newConfigSetCmd().Long,
		"llm update": newLLMAddCmd(true).Flag("billing-mode").Usage,
	}
	for name, txt := range texts {
		for _, w := range []string{"api", "allowance", "local"} {
			if !strings.Contains(txt, w) {
				t.Errorf("%s help missing %q", name, w)
			}
		}
	}
}
