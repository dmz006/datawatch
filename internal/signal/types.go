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
type GroupInfo struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName,omitempty"`
}

// DataMessage is the data payload of a Signal envelope.
type DataMessage struct {
	Message   string     `json:"message,omitempty"`
	Timestamp int64      `json:"timestamp"`
	GroupInfo *GroupInfo `json:"groupInfo,omitempty"`
}

// Envelope is the outer wrapper of a Signal message as returned by signal-cli.
type Envelope struct {
	Source      string       `json:"source"`
	SourceName  string       `json:"sourceName,omitempty"`
	Timestamp   int64        `json:"timestamp"`
	DataMessage *DataMessage `json:"dataMessage,omitempty"`
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
