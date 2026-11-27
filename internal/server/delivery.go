// Delivery check REST surface.
//
//	GET  /api/delivery               the current report; runs no check
//	POST /api/delivery/check         run the check now and return its report
//	POST /api/delivery/ack           mark entries as seen { selector, actor? }
//	POST /api/delivery/accept        let entries wait { selector, reason, until, actor? }
//	POST /api/delivery/drop          close failed or backlog entries { selector, reason, actor? }
//	POST /api/delivery/test-message  send a test message on every channel
//
// A selector is keys[], kind, state, match_reason and backlog. The check
// reports: it blocks nothing. Reading and running it needs delivery:read; the
// other routes need delivery:write. The rule that only the operator
// acknowledges, accepts or drops is recorded and not enforced: no caller is
// refused for being a session or a peer, and who did it comes from the
// credential.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/delivery"
	"github.com/dmz006/datawatch/internal/federation"
)

// SetDelivery wires the delivery check exposed under /api/delivery.
func (s *HTTPServer) SetDelivery(d delivery.Service) {
	if s.api != nil {
		s.api.delivery = d
	}
}

const deliveryUnavailable = "the delivery check is not available"

// deliveryMaxBody bounds an action's body; a thousand keys fit many times over.
const deliveryMaxBody = 1 << 20

// deliveryGate applies the method, the capability and the wiring checks.
func (s *Server) deliveryGate(w http.ResponseWriter, r *http.Request, method, required string) bool {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if !s.fedCap(w, r, required) {
		return false
	}
	if s.delivery == nil {
		http.Error(w, deliveryUnavailable, http.StatusServiceUnavailable)
		return false
	}
	return true
}

// deliveryCallerRe is a session or peer name an assistant tool or chat command
// may put in the caller header.
var deliveryCallerRe = regexp.MustCompile(`^(session|peer):[A-Za-z0-9._-]{1,128}$`)

// deliveryBy is who an action is recorded under. A session or peer credential
// names itself, whatever the request says. On an admin request the caller
// header (set by the assistant tools and chat commands when they act for a
// session or peer) can name only a session or peer; otherwise the claimed
// surface label is kept, and any other claim reads "operator", so records
// never take free text from a body.
func (s *Server) deliveryBy(r *http.Request, claimed string) string {
	if by := s.pauseActor(r.Context(), ""); delivery.NonOperator(by) {
		return by
	}
	if h := r.Header.Get(federation.HeaderDeliveryCaller); delivery.NonOperator(h) {
		if deliveryCallerRe.MatchString(h) {
			return h
		}
		kind, _, _ := strings.Cut(h, ":")
		return kind + ":unknown"
	}
	switch c := strings.TrimSpace(claimed); c {
	case "cli", "web", "channel":
		return c
	}
	return "operator"
}

// deliveryDecode reads one JSON object. An unknown field is refused: a mistyped
// selector field would otherwise widen what the action applies to.
func deliveryDecode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, deliveryMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		http.Error(w, "bad request: one JSON object only", http.StatusBadRequest)
		return false
	}
	return true
}

// deliveryError writes the check's refusal with its status.
func deliveryError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, delivery.ErrSelector), errors.Is(err, delivery.ErrReasonCode), errors.Is(err, delivery.ErrUntil):
		code = http.StatusBadRequest
	case errors.Is(err, delivery.ErrNoMatch):
		code = http.StatusNotFound
	case errors.Is(err, delivery.ErrNotDroppable):
		code = http.StatusConflict
	case errors.Is(err, delivery.ErrCannotRecord), errors.Is(err, delivery.ErrRecords):
		code = http.StatusServiceUnavailable
	}
	http.Error(w, err.Error(), code)
}

func deliveryChanged(w http.ResponseWriter, n int, err error) {
	if err != nil {
		deliveryError(w, err)
		return
	}
	writeJSONOK(w, map[string]int{"changed": n})
}

// handleDelivery serves GET /api/delivery.
func (s *Server) handleDelivery(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodGet, federation.CapDeliveryRead) {
		return
	}
	writeJSONOK(w, s.delivery.Report())
}

// handleDeliveryCheck serves POST /api/delivery/check.
func (s *Server) handleDeliveryCheck(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodPost, federation.CapDeliveryRead) {
		return
	}
	// A caller that goes away must not stop a pass between its sends.
	report, err := s.delivery.Pass(context.WithoutCancel(r.Context()))
	if err != nil {
		deliveryError(w, err)
		return
	}
	writeJSONOK(w, report)
}

// handleDeliveryAck serves POST /api/delivery/ack.
func (s *Server) handleDeliveryAck(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodPost, federation.CapDeliveryWrite) {
		return
	}
	var req struct {
		delivery.Selector
		Actor string `json:"actor"`
	}
	if !deliveryDecode(w, r, &req) {
		return
	}
	n, err := s.delivery.Ack(req.Selector, s.deliveryBy(r, req.Actor))
	deliveryChanged(w, n, err)
}

// handleDeliveryAccept serves POST /api/delivery/accept.
func (s *Server) handleDeliveryAccept(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodPost, federation.CapDeliveryWrite) {
		return
	}
	var req struct {
		delivery.Selector
		Reason string `json:"reason"`
		Until  string `json:"until"`
		Actor  string `json:"actor"`
	}
	if !deliveryDecode(w, r, &req) {
		return
	}
	until, err := delivery.ParseUntil(req.Until)
	if err != nil {
		deliveryError(w, err)
		return
	}
	n, err := s.delivery.Accept(req.Selector, req.Reason, until, s.deliveryBy(r, req.Actor))
	deliveryChanged(w, n, err)
}

// handleDeliveryDrop serves POST /api/delivery/drop.
func (s *Server) handleDeliveryDrop(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodPost, federation.CapDeliveryWrite) {
		return
	}
	var req struct {
		delivery.Selector
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
	}
	if !deliveryDecode(w, r, &req) {
		return
	}
	n, err := s.delivery.Drop(req.Selector, req.Reason, s.deliveryBy(r, req.Actor))
	deliveryChanged(w, n, err)
}

// handleDeliveryTestMessage serves POST /api/delivery/test-message.
func (s *Server) handleDeliveryTestMessage(w http.ResponseWriter, r *http.Request) {
	if !s.deliveryGate(w, r, http.MethodPost, federation.CapDeliveryWrite) {
		return
	}
	results := s.delivery.TestMessage(context.WithoutCancel(r.Context()))
	if results == nil {
		results = []delivery.SendResult{}
	}
	writeJSONOK(w, map[string]any{"results": results})
}

// ----- settings -------------------------------------------------------------

const (
	deliveryEnabledKey  = "delivery.enabled"
	deliveryNotifyKey   = "delivery.notify"
	deliveryIntervalKey = "delivery.interval_seconds"

	deliveryMinIntervalSeconds = 60
)

// deliveryConfigSurface is the delivery block of GET /api/config: every key at
// its effective value.
func deliveryConfigSurface(c config.DeliveryConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":          c.EnabledOn(),
		"notify":           c.NotifyOn(),
		"interval_seconds": int(c.Interval().Seconds()),
	}
}

func deliveryBool(v interface{}) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(x))
		return b, err == nil
	}
	return false, false
}

// deliveryInterval reads the seconds between checks: 0 (the default) or a whole
// number of at least 60.
func deliveryInterval(v interface{}) (int, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case int:
		f = float64(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0, false
		}
		f = float64(n)
	default:
		return 0, false
	}
	if f != math.Trunc(f) || f > math.MaxInt32 || (f != 0 && f < deliveryMinIntervalSeconds) {
		return 0, false
	}
	return int(f), true
}

// firstDeliverySettingError refuses a bad delivery value before anything in the
// same PUT is applied.
func firstDeliverySettingError(patch map[string]interface{}) error {
	for _, key := range []string{deliveryEnabledKey, deliveryNotifyKey} {
		if v, ok := patch[key]; ok {
			if _, good := deliveryBool(v); !good {
				return fmt.Errorf("%s: must be a boolean (true or false)", key)
			}
		}
	}
	if v, ok := patch[deliveryIntervalKey]; ok {
		if _, good := deliveryInterval(v); !good {
			return fmt.Errorf("%s: must be a whole number of seconds, at least %d (0 = the default, %d)",
				deliveryIntervalKey, deliveryMinIntervalSeconds, config.DefaultDeliveryIntervalSeconds)
		}
	}
	return nil
}

// applyDeliveryConfigKey stores one delivery key; a bad value changes nothing.
func applyDeliveryConfigKey(cfg *config.Config, key string, v interface{}) {
	switch key {
	case deliveryEnabledKey:
		if b, ok := deliveryBool(v); ok {
			cfg.Delivery.Enabled = &b
		}
	case deliveryNotifyKey:
		if b, ok := deliveryBool(v); ok {
			cfg.Delivery.Notify = &b
		}
	case deliveryIntervalKey:
		if n, ok := deliveryInterval(v); ok {
			cfg.Delivery.IntervalSeconds = n
		}
	}
}
