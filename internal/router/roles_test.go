package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRoleParityCommAddUpdateAndList(t *testing.T) {
	var bodies []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/llms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"llms":[{"name":"a","roles":["planning","review"]}],"status":{"role_warnings":["role \"planning\" is claimed by 2 enabled entries (a, b); using \"a\""]}}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/llms/", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	r := loopbackRouter(t, srv)
	send := func(msg string) string { return strings.Join(r.HandleTestMessage(msg), "\n") }

	send("llm add a kind=ollama roles=planning,Review")
	if len(bodies) != 1 || !reflect.DeepEqual(bodies[0]["roles"], []any{"planning", "Review"}) {
		t.Fatalf("llm add roles body = %v", bodies)
	}
	send("llm update a model=m")
	if _, has := bodies[1]["roles"]; has {
		t.Errorf("update without roles must not send roles: %v", bodies[1])
	}
	send("llm update a roles=")
	if got, has := bodies[2]["roles"]; !has || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("update with empty roles sent %v (present=%v), want []", got, has)
	}

	out := send("llm list")
	for _, want := range []string{"planning", "review", "role_warnings"} {
		if !strings.Contains(out, want) {
			t.Errorf("llm list reply missing %q:\n%s", want, out)
		}
	}
}
