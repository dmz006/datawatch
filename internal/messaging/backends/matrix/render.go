// render.go — sender display-name normaliser for bridge ghost MXIDs (BL241 P1, Q8.1).
//
// Bridge bots use machine-generated MXIDs like @signal_+15555550100:server.
// This package normalises those into human-readable display strings so audit
// logs and the routing layer don't surface raw MXIDs to operators.
package matrix

import (
	"regexp"
	"strings"
)

// NormaliseSender returns a human-readable label for an MXID.
// For native users it returns the MXID unchanged.
// For bridge ghosts it returns "<Bridge>: <handle>", e.g. "Signal: +15555550100".
func NormaliseSender(mxid string) string {
	origin := Classify(mxid)
	if !IsBridge(origin) {
		return mxid
	}
	handle := extractHandle(mxid, origin)
	prefix := bridgePrefix(origin)
	if handle == "" {
		return prefix + ": " + mxid
	}
	return prefix + ": " + handle
}

// extractHandle strips the bridge prefix and server suffix from a ghost MXID
// and returns the remaining handle (phone number, username, ID, etc.).
func extractHandle(mxid string, origin Origin) string {
	localpart := mxid
	// strip server part
	if idx := strings.Index(mxid, ":"); idx >= 0 {
		localpart = mxid[:idx]
	}
	// strip leading '@'
	localpart = strings.TrimPrefix(localpart, "@")

	var prefixRe *regexp.Regexp
	switch origin {
	case OriginBridgeSignal:
		prefixRe = regexp.MustCompile(`(?i)^signal_`)
	case OriginBridgeTelegram:
		prefixRe = regexp.MustCompile(`(?i)^telegram_`)
	case OriginBridgeWhatsApp:
		prefixRe = regexp.MustCompile(`(?i)^whatsapp_`)
	case OriginBridgeSlack:
		prefixRe = regexp.MustCompile(`(?i)^slack_`)
	case OriginBridgeDiscord:
		prefixRe = regexp.MustCompile(`(?i)^discord_`)
	case OriginBridgeIRC:
		prefixRe = regexp.MustCompile(`(?i)^(_irc_|irc_)`)
	case OriginBridgeUnknown:
		prefixRe = regexp.MustCompile(`(?i)^_\w+_`)
	}
	if prefixRe == nil {
		return localpart
	}
	return prefixRe.ReplaceAllString(localpart, "")
}

// bridgePrefix returns the human-readable bridge name.
func bridgePrefix(origin Origin) string {
	switch origin {
	case OriginBridgeSignal:
		return "Signal"
	case OriginBridgeTelegram:
		return "Telegram"
	case OriginBridgeWhatsApp:
		return "WhatsApp"
	case OriginBridgeSlack:
		return "Slack"
	case OriginBridgeDiscord:
		return "Discord"
	case OriginBridgeIRC:
		return "IRC"
	case OriginBridgeUnknown:
		return "Bridge"
	default:
		return "Matrix"
	}
}
