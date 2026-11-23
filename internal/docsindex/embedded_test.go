// v9.182.46 — regression test for the smoke gate's docs_list_howtos check:
// the binary-embedded docs index must be the generated one (make docs-index),
// not the placeholder `{}` a bare `go build` embeds. The smoke daemon serves
// ListHowtos() from exactly this embedded JSON; with the placeholder it
// answers null and the smoke gate's "howtos >= 10" check fails.
package docsindex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedIndexHasHowtos(t *testing.T) {
	var doc struct {
		Chunks []Chunk `json:"chunks"`
	}
	if err := json.Unmarshal(EmbeddedBM25JSON, &doc); err != nil {
		t.Fatalf("embedded docs index is not valid BM25 JSON: %v", err)
	}
	howtos := map[string]bool{}
	for _, c := range doc.Chunks {
		if strings.HasPrefix(c.Path, "howto/") {
			howtos[c.Path] = true
		}
	}
	if len(howtos) < 10 {
		t.Fatalf("embedded docs index has %d howto paths — the placeholder index (make docs-index not run before the build); the smoke gate requires at least 10", len(howtos))
	}
}
