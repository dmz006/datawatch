// v5.27.10 (BL216) — tests for `channel info` chat-channel command parser.

package router

import "testing"

func TestParse_ChannelInfo(t *testing.T) {
	cmd := Parse("channel info")
	if cmd.Type != CmdChannelInfo {
		t.Errorf("got %v, want CmdChannelInfo", cmd.Type)
	}
}

func TestParse_ChannelInfo_CaseInsensitive(t *testing.T) {
	cmd := Parse("Channel Info")
	if cmd.Type != CmdChannelInfo {
		t.Errorf("got %v, want CmdChannelInfo", cmd.Type)
	}
}

func TestParse_ChannelInfo_NoMatch(t *testing.T) {
	// Plain "channel" without the info suffix should not trigger.
	cmd := Parse("channel")
	if cmd.Type == CmdChannelInfo {
		t.Errorf("plain 'channel' should not match CmdChannelInfo")
	}
}
