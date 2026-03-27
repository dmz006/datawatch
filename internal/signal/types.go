package signal

import (
	"encoding/json"
	"time"
)

// MessageType represents the type of a Signal message.
type MessageType string

const (
	MessageTypeText    MessageType = "text"
	MessageTypeReceipt MessageType = "receipt"
)

// GroupInfo holds information about a Signal group.
// signal-cli v0.10+ uses "groupId" (base64); older versions used "groupId" as hex.
type GroupInfo struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName,omitempty"`
	Type      string `json:"type,omitempty"` // "DELIVER", "UPDATE", etc.
}

// DataMessage is the data payload of a Signal envelope.
type DataMessage struct {
	Message          string     `json:"message,omitempty"`
	Timestamp        int64      `json:"timestamp"`
	GroupInfo        *GroupInfo `json:"groupInfo,omitempty"`
	ExpiresInSeconds int        `json:"expiresInSeconds,omitempty"`
	ViewOnce         bool       `json:"viewOnce,omitempty"`
}

// Envelope is the outer wrapper of a Signal message as returned by signal-cli.
// The field names reflect signal-cli v0.10+ JSON-RPC format.
type Envelope struct {
	Source      string       `json:"source"`
	SourceName  string       `json:"sourceName,omitempty"`
	SourceUUID  string       `json:"sourceUuid,omitempty"`
	SourceNumber string      `json:"sourceNumber,omitempty"` // signal-cli v0.11+
	Timestamp   int64        `json:"timestamp"`
	DataMessage *DataMessage `json:"dataMessage,omitempty"`
	// Additional envelope types (not processed but logged when verbose)
	ReceiptMessage  json.RawMessage `json:"receiptMessage,omitempty"`
	TypingMessage   json.RawMessage `json:"typingMessage,omitempty"`
	SyncMessage     json.RawMessage `json:"syncMessage,omitempty"`
}

// EnvelopeType returns a human-readable type label for logging.
func (e *Envelope) EnvelopeType() string {
	switch {
	case e.DataMessage != nil:
		return "data"
	case e.ReceiptMessage != nil:
		return "receipt"
	case e.TypingMessage != nil:
		return "typing"
	case e.SyncMessage != nil:
		return "sync"
	default:
		return "unknown"
	}
}

// EffectiveSource returns the best available sender identifier.
// signal-cli v0.11+ populates sourceNumber; older versions use source.
func (e *Envelope) EffectiveSource() string {
	if e.SourceNumber != "" {
		return e.SourceNumber
	}
	return e.Source
}

// IncomingMessage is the parsed, ready-to-use form of an incoming Signal message.
type IncomingMessage struct {
	Envelope   Envelope
	GroupID    string
	Text       string
	Sender     string
	SenderName string
	ReceivedAt time.Time
}

// JSONRPCRequest is a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response or notification.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      *int            `json:"id"`
	Method  string          `json:"method,omitempty"` // for notifications
	Params  json.RawMessage `json:"params,omitempty"` // for notifications
}

// JSONRPCError is the error object in a JSON-RPC 2.0 response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
