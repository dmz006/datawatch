//go:build linux

package mergepipeline

import (
	"context"

	"github.com/dmz006/datawatch/internal/proclimit"
)

func sandboxLimit() (proclimit.Starter, bool) {
	s := proclimit.Scope{MemoryBytes: 2 << 30}
	return s, s.Available(context.Background()) == nil
}
