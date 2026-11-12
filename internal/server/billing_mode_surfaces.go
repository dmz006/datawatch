package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// LLM billing mode on the config surface. The registry holds the entries, so
// the config keys are "llms.<name>.billing_mode"; GET /api/config lists each
// entry's stored mode under "llms".

const (
	llmConfigPrefix      = "llms."
	llmBillingModeSuffix = ".billing_mode"
)

// llmBillingModeKey splits "llms.<name>.billing_mode" into the entry name.
func llmBillingModeKey(key string) (string, bool) {
	if !strings.HasPrefix(key, llmConfigPrefix) || !strings.HasSuffix(key, llmBillingModeSuffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(key, llmConfigPrefix), llmBillingModeSuffix)
	return name, name != ""
}

// firstLLMBillingModeError checks every llms.<name>.billing_mode key in a
// config patch with the shared validator, so a bad value rejects the whole PUT
// with the same message the LLM endpoints and tools give.
func (s *Server) firstLLMBillingModeError(patch map[string]interface{}) (int, error) {
	for k, v := range patch {
		name, ok := llmBillingModeKey(k)
		if !ok {
			continue
		}
		str, _ := v.(string)
		if err := config.ValidateBillingMode(strings.TrimSpace(str)); err != nil {
			return http.StatusBadRequest, err
		}
		if s.inferenceReg == nil {
			return http.StatusServiceUnavailable, errLLMRegistryDisabled
		}
		if _, err := s.inferenceReg.Get(name); err != nil {
			return http.StatusNotFound, err
		}
	}
	return 0, nil
}

// applyLLMBillingModePatch writes the billing mode keys to the registry and
// removes them from the patch so the generic config applier never sees them.
func (s *Server) applyLLMBillingModePatch(patch map[string]interface{}) error {
	for k, v := range patch {
		name, ok := llmBillingModeKey(k)
		if !ok {
			continue
		}
		delete(patch, k)
		existing, err := s.inferenceReg.Get(name)
		if err != nil {
			return err
		}
		l := *existing
		l.APIKeyRefPresent, l.APIKeyRefPrefix = false, ""
		str, _ := v.(string)
		l.BillingMode = strings.TrimSpace(str)
		if err := s.inferenceReg.Update(&l); err != nil {
			return err
		}
		s.auditLLM(name, "llm_update")
	}
	return nil
}

// llmBillingModes is the "llms" block of GET /api/config.
func (s *Server) llmBillingModes() []map[string]interface{} {
	out := []map[string]interface{}{}
	if s.inferenceReg == nil {
		return out
	}
	for _, l := range s.inferenceReg.List() {
		out = append(out, map[string]interface{}{"name": l.Name, "billing_mode": l.BillingMode})
	}
	return out
}

var errLLMRegistryDisabled = errors.New("llm registry disabled")
