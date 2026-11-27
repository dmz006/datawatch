package server

import (
	"net/http"
	"testing"

	"github.com/dmz006/datawatch/internal/acceptance"
)

// No request can say that datawatch itself saw the source, and a source named
// in a request never gives a list lighter rules.
func TestAcceptanceREST_ARequestCannotPickTheKind(t *testing.T) {
	s, _ := acceptanceServer(t, "report")
	for _, field := range []string{`"source_known":true`, `"sourceknown":true`, `"kind":"delivery"`} {
		body := `{"id":"x","source":"message","ask":"Fix the export crash",` + field + `}`
		if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", body, nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", field, rr.Code, rr.Body.String())
		}
	}
	v := acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", `{"id":"m","source":"message","ask":"Fix the export crash"}`, nil))
	if v.List.Kind != acceptance.KindOther || v.List.SourceKnown || len(v.List.Items) != 2 {
		t.Fatalf("kind %q known %v items %+v", v.List.Kind, v.List.SourceKnown, v.List.Items)
	}
}
