// as.go — Application Service registration and transaction handler (BL241 P1).
//
// When cfg.Matrix.AS.Enabled is true, the daemon registers itself as a Matrix
// Application Service. The homeserver pushes events to the AS HTTP endpoint
// instead of the daemon polling via /sync. This gives the daemon a stable
// namespace (@datawatch_*:server) and avoids the sync-loop overhead.
//
// Usage:
//  1. Run `datawatch setup matrix as-register` to generate registration.yaml.
//  2. Add the registration.yaml path to the homeserver's app_service_config_files.
//  3. Set matrix.application_service.enabled: true in datawatch config.
//
// The AS handler is mounted at POST /_matrix/app/v1/transactions/{txnID} by
// the server package when AS mode is active.
package matrix

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// ASConfig holds the parameters needed to write registration.yaml.
type ASConfig struct {
	Homeserver   string
	ASID         string // e.g. "datawatch"
	Namespace    string // e.g. "@datawatch_*" (localpart regex)
	ASToken      string
	HSToken      string
	CallbackURL  string // e.g. "http://localhost:29333"
	ListenAddr   string // e.g. ":29333"
}

// registrationYAML returns the content of registration.yaml that must be
// placed in the homeserver's app_service_config_files list.
func registrationYAML(cfg ASConfig) string {
	ns := cfg.Namespace
	if ns == "" {
		ns = "@datawatch_.*"
	}
	id := cfg.ASID
	if id == "" {
		id = "datawatch"
	}
	return fmt.Sprintf(`id: %s
url: %s
as_token: %s
hs_token: %s
sender_localpart: %s_bot
namespaces:
  users:
    - exclusive: false
      regex: '%s'
  rooms: []
  aliases: []
rate_limited: false
protocols: []
`, id, cfg.CallbackURL, cfg.ASToken, cfg.HSToken, id, ns)
}

// WriteRegistrationFile writes registration.yaml to path using the given config.
func WriteRegistrationFile(path string, cfg ASConfig) error {
	return os.WriteFile(path, []byte(registrationYAML(cfg)), 0600)
}

// asTransaction is the payload the homeserver POSTs to /_matrix/app/v1/transactions/{txnID}.
type asTransaction struct {
	Events []json.RawMessage `json:"events"`
}

// ASHandler processes homeserver-pushed events for the Application Service path.
// It is embedded in Backend when AS mode is active and satisfies http.Handler.
type ASHandler struct {
	hsToken  string
	roomID   id.RoomID
	botMXID  id.UserID
	handler  func(msg event.MessageEventContent, sender string, roomID string, evID string)
}

// NewASHandler creates an ASHandler. hsToken is validated on every request.
func NewASHandler(hsToken string, roomID id.RoomID, botMXID id.UserID,
	handler func(event.MessageEventContent, string, string, string)) *ASHandler {
	return &ASHandler{
		hsToken: hsToken,
		roomID:  roomID,
		botMXID: botMXID,
		handler: handler,
	}
}

// ServeHTTP handles POST /_matrix/app/v1/transactions/{txnID}.
func (h *ASHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate hs_token from Authorization header.
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("access_token")
	}
	if token != h.hsToken {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var tx asTransaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	for _, rawEv := range tx.Events {
		h.processRawEvent(rawEv)
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func (h *ASHandler) processRawEvent(raw json.RawMessage) {
	var envelope struct {
		Type    string          `json:"type"`
		RoomID  string          `json:"room_id"`
		Sender  string          `json:"sender"`
		EventID string          `json:"event_id"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return
	}
	if envelope.Type != "m.room.message" {
		return
	}
	if id.UserID(envelope.Sender) == h.botMXID {
		return
	}
	if h.roomID != "" && id.RoomID(envelope.RoomID) != h.roomID {
		return
	}
	var content event.MessageEventContent
	if err := json.Unmarshal(envelope.Content, &content); err != nil {
		return
	}
	h.handler(content, envelope.Sender, envelope.RoomID, envelope.EventID)
}

// GenerateTokens creates a random AS token + HS token pair suitable for
// registration.yaml. They are hex-encoded 32-byte random values.
func GenerateTokens() (asToken, hsToken string, err error) {
	buf := make([]byte, 32)
	_, err = randomRead(buf)
	if err != nil {
		return
	}
	asToken = fmt.Sprintf("%x", buf)
	_, err = randomRead(buf)
	if err != nil {
		return
	}
	hsToken = fmt.Sprintf("%x", buf)
	return
}

// timestampSuffix returns a short timestamp string for generating unique ASID.
func timestampSuffix() string {
	return fmt.Sprintf("%d", time.Now().Unix()%100000)
}
