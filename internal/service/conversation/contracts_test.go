package conversation

import (
	"testing"

	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
)

func TestConversationTurn_fields(t *testing.T) {
	raw := map[string]any{"key": "val"}
	turn := convchannel.Turn{
		Channel:        convchannel.ChannelPortal,
		ConversationID: "c_xyz",
		UserID:         "u_123",
		Message:        "hello",
		Raw:            raw,
	}
	if turn.Channel != convchannel.ChannelPortal || turn.ConversationID != "c_xyz" {
		t.Errorf("Channel/ConversationID: got %q %q", turn.Channel, turn.ConversationID)
	}
	if turn.UserID != "u_123" || turn.Message != "hello" {
		t.Errorf("UserID/Message: got %q %q", turn.UserID, turn.Message)
	}
	if turn.Raw["key"] != "val" {
		t.Errorf("Raw: got %v", turn.Raw)
	}
}

func TestConversationResult_fields(t *testing.T) {
	result := ConversationResult{
		Reply: "Sure, I'll do that.",
		Runs:  []SpawnedRun{{TaskID: "t_1", RunID: "r_001"}, {TaskID: "t_2", RunID: "r_002"}},
	}
	if result.Reply != "Sure, I'll do that." {
		t.Errorf("Reply: got %q", result.Reply)
	}
	if len(result.Runs) != 2 || result.Runs[0].RunID != "r_001" || result.Runs[1].TaskID != "t_2" {
		t.Errorf("Runs: got %v", result.Runs)
	}
}

func TestChannelConstants_nonEmptyAndDistinct(t *testing.T) {
	seen := make(map[string]bool)
	validChannels := convchannel.ValidChannels()
	for _, ch := range validChannels {
		if ch == "" {
			t.Errorf("channel constant is empty")
		}
		if seen[ch] {
			t.Errorf("duplicate channel %q", ch)
		}
		seen[ch] = true
	}
	if len(validChannels) != 2 {
		t.Errorf("expected 2 channel constants, got %d", len(validChannels))
	}
	// Ensure the named constants match
	if convchannel.ChannelPortal != "portal" || convchannel.ChannelWebhook != "webhook" {
		t.Errorf("channel constant values changed")
	}
}

func TestValidChannel(t *testing.T) {
	for _, ch := range convchannel.ValidChannels() {
		if !convchannel.ValidChannel(ch) {
			t.Errorf("ValidChannel(%q) = false, want true", ch)
		}
	}
	if convchannel.ValidChannel("") {
		t.Error("ValidChannel(\"\") = true, want false")
	}
	if convchannel.ValidChannel("slack") {
		t.Error("ValidChannel(\"slack\") = true, want false")
	}
}
