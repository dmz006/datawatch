//go:build linux

package server

import (
	"context"

	"github.com/dmz006/datawatch/internal/proclimit"
)

func sandboxHandLimit() proclimit.Starter {
	s := proclimit.Scope{MemoryBytes: 2 << 30}
	if s.Available(context.Background()) != nil {
		return noTreeLimit{}
	}
	return s
}
