//go:build !linux

package server

import "github.com/dmz006/datawatch/internal/proclimit"

func sandboxHandLimit() proclimit.Starter { return noTreeLimit{} }
