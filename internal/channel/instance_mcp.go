// Instance-level .mcp.json management with a scoped, short-lived token.
//
// WriteInstanceMCPConfig (mcp_config.go) carries DATAWATCH_TOKEN in the
// entry's env. Previously that was the daemon's long-lived server token;
// now the instance config must carry only a scoped, TTL-bound token that
// can be revoked independently of every session and removed when no
// longer needed.
//
// This package does not import the token store (internal/auth) — it
// depends on the InstanceTokenMinter interface, which the daemon wires
// to the real store; tests use a fake.

package channel

import (
	"fmt"
	"sync"
	"time"
)

// InstanceTokenMinter is the minimal slice of the session token store
// that instance-level MCP config management needs: mint a scoped
// (capability-limited, TTL-bound) token for the instance owner, and
// revoke whatever token that owner currently holds.
type InstanceTokenMinter interface {
	MintScoped(ownerID string, caps []string, ttl time.Duration) (string, error)
	Revoke(ownerID string) error
}

// InstanceTokenOwnerID is the owner id used when minting the scoped
// token carried by the instance-level .mcp.json. It is a reserved
// owner id not tied to any one session, so token-store sweeps keyed on
// the active-session list will not collect it before it expires.
const InstanceTokenOwnerID = "instance:mcp-config"

// InstanceTokenTTL is the default lifetime of the scoped token carried
// by the instance-level .mcp.json. It is deliberately short: the token
// is re-minted by Refresh on every session launch while stale, and
// Refresh reuses the existing token while it still has more than half
// of its lifetime left, so a running tool's credential is not
// superseded per spawn.
const InstanceTokenTTL = time.Hour

// InstanceMCPConfig manages the daemon's instance-level .mcp.json
// (in the daemon's data directory) so it carries only a scoped,
// short-lived DATAWATCH_TOKEN instead of a long-lived credential.
//
// DataDir/ChannelJSPath are the instance config's location and the JS
// bridge path to register; BaseEnv is the env map to write minus the
// token (which must NOT carry any token); Caps are the scoped
// capability set requested for the minted token; Minter issues and
// revokes tokens (see InstanceTokenMinter); TTL optionally overrides
// the token lifetime (zero means InstanceTokenTTL).
//
// All methods are safe for concurrent use. clock is an unexported test
// seam: when non-nil, tests pin "now" instead of time.Now.
type InstanceMCPConfig struct {
	DataDir       string
	ChannelJSPath string
	BaseEnv       map[string]string
	Caps          []string
	Minter        InstanceTokenMinter
	TTL           time.Duration

	mu       sync.Mutex
	lastTok  string
	issuedAt time.Time

	clock func() time.Time
}

func (c *InstanceMCPConfig) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func (c *InstanceMCPConfig) ttl() time.Duration {
	if c.TTL != 0 {
		return c.TTL
	}
	return InstanceTokenTTL
}

// Refresh makes sure the instance-level .mcp.json is present and
// carries a valid scoped token:
//
//   - While the token this value already minted still has more than half
//     of its lifetime remaining, it is reused (kept in memory, guarded
//     by a mutex) so concurrent session launches don't keep superseding
//     a token a running tool may still hold.
//   - Otherwise (or when no token has been minted yet) a new scoped
//     token is minted for InstanceTokenOwnerID. A nil Minter or a mint
//     error is an error and writes NO token — on a mint error any stale
//     datawatch entry is also removed so no old credential stays on
//     disk. There is deliberately no fallback to a long-lived token.
//
// Returns true when the file was rewritten.
func (c *InstanceMCPConfig) Refresh() (bool, error) {
	ttl := c.ttl()

	c.mu.Lock()
	reuse := c.lastTok != "" && c.now().Sub(c.issuedAt) < ttl/2
	if !reuse {
		if c.Minter == nil {
			c.mu.Unlock()
			return false, fmt.Errorf("InstanceMCPConfig.Refresh: no Minter configured; refusing to fall back to a long-lived token")
		}
		tok, err := c.Minter.MintScoped(InstanceTokenOwnerID, c.Caps, ttl)
		if err != nil {
			c.mu.Unlock()
			// No token was minted: remove any stale entry so an old
			// credential does not stay on disk, and keep the error from
			// carrying the (nonexistent) token.
			if rmErr := RemoveInstanceMCPConfig(c.DataDir); rmErr != nil {
				return false, fmt.Errorf("InstanceMCPConfig.Refresh: mint: %w (and removing stale entry: %v)", err, rmErr)
			}
			return false, fmt.Errorf("InstanceMCPConfig.Refresh: mint scoped token: %w", err)
		}
		c.lastTok = tok
		c.issuedAt = c.now()
	}
	tok := c.lastTok
	c.mu.Unlock()

	env := make(map[string]string, len(c.BaseEnv)+1)
	for k, v := range c.BaseEnv {
		env[k] = v
	}
	env["DATAWATCH_TOKEN"] = tok

	return WriteInstanceMCPConfig(c.DataDir, c.ChannelJSPath, env)
}

// Release revokes the instance token through the Minter and removes the
// datawatch entry (and the file, if it holds nothing else) from the
// instance-level .mcp.json, dropping the in-memory token too.
// Idempotent and safe when nothing was ever minted.
func (c *InstanceMCPConfig) Release() error {
	if c.Minter != nil {
		if err := c.Minter.Revoke(InstanceTokenOwnerID); err != nil {
			return fmt.Errorf("InstanceMCPConfig.Release: revoke: %w", err)
		}
	}
	c.mu.Lock()
	c.lastTok = ""
	c.issuedAt = time.Time{}
	c.mu.Unlock()
	return RemoveInstanceMCPConfig(c.DataDir)
}
