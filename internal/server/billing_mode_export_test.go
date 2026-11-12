package server

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

// Exports for the external billing_mode_surfaces_test package, which must live
// outside package server so it can import the MCP package.

func NewLLMServerForTest(t *testing.T) *Server { return newLLMServer(t) }

func (s *Server) LLMHandlerForTest() http.HandlerFunc { return s.handleLLMs }

func (s *Server) RegistryForTest() *inference.Registry { return s.inferenceReg }

func (s *Server) LLMBillingModesForTest() []map[string]interface{} { return s.llmBillingModes() }

func (s *Server) FirstLLMBillingModeErrorForTest(p map[string]interface{}) (int, error) {
	return s.firstLLMBillingModeError(p)
}

func (s *Server) ApplyLLMBillingModePatchForTest(p map[string]interface{}) error {
	return s.applyLLMBillingModePatch(p)
}
