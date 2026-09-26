package conversation

import (
	"strings"
	"testing"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/mock"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
)

// Internal input keeps its stored role when a conversation is replayed.
func TestReplayMessageFromStoreKeepsASystemChannelMessageAsInput(t *testing.T) {
	channel := "system"
	msg := replayMessageFromStore(coreconv.Message{
		Role:    "user",
		Content: "internal context",
		Channel: &channel,
	})
	if msg.Role != "user" {
		t.Fatalf("replayMessageFromStore.Role = %q, want user", msg.Role)
	}
	if msg.Content != "internal context" {
		t.Fatalf("replayMessageFromStore.Content = %q", msg.Content)
	}
}

func TestReplayMessageFromStore_UserRolePassthrough(t *testing.T) {
	channel := "portal"
	msg := replayMessageFromStore(coreconv.Message{
		Role:    "user",
		Content: "hello",
		Channel: &channel,
	})
	if msg.Role != "user" {
		t.Fatalf("replayMessageFromStore(user).Role = %q, want user", msg.Role)
	}
}

// A Tier 1 conversation resumes from the stored rows on every turn, so
// reasoning state has to survive the round trip through the message table or a
// second turn would send the protocol a turn it rejects.
func TestReplayMessageFromStore_CarriesReasoningState(t *testing.T) {
	stored := `{"protocol":"anthropic","data":[{"type":"thinking","signature":"sig-1"}]}`
	msg := replayMessageFromStore(coreconv.Message{
		Role:              "assistant",
		Content:           "done",
		ProviderStateJSON: &stored,
	})
	if !msg.ProviderState.Belongs("anthropic") {
		t.Fatalf("ProviderState = %+v, want the stored anthropic state", msg.ProviderState)
	}
	if !strings.Contains(string(msg.ProviderState.Data), "sig-1") {
		t.Errorf("state %s lost the signature", msg.ProviderState.Data)
	}
}

// A row written before reasoning state existed, and one holding something that
// no longer parses, both replay as a message without state rather than failing
// the turn: a conversation without reasoning continuity still works.
func TestReplayMessageFromStore_ToleratesMissingAndUnreadableState(t *testing.T) {
	unreadable := "{not json"
	for name, stored := range map[string]*string{
		"absent":     nil,
		"unreadable": &unreadable,
	} {
		t.Run(name, func(t *testing.T) {
			msg := replayMessageFromStore(coreconv.Message{
				Role: "assistant", Content: "done", ProviderStateJSON: stored,
			})
			if msg.ProviderState != nil {
				t.Errorf("ProviderState = %+v, want none", msg.ProviderState)
			}
			if msg.Content != "done" {
				t.Errorf("Content = %q, want the message to survive", msg.Content)
			}
		})
	}
}

func TestSystemPromptNamesTheSpaceAndHowToSwitch(t *testing.T) {
	portal := systemPrompt(turnRunInput{Channel: convchannel.ChannelPortal, SpaceName: "QA"})
	if !strings.Contains(portal, `belongs to the Space "QA"`) || !strings.Contains(portal, "sidebar") {
		t.Errorf("portal prompt lacks the Space or the Portal route:\n%s", portal)
	}
	if strings.Contains(portal, "/space") {
		t.Errorf("portal prompt mentions the chat command:\n%s", portal)
	}

	chat := systemPrompt(turnRunInput{Channel: "telegram", SpaceName: "QA"})
	for _, want := range []string{`"QA"`, "started in Telegram", "/space <number>", "sidebar"} {
		if !strings.Contains(chat, want) {
			t.Errorf("chat prompt lacks %q:\n%s", want, chat)
		}
	}

	if unnamed := systemPrompt(turnRunInput{Channel: convchannel.ChannelPortal}); strings.Contains(unnamed, "# Space") {
		t.Errorf("prompt without a Space name has a Space section:\n%s", unnamed)
	}
}

func TestBuildConversationToolsListsSpacesOnlyWithAStore(t *testing.T) {
	base := turnRunInput{Channel: convchannel.ChannelPortal, UserID: "u_1", SpaceID: "s_1", TaskService: &task.Service{}}
	if hasTool(buildConversationTools(base, nil), toolNameListSpaces) {
		t.Error("ListSpaces registered without a space store")
	}
	base.Spaces = &mock.MockSpaceStore{}
	if !hasTool(buildConversationTools(base, nil), toolNameListSpaces) {
		t.Error("ListSpaces missing with a space store")
	}
}

func hasTool(tools []llm.Tool, name string) bool {
	for _, t := range tools {
		if t.Name() == name {
			return true
		}
	}
	return false
}
