// Package matrix — MXID origin classifier (BL241 P1, Round-3 Q8.1).
//
// classify.go is the single source of truth for bridge-ghost detection.
// Pattern rules are applied in order; the first match wins.
// Unknown MXIDs fall back to OriginNative.
package matrix

import (
	"regexp"
	"strings"
)

// Origin identifies where an MXID came from.
type Origin int

const (
	OriginNative          Origin = iota // regular Matrix user
	OriginBridgeSignal                  // Signal bridge ghost  (@signal_+1...:server)
	OriginBridgeTelegram                // Telegram bridge ghost (@telegram_...:server)
	OriginBridgeWhatsApp                // WhatsApp bridge ghost (@whatsapp_...:server)
	OriginBridgeSlack                   // Slack bridge ghost    (@slack_...:server)
	OriginBridgeDiscord                 // Discord bridge ghost  (@discord_...:server)
	OriginBridgeIRC                     // IRC bridge ghost      (@irc_...:server or _irc_*)
	OriginBridgeUnknown                 // unrecognised bridge prefix
)

// String returns the name used in audit logs.
func (o Origin) String() string {
	switch o {
	case OriginBridgeSignal:
		return "signal-bridge"
	case OriginBridgeTelegram:
		return "telegram-bridge"
	case OriginBridgeWhatsApp:
		return "whatsapp-bridge"
	case OriginBridgeSlack:
		return "slack-bridge"
	case OriginBridgeDiscord:
		return "discord-bridge"
	case OriginBridgeIRC:
		return "irc-bridge"
	case OriginBridgeUnknown:
		return "unknown-bridge"
	default:
		return "native"
	}
}

// bridgeRule maps a compiled pattern to an Origin.
type bridgeRule struct {
	re     *regexp.Regexp
	origin Origin
}

// bridgeRules is evaluated in order; first match wins.
var bridgeRules = []bridgeRule{
	{re: regexp.MustCompile(`(?i)^@signal_`), origin: OriginBridgeSignal},
	{re: regexp.MustCompile(`(?i)^@telegram_`), origin: OriginBridgeTelegram},
	{re: regexp.MustCompile(`(?i)^@whatsapp_`), origin: OriginBridgeWhatsApp},
	{re: regexp.MustCompile(`(?i)^@slack_`), origin: OriginBridgeSlack},
	{re: regexp.MustCompile(`(?i)^@discord_`), origin: OriginBridgeDiscord},
	{re: regexp.MustCompile(`(?i)^@(_irc_|irc_)`), origin: OriginBridgeIRC},
	// Generic puppet patterns: @_<bridgename>_... used by Double Puppet bridges.
	{re: regexp.MustCompile(`(?i)^@_\w+_`), origin: OriginBridgeUnknown},
}

// Classify returns the Origin for a given MXID string like "@user:server".
func Classify(mxid string) Origin {
	localpart := mxid
	if idx := strings.Index(mxid, ":"); idx >= 0 {
		localpart = mxid[:idx]
	}
	for _, rule := range bridgeRules {
		if rule.re.MatchString(localpart) {
			return rule.origin
		}
	}
	return OriginNative
}

// IsBridge returns true for any bridge origin.
func IsBridge(o Origin) bool { return o != OriginNative }
