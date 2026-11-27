//go:build unix && !linux

package mergepipeline

import "github.com/dmz006/datawatch/internal/proclimit"

func sandboxLimit() (proclimit.Starter, bool) { return nil, false }
