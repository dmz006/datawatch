// v7.0.0-alpha.14 (#236) — chained transcriber. Tries primary first;
// falls back to secondary on any error. Used to back the openai-compat
// transport with the local Whisper venv as last-resort, so a server
// outage / model-not-loaded / network blip doesn't kill voice.
//
// Operator-filed 2026-05-09: "fallback could also try the local
// whisper. I don't remember configuring whisper to use ollama, that
// is ok of it is because it is a bigger gpu". Op uses openai-compat
// to OpenWebUI for big-GPU throughput, but wants the venv to take
// over silently when the remote path fails.

package transcribe

import (
	"context"
	"fmt"
	"strings"
)

// ChainedTranscriber tries Primary first; on any error, falls back to
// Secondary. Both errors surface in the final error message so the
// operator sees what the chain attempted.
type ChainedTranscriber struct {
	Primary   Transcriber
	Secondary Transcriber
	// Names are used in error messages and the auto-fall-back footer.
	PrimaryName   string
	SecondaryName string
}

// Transcribe runs the chain.
func (c *ChainedTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if c.Primary == nil && c.Secondary == nil {
		return "", fmt.Errorf("transcribe(chain): no transcribers configured")
	}
	var primaryErr error
	if c.Primary != nil {
		text, err := c.Primary.Transcribe(ctx, audioPath)
		if err == nil {
			return text, nil
		}
		primaryErr = err
		if c.Secondary == nil {
			return "", err
		}
	}
	if c.Secondary == nil {
		return "", primaryErr
	}
	text, err := c.Secondary.Transcribe(ctx, audioPath)
	if err == nil {
		// Tag the response so the operator knows the chain fell through.
		// Strip any nested fall-back footer the secondary might have
		// added (e.g. openai-compat's own auto-fall-back notice).
		core := strings.TrimSpace(text)
		secondary := c.SecondaryName
		if secondary == "" {
			secondary = "secondary"
		}
		primary := c.PrimaryName
		if primary == "" {
			primary = "primary"
		}
		return core + fmt.Sprintf("\n\n_(transcribe: %s failed (%s); fell back to %s)_", primary, primaryErr, secondary), nil
	}
	primary := c.PrimaryName
	if primary == "" {
		primary = "primary"
	}
	secondary := c.SecondaryName
	if secondary == "" {
		secondary = "secondary"
	}
	return "", fmt.Errorf("transcribe(chain): both transcribers failed.\n  %s: %s\n  %s: %s", primary, primaryErr, secondary, err)
}

// Preflight runs a lightweight reachability check for any transcriber
// that supports it. Returns nil for transcribers without preflight
// (best-effort — preflight failures are warnings, not hard errors).
type Preflighter interface {
	Preflight(ctx context.Context) error
}

// Preflight on a chain delegates to whichever members implement it.
// Returns nil if no member has Preflight; combined error otherwise.
func (c *ChainedTranscriber) Preflight(ctx context.Context) error {
	var msgs []string
	if pf, ok := c.Primary.(Preflighter); ok && pf != nil {
		if err := pf.Preflight(ctx); err != nil {
			name := c.PrimaryName
			if name == "" {
				name = "primary"
			}
			msgs = append(msgs, name+": "+err.Error())
		}
	}
	if pf, ok := c.Secondary.(Preflighter); ok && pf != nil {
		if err := pf.Preflight(ctx); err != nil {
			name := c.SecondaryName
			if name == "" {
				name = "secondary"
			}
			msgs = append(msgs, name+": "+err.Error())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("preflight: %s", strings.Join(msgs, "; "))
}
