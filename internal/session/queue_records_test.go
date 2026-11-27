package session

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

type memRecords struct {
	m    map[string][]byte
	fail error
}

func (r *memRecords) LoadQueue() ([]json.RawMessage, error) {
	var ids []string
	for id := range r.m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []json.RawMessage
	for _, id := range ids {
		out = append(out, r.m[id])
	}
	return out, nil
}

func (r *memRecords) SaveQueueItem(id string, data []byte) error {
	if r.fail != nil {
		return r.fail
	}
	r.m[id] = append([]byte(nil), data...)
	return nil
}

func (r *memRecords) DeleteQueueItem(id string) error {
	if r.fail != nil {
		return r.fail
	}
	delete(r.m, id)
	return nil
}

func TestReqStoreQueueOverRecords(t *testing.T) {
	rec := &memRecords{m: map[string][]byte{}}
	q, err := NewQueueStoreOn(rec)
	if err != nil {
		t.Fatal(err)
	}
	a, err := q.Push("r", map[string]any{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Claim("r", "s", 60); err != nil {
		t.Fatal(err)
	}
	again, err := NewQueueStoreOn(rec)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := again.Get(a.ID); !ok || got.State != QueueStateClaimed {
		t.Fatalf("%+v", got)
	}
	// A change that cannot be saved did not happen.
	rec.fail = errors.New("no space")
	if it, err := again.Push("r", nil); err == nil || it != nil {
		t.Fatalf("%v %v", it, err)
	}
	if err := again.Complete(a.ID, nil); err == nil {
		t.Fatal("completed unsaved")
	}
	if err := again.Delete(a.ID); err == nil {
		t.Fatal("deleted unsaved")
	}
	rec.fail = nil
	if got, ok := again.Get(a.ID); !ok || got.State != QueueStateClaimed || len(again.List("", "")) != 1 {
		t.Fatalf("%+v %d", got, len(again.List("", "")))
	}
	if err := again.Delete(a.ID); err != nil || len(rec.m) != 0 {
		t.Fatalf("%v %d", err, len(rec.m))
	}
}

func TestReqStoreDeferredQueueIsUnreadableNotEmpty(t *testing.T) {
	q := NewDeferredQueueStore()
	if _, err := q.Push("r", nil); !errors.Is(err, ErrQueueNotReady) {
		t.Fatalf("push: %v", err)
	}
	if _, err := q.Claim("r", "s", 1); !errors.Is(err, ErrQueueNotReady) {
		t.Fatalf("claim: %v", err)
	}
	if _, err := q.ListErr("", ""); !errors.Is(err, ErrQueueNotReady) {
		t.Fatalf("list: %v", err)
	}
	if len(q.List("", "")) != 0 {
		t.Fatal("an unsaved push stayed")
	}
}

func TestReqWireRemoteQueueAsksTheDaemon(t *testing.T) {
	var seen []string
	d := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.URL.Path == "/api/queue/claim":
			_, _ = w.Write([]byte("null"))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"id":"a1","role":"r","state":"pending"}]`))
		default:
			_, _ = w.Write([]byte(`{"id":"a1","role":"r","state":"pending"}`))
		}
	}))
	defer d.Close()
	down := false
	q := NewRemoteQueueStore(func(method, path string, body any) ([]byte, error) {
		if down {
			return nil, errors.New("daemon not reachable")
		}
		data, _ := json.Marshal(body)
		req, err := http.NewRequest(method, d.URL+path, strings.NewReader(string(data)))
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var buf strings.Builder
		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return nil, err
		}
		buf.Write(raw)
		return []byte(buf.String()), nil
	})
	if it, err := q.Push("r", nil); err != nil || it.ID != "a1" {
		t.Fatalf("%v %v", it, err)
	}
	if it, err := q.Claim("r", "s", 5); err != nil || it != nil {
		t.Fatalf("%v %v", it, err)
	}
	if err := q.Complete("a1", nil); err != nil {
		t.Fatal(err)
	}
	if err := q.Fail("a1", "x"); err != nil {
		t.Fatal(err)
	}
	if err := q.Delete("a1"); err != nil {
		t.Fatal(err)
	}
	if got := q.List("r", "pending"); len(got) != 1 {
		t.Fatalf("%v", got)
	}
	if q.ExpireLeases() != 0 {
		t.Fatal("a remote queue expires nothing")
	}
	want := []string{"POST /api/queue/push", "POST /api/queue/claim", "POST /api/queue/complete",
		"POST /api/queue/fail", "DELETE /api/queue/a1", "GET /api/queue?role=r&state=pending"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("%v", seen)
	}
	down = true
	if _, err := q.ListErr("", ""); err == nil {
		t.Fatal("an unreachable daemon reads as an empty queue")
	}
}
