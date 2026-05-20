// bot.go — bot-user registration helper for the Matrix backend (BL241 P1).
//
// Implements the `datawatch setup matrix create-account` flow:
//  1. Try to register a new user on the homeserver.
//  2. If registration is closed, fall back to login (user already exists).
//  3. Returns the access token + MXID for the caller to persist via the
//     secrets store per the Secrets-Store Rule.
//
// The Register and Login functions are intentionally side-effect-free (no config
// writes); the CLI command in main.go handles persistence.
package matrix

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/id"
)

// randomRead is a package-level variable so tests can inject a deterministic reader.
var randomRead = func(b []byte) (int, error) { return rand.Read(b) }

// BotCredentials is the result of a successful Register or Login call.
type BotCredentials struct {
	MXID        string
	AccessToken string
	DeviceID    string
}

// ErrRegistrationClosed is returned when the homeserver rejects registration
// with M_FORBIDDEN or M_UNKNOWN (common on closed servers like matrix.org).
var ErrRegistrationClosed = errors.New("homeserver does not accept third-party registrations — create an account in Element first, then run `datawatch setup matrix` to paste the access token")

// Register attempts to create a new bot account on homeserver with the given
// username and password. It uses the /register?kind=user endpoint with
// auth type m.login.dummy (works on open-registration homeservers like a local
// Synapse).
//
// On M_FORBIDDEN (closed registration) it returns ErrRegistrationClosed so the
// caller can suggest the manual path.
func Register(ctx context.Context, homeserver, username, password string) (*BotCredentials, error) {
	client, err := mautrix.NewClient(homeserver, "", "")
	if err != nil {
		return nil, fmt.Errorf("matrix client: %w", err)
	}
	client.Client = &http.Client{}

	creds, err := client.RegisterDummy(ctx, &mautrix.ReqRegister{
		Username: username,
		Password: password,
		Auth:     map[string]interface{}{"type": "m.login.dummy"},
	})
	if err != nil {
		if isClosedRegistration(err) {
			return nil, ErrRegistrationClosed
		}
		return nil, fmt.Errorf("register: %w", err)
	}
	return &BotCredentials{
		MXID:        string(creds.UserID),
		AccessToken: creds.AccessToken,
		DeviceID:    string(creds.DeviceID),
	}, nil
}

// Login authenticates an existing bot account and returns a new access token.
// Use this when the account already exists but no token is stored.
func Login(ctx context.Context, homeserver, mxid, password string) (*BotCredentials, error) {
	client, err := mautrix.NewClient(homeserver, id.UserID(mxid), "")
	if err != nil {
		return nil, fmt.Errorf("matrix client: %w", err)
	}

	resp, err := client.Login(ctx, &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: localpart(mxid),
		},
		Password:                 password,
		StoreCredentials:         true,
		StoreHomeserverURL:       true,
		InitialDeviceDisplayName: "datawatch",
	})
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	return &BotCredentials{
		MXID:        string(resp.UserID),
		AccessToken: resp.AccessToken,
		DeviceID:    string(resp.DeviceID),
	}, nil
}

// isClosedRegistration returns true for homeserver errors that indicate
// registration is not allowed for third parties.
func isClosedRegistration(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "M_FORBIDDEN") ||
		strings.Contains(msg, "M_UNKNOWN") ||
		strings.Contains(msg, "registration is not enabled")
}

// localpart extracts the localpart from an MXID like @user:server → "user".
func localpart(mxid string) string {
	s := strings.TrimPrefix(mxid, "@")
	if idx := strings.Index(s, ":"); idx >= 0 {
		return s[:idx]
	}
	return s
}

// GeneratePassword generates a 24-character random password suitable for bot
// accounts. It is not returned to the operator (the access token is what
// matters); the password is stored in the secrets store if the operator wants
// it for future logins.
func GeneratePassword() (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	const length = 24
	b := make([]byte, length)
	rb := make([]byte, length*2)
	if _, err := io.ReadFull(rand.Reader, rb); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = charset[int(rb[i])%len(charset)]
	}
	return string(b), nil
}
