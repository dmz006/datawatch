package signal

import "context"

// SignalBackend abstracts the Signal protocol implementation.
// The current implementation uses signal-cli over JSON-RPC.
// Future implementations may use native Go bindings to libsignal-ffi.
type SignalBackend interface {
	// Link initiates device linking. Calls onQR with the QR URI string.
	// The caller should render the QR code for the user to scan.
	Link(deviceName string, onQR func(qrURI string)) error

	// Send sends a text message to a Signal group.
	Send(groupID, message string) error

	// Subscribe starts receiving messages. Calls handler for each incoming message.
	// Blocks until ctx is cancelled.
	Subscribe(ctx context.Context, handler func(IncomingMessage)) error

	// ListGroups returns the list of joined groups.
	ListGroups(ctx context.Context) ([]Group, error)

	// SelfNumber returns the registered phone number.
	SelfNumber() string

	// Close shuts down the backend cleanly.
	Close() error
}

// Group represents a Signal group.
type Group struct {
	ID   string
	Name string
}
