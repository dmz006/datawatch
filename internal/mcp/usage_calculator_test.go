// usage_calculator_test.go — the six usage-calculator MCP tools, proxied
// against an httptest backend standing in for the loopback /api/usage/...
// REST surface.

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

type usageCalcRecord struct {
	method   string
	path     string
	rawQuery string
	body     map[string]any
}

// usageCalcServer answers every request with reply (status code code) and
// records the last one, including the raw query string the advice tool needs.
func usageCalcServer(t *testing.T, code int, reply string) (*Server, *usageCalcRecord) {
	t.Helper()
	rec := &usageCalcRecord{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.method, rec.path, rec.rawQuery = r.Method, r.URL.Path, r.URL.RawQuery
		rec.body = nil
		_ = json.Unmarshal(raw, &rec.body)
		w.WriteHeader(code)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	return &Server{webPort: port}, rec
}

func TestUsageCalcAdviceProxiesGetWithQuery(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"percent":42,"advice":"carry 3 lanes"}`)
	res, err := s.handleUsageAdvice(context.Background(), callReq(map[string]any{
		"variant": "pace-gauge", "policy": "run-flat-out", "lanes": 5.0, "cost": 2.5,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/usage/advice" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	qp, _ := url.ParseQuery(rec.rawQuery)
	for k, want := range map[string]string{"variant": "pace-gauge", "policy": "run-flat-out", "lanes": "5", "cost": "2.5"} {
		if got := qp.Get(k); got != want {
			t.Errorf("query %s = %q, want %q (raw: %s)", k, got, want, rec.rawQuery)
		}
	}
	if text := automationText(t, res); !strings.Contains(text, `"advice"`) {
		t.Errorf("result = %s", text)
	}
}

func TestUsageCalcAdviceOmitsOmittedArguments(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"percent":42}`)
	if _, err := s.handleUsageAdvice(context.Background(), callReq(nil)); err != nil {
		t.Fatal(err)
	}
	if rec.rawQuery != "" {
		t.Errorf("query = %q, want empty for omitted arguments", rec.rawQuery)
	}
	// A single argument sends only itself.
	if _, err := s.handleUsageAdvice(context.Background(), callReq(map[string]any{"policy": "steady"})); err != nil {
		t.Fatal(err)
	}
	if rec.rawQuery != "policy=steady" {
		t.Errorf("query = %q, want policy=steady", rec.rawQuery)
	}
}

func TestUsageCalcReadingsProxiesGet(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"readings":[{"id":"r1","percent":42.5,"session_percent":33,"lanes":2,"source":"typed"}],"extra_credit_points":25,"is_keeper":true,"stale":false}`)
	res, err := s.handleUsageReadings(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodGet || rec.path != "/api/usage/readings" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"readings"`) || !strings.Contains(text, `"extra_credit_points"`) {
		t.Errorf("result = %s", text)
	}
}

func TestUsageCalcReadingAdd(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)

	// A percent reading with the optional secondary fields.
	res, err := s.handleUsageReadingAdd(context.Background(), callReq(map[string]any{
		"percent": 42.5, "at": "2026-12-15T09:00:00Z", "session_percent": 33, "lanes": 2.0,
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPost || rec.path != "/api/usage/readings" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["percent"] != 42.5 || rec.body["session_percent"] != 33.0 || rec.body["lanes"] != 2.0 || rec.body["at"] != "2026-12-15T09:00:00Z" {
		t.Errorf("body = %v", rec.body)
	}
	if _, ok := rec.body["spend_usd"]; ok {
		t.Errorf("spend_usd must not be sent with a percent reading: %v", rec.body)
	}
	// No source is sent: REST treats an empty one as the built (typed) kind,
	// and the planned kinds are not usable yet.
	if _, ok := rec.body["source"]; ok {
		t.Errorf("source must not be a tool argument yet: %v", rec.body)
	}

	// A spend reading stands on its own.
	if _, err := s.handleUsageReadingAdd(context.Background(), callReq(map[string]any{"spend_usd": 6.75})); err != nil {
		t.Fatal(err)
	}
	if rec.body["spend_usd"] != 6.75 {
		t.Errorf("body = %v", rec.body)
	}
	if _, ok := rec.body["percent"]; ok {
		t.Errorf("percent must not be sent with a spend reading: %v", rec.body)
	}
}

func TestUsageCalcReadingAddRejectsBadInput(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	ctx := context.Background()
	for name, args := range map[string]map[string]any{
		"both figures":      {"percent": 40, "spend_usd": 6},
		"neither figure":    {"at": "2026-12-15T09:00:00Z"},
		"percent above 100": {"percent": 101},
		"percent below 0":   {"percent": -4},
		"negative spend":    {"spend_usd": -1},
		"negative lanes":    {"percent": 40, "lanes": -1.0},
		"session above 100": {"percent": 40, "session_percent": 101},
	} {
		rec.method = ""
		res, err := s.handleUsageReadingAdd(ctx, callReq(args))
		if err != nil || res == nil {
			t.Fatalf("%s: err=%v res=%v", name, err, res)
		}
		text := automationText(t, res)
		if !res.IsError {
			t.Errorf("%s: result = %s", name, text)
		}
		if rec.method != "" {
			t.Errorf("%s: reached the server before local validation", name)
		}
	}

	// A refusal the server answers comes back as an error result.
	s, _ = usageCalcServer(t, http.StatusBadRequest, "invalid usage reading: carry either a percent or a spend figure, not both")
	res, err := s.handleUsageReadingAdd(ctx, callReq(map[string]any{"percent": 42.5}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !res.IsError || !strings.Contains(text, "invalid usage reading") {
		t.Errorf("bad reading not surfaced: %s", text)
	}
}

func TestUsageCalcReadingDelete(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	res, err := s.handleUsageReadingDelete(context.Background(), callReq(map[string]any{"id": "r-9"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodDelete || rec.path != "/api/usage/readings/r-9" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok":true`) {
		t.Errorf("result = %s", text)
	}

	// An id-less call is a local error and never reaches the server.
	rec.method = ""
	res, err = s.handleUsageReadingDelete(context.Background(), callReq(nil))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !res.IsError || !strings.Contains(text, "id") {
		t.Errorf("missing id not rejected: %s", text)
	}
	if rec.method != "" {
		t.Errorf("id-less delete reached the server")
	}

	// An unknown id is surfaced as a 404 error result.
	s, _ = usageCalcServer(t, http.StatusNotFound, "usage reading not found")
	res, err = s.handleUsageReadingDelete(context.Background(), callReq(map[string]any{"id": "nope"}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text = automationText(t, res)
	if !res.IsError || !strings.Contains(text, "not found") || !strings.Contains(text, "404") {
		t.Errorf("unknown id not surfaced: %s", text)
	}

	// An id carrying slash or query characters stays one path segment on
	// the route: it is escaped before the loopback call.
	s, rec = usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	res, err = s.handleUsageReadingDelete(context.Background(), callReq(map[string]any{"id": "r/9?x=1"}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v isError=%v", err, res != nil && res.IsError)
	}
	if rec.path != "/api/usage/readings/r/9?x=1" {
		t.Errorf("path = %q, want the escaped id resolved back to one segment", rec.path)
	}
	if rec.rawQuery != "" {
		t.Errorf("rawQuery = %q, want empty (no id characters leaking into the query)", rec.rawQuery)
	}
}

func TestUsageCalcExtraCreditSet(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	res, err := s.handleUsageExtraCreditSet(context.Background(), callReq(map[string]any{"points": 25}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/usage/extra-credit" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["points"] != 25.0 || len(rec.body) != 1 {
		t.Errorf("body = %v", rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok":true`) {
		t.Errorf("result = %s", text)
	}

	// Zero is a valid figure (nothing extra bought), sent as given.
	if _, err := s.handleUsageExtraCreditSet(context.Background(), callReq(map[string]any{"points": 0.0})); err != nil {
		t.Fatal(err)
	}
	if rec.body["points"] != 0.0 {
		t.Errorf("zero figure dropped: %v", rec.body)
	}

	// Negative or missing points never reach the server.
	rec.method = ""
	for _, args := range []map[string]any{{"points": -5.0}, {}} {
		res, err := s.handleUsageExtraCreditSet(context.Background(), callReq(args))
		if err != nil || res == nil {
			t.Fatalf("args=%v: err=%v res=%v", args, err, res)
		}
		text := automationText(t, res)
		if !res.IsError {
			t.Errorf("args=%v: not rejected: %s", args, text)
		}
		if rec.method != "" {
			t.Errorf("args=%v: reached the server", args)
		}
	}
}

func TestUsageCalcRemainingWorkSet(t *testing.T) {
	s, rec := usageCalcServer(t, http.StatusOK, `{"ok":true}`)
	res, err := s.handleUsageRemainingWorkSet(context.Background(), callReq(map[string]any{"points": 15.5}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/usage/remaining-work" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["points"] != 15.5 || len(rec.body) != 1 {
		t.Errorf("body = %v", rec.body)
	}
	if text := automationText(t, res); !strings.Contains(text, `"ok":true`) {
		t.Errorf("result = %s", text)
	}

	// Omitting points, or passing it as null, clears the typed figure:
	// both travel as a null PUT, and REST removes the setting so the
	// finish-early estimate falls back to the queue estimate.
	for _, args := range []map[string]any{{}, {"points": nil}} {
		rec.method = ""
		if _, err := s.handleUsageRemainingWorkSet(context.Background(), callReq(args)); err != nil {
			t.Fatalf("args=%v: %v", args, err)
		}
		if rec.method != http.MethodPut || rec.path != "/api/usage/remaining-work" {
			t.Errorf("args=%v: proxied %s %s", args, rec.method, rec.path)
		}
		if v, ok := rec.body["points"]; !ok || v != nil {
			t.Errorf("args=%v: body = %v, want {\"points\":null} (clear)", args, rec.body)
		}
	}

	rec.method = ""
	res, err = s.handleUsageRemainingWorkSet(context.Background(), callReq(map[string]any{"points": -1.0}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !res.IsError {
		t.Errorf("negative figure not rejected: %s", text)
	}
	if rec.method != "" {
		t.Errorf("negative figure reached the server")
	}
}

func TestUsageCalcToolsRegisteredAndCapped(t *testing.T) {
	want := map[string]string{
		"usage_advice":             federation.CapUsageRead,
		"usage_readings":           federation.CapUsageRead,
		"usage_reading_add":        federation.CapUsageWrite,
		"usage_reading_delete":     federation.CapUsageWrite,
		"usage_extra_credit_set":   federation.CapUsageWrite,
		"usage_remaining_work_set": federation.CapUsageWrite,
	}
	for name, cap := range want {
		got, ok := federation.MCPToolCap[name]
		if !ok {
			t.Errorf("%s: no capability mapping", name)
			continue
		}
		if got != cap {
			t.Errorf("%s: capability = %s, want %s", name, got, cap)
		}
	}

	tools := map[string]mcpsdk.Tool{
		"usage_advice":             (&Server{}).toolUsageAdvice(),
		"usage_readings":           (&Server{}).toolUsageReadings(),
		"usage_reading_add":        (&Server{}).toolUsageReadingAdd(),
		"usage_reading_delete":     (&Server{}).toolUsageReadingDelete(),
		"usage_extra_credit_set":   (&Server{}).toolUsageExtraCreditSet(),
		"usage_remaining_work_set": (&Server{}).toolUsageRemainingWorkSet(),
	}
	for name, tool := range tools {
		if tool.Name != name {
			t.Errorf("tool name = %q, want %q", tool.Name, name)
		}
		// No tracker identifiers in a shipped name or description.
		trackerID := regexp.MustCompile(`\b(BL|B|G|GH|SEC)-?[0-9]+`)
		if trackerID.MatchString(tool.Name) || trackerID.MatchString(tool.Description) {
			t.Errorf("%s: name or description holds a tracker identifier", name)
		}
		// Every tool says the calculator is advice only: it never stops or
		// slows the lanes it reports on.
		d := strings.ToLower(tool.Description)
		if !strings.Contains(d, "never stops or slows") {
			t.Errorf("%s: description does not state the calculator never stops or slows lanes", name)
		}
	}

	required := map[string][]string{
		"usage_advice":           nil,
		"usage_readings":         nil,
		"usage_reading_add":      nil,
		"usage_reading_delete":   {"id"},
		"usage_extra_credit_set": {"points"},
		// points is optional: omit it, or pass null, to clear the typed
		// figure.
		"usage_remaining_work_set": nil,
	}
	for name, wantRequired := range required {
		got := tools[name].InputSchema.Required
		if len(got) != len(wantRequired) {
			t.Errorf("%s: required = %v, want %v", name, got, wantRequired)
			continue
		}
		for _, w := range wantRequired {
			found := false
			for _, g := range got {
				if g == w {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: %s not required: %v", name, w, got)
			}
		}
	}

	props := tools["usage_advice"].InputSchema.Properties
	for _, arg := range []string{"variant", "policy", "lanes", "cost"} {
		if _, ok := props[arg]; !ok {
			t.Errorf("usage_advice argument %s missing", arg)
		}
	}
	props = tools["usage_reading_add"].InputSchema.Properties
	for _, arg := range []string{"percent", "spend_usd", "at", "session_percent", "lanes"} {
		if _, ok := props[arg]; !ok {
			t.Errorf("usage_reading_add argument %s missing", arg)
		}
	}

	// The descriptions state the operator decisions that shape the tools.
	d := strings.ToLower(tools["usage_advice"].Description)
	for _, phrase := range []string{
		"never stops or slows",
		"variant and policy are independent",
		"one call only",
	} {
		if !strings.Contains(d, phrase) {
			t.Errorf("usage_advice description does not say %q", phrase)
		}
	}
	d = strings.ToLower(tools["usage_extra_credit_set"].Description)
	if !strings.Contains(d, "never a reading") || !strings.Contains(d, "the operator's call") {
		t.Errorf("usage_extra_credit_set description does not state the setting-not-reading decision")
	}
	d = strings.ToLower(tools["usage_remaining_work_set"].Description)
	if !strings.Contains(d, "never a reading") || !strings.Contains(d, "daemon-local") {
		t.Errorf("usage_remaining_work_set description does not state the setting-not-reading decision")
	}
	// points is optional and omitting it, or passing null, clears.
	if !strings.Contains(d, "omit points") && !strings.Contains(d, "omitted") {
		t.Errorf("usage_remaining_work_set description does not say points can be omitted: %s", d)
	}
	if !strings.Contains(d, "null") || !strings.Contains(d, "clear") {
		t.Errorf("usage_remaining_work_set description does not say null clears the figure: %s", d)
	}
}

// TestUsageCalcConfigSetVariant proves config_set accepts the calculator's
// variant key: the same general-configuration surface the REST PUT uses,
// so the MCP side needs no usage-specific routing for the setting.
func TestUsageCalcConfigSetVariant(t *testing.T) {
	rec := &usageCalcRecord{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.method, rec.path = r.Method, r.URL.Path
		rec.body = nil
		if !json.Valid(raw) {
			http.Error(w, "invalid config json", http.StatusBadRequest)
			return
		}
		_ = json.Unmarshal(raw, &rec.body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(srv.URL, ":")[2])
	s := &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: true}}

	res, err := s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "usage.calculator.variant", "value": "pace-gauge",
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if rec.method != http.MethodPut || rec.path != "/api/config" {
		t.Errorf("proxied %s %s", rec.method, rec.path)
	}
	if rec.body["usage.calculator.variant"] != "pace-gauge" {
		t.Errorf("body = %v, want the variant key carried through", rec.body)
	}

	// A denied server (mcp.allow_self_config off) refuses before any
	// request is made.
	s = &Server{webPort: port, cfg: &config.MCPConfig{AllowSelfConfig: false}}
	rec.method = ""
	res, err = s.handleConfigSet(context.Background(), callReq(map[string]any{
		"key": "usage.calculator.variant", "value": "pace-gauge",
	}))
	if err != nil || res == nil {
		t.Fatalf("err=%v res=%v", err, res)
	}
	text := automationText(t, res)
	if !strings.Contains(text, "allow_self_config") {
		t.Errorf("denial = %s", text)
	}
	if rec.method != "" {
		t.Errorf("denied config_set reached the server")
	}
}
