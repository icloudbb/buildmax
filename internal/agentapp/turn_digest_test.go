package agentapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

func digestApp(t *testing.T, cfg config.TurnDigestConfig) *AgentApp {
	t.Helper()
	return &AgentApp{settings: config.Settings{Agent: config.AgentConfig{TurnDigest: cfg}}}
}

func toolTurn(reply string) TurnSummary {
	return TurnSummary{
		Prompt:         "fix the parser",
		Reply:          reply,
		Transcript:     "[user] fix the parser\n[calls write_file] {\"path\":\"parse.go\"}\n[tool result] ok\n[assistant] " + reply,
		ToolCalls:      1,
		WriteToolCalls: 1,
	}
}

func TestGenerateTurnDigestReadsBothFields(t *testing.T) {
	client := &scriptedClient{replies: []scriptedReply{{
		content: "```json\n{\"recap\": \"Rewrote the parser and ran the tests.\", \"suggestion\": \"yes, go ahead\"}\n```",
	}}}
	sess := NewSessionContext("test")
	before := len(sess.Messages())

	digest, err := digestApp(t, config.TurnDigestConfig{}).
		GenerateTurnDigest(context.Background(), sess, client, toolTurn("Done. Should I also update the docs?"))
	if err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if digest.Recap != "Rewrote the parser and ran the tests." {
		t.Errorf("recap = %q", digest.Recap)
	}
	if digest.Suggestion != "yes, go ahead" {
		t.Errorf("suggestion = %q", digest.Suggestion)
	}
	// The whole premise of the feature: what it writes is shown to the user and
	// never becomes something the model reads back.
	if got := len(sess.Messages()); got != before {
		t.Fatalf("digest appended %d messages to history; it must append none", got-before)
	}
	if client.profiles[0] != llm.ProfileProbe {
		t.Errorf("profile = %q, want %q", client.profiles[0], llm.ProfileProbe)
	}
}

// A turn that asked nothing gets no suggestion offered, so the call must not
// ask for one — otherwise the model fills the field to be helpful.
func TestGenerateTurnDigestSkipsSuggestionWhenNothingWasAsked(t *testing.T) {
	client := &scriptedClient{replies: []scriptedReply{{
		content: `{"recap": "Renamed the field.", "suggestion": "sounds good"}`,
	}}}
	digest, err := digestApp(t, config.TurnDigestConfig{}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), client, toolTurn("Renamed the field everywhere."))
	if err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if digest.Suggestion != "" {
		t.Errorf("suggestion = %q, want none for a turn that asked nothing", digest.Suggestion)
	}
	if strings.Contains(client.sent[0][0].Content, "suggestion") {
		t.Error("system prompt asked for a suggestion the turn could not earn")
	}
}

func TestGenerateTurnDigestSkipsCheapTurnsEntirely(t *testing.T) {
	client := &scriptedClient{replies: []scriptedReply{{content: `{"recap": "x"}`}}}
	summary := TurnSummary{Prompt: "what is 2+2", Reply: "4.", Transcript: "[user] what is 2+2\n[assistant] 4."}

	digest, err := digestApp(t, config.TurnDigestConfig{}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), client, summary)
	if err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if !digest.Empty() {
		t.Errorf("digest = %+v, want empty", digest)
	}
	if client.calls != 0 {
		t.Errorf("made %d model calls for a turn with nothing to describe", client.calls)
	}
}

// The recap earns its tokens only on a turn that changed state and did not
// already spell the change out. worthRecapping is the gate that keeps the call
// off the turns that could not produce anything worth showing.
func TestWorthRecapping(t *testing.T) {
	long := strings.Repeat("done. ", digestSelfNarrateCeiling)
	cases := []struct {
		name  string
		s     TurnSummary
		worth bool
	}{
		{"no tools", TurnSummary{Reply: "The answer is 4."}, false},
		{"read-only tools only", TurnSummary{ToolCalls: 3, WriteToolCalls: 0, Reply: "Found it in parse.go."}, false},
		{"wrote state, brief reply", TurnSummary{ToolCalls: 2, WriteToolCalls: 1, Reply: "Done."}, true},
		{"wrote state, long self-narrating reply", TurnSummary{ToolCalls: 2, WriteToolCalls: 1, Reply: long}, false},
		{"long tool-free reply no longer recaps", TurnSummary{WriteToolCalls: 0, Reply: long}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.worthRecapping(); got != tc.worth {
				t.Errorf("worthRecapping() = %v, want %v", got, tc.worth)
			}
		})
	}
}

// A turn that only read and searched has its answer in the reply; a recap would
// only repeat it, so no call is made even though tools ran.
func TestGenerateTurnDigestSkipsReadOnlyTurns(t *testing.T) {
	client := &scriptedClient{replies: []scriptedReply{{content: `{"recap": "x"}`}}}
	summary := TurnSummary{
		Prompt:     "where is the parser",
		Reply:      "It is in internal/parse/parse.go.",
		Transcript: "[user] where is the parser\n[calls grep] {\"pattern\":\"parse\"}\n[tool result] parse.go",
		ToolCalls:  2,
		// WriteToolCalls stays zero: grep and read change nothing.
	}
	digest, err := digestApp(t, config.TurnDigestConfig{}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), client, summary)
	if err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if !digest.Empty() {
		t.Errorf("digest = %+v, want empty for a read-only turn", digest)
	}
	if client.calls != 0 {
		t.Errorf("made %d model calls for a turn that changed nothing", client.calls)
	}
}

func TestGenerateTurnDigestHonoursConfig(t *testing.T) {
	off := false
	client := &scriptedClient{replies: []scriptedReply{{
		content: `{"recap": "Rewrote the parser.", "suggestion": "yes"}`,
	}}}
	digest, err := digestApp(t, config.TurnDigestConfig{Recap: &off}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), client, toolTurn("Done. Update the docs too?"))
	if err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if digest.Recap != "" {
		t.Errorf("recap = %q with recap disabled", digest.Recap)
	}
	if digest.Suggestion != "yes" {
		t.Errorf("suggestion = %q, want it still offered", digest.Suggestion)
	}

	bothOff := &scriptedClient{replies: []scriptedReply{{content: `{"recap": "x", "suggestion": "y"}`}}}
	if _, err := digestApp(t, config.TurnDigestConfig{Recap: &off, Suggest: &off}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), bothOff, toolTurn("Done. Update the docs too?")); err != nil {
		t.Fatalf("GenerateTurnDigest: %v", err)
	}
	if bothOff.calls != 0 {
		t.Errorf("made %d model calls with the whole digest disabled", bothOff.calls)
	}
}

func TestGenerateTurnDigestFailsOpen(t *testing.T) {
	client := &scriptedClient{err: errors.New("provider down")}
	digest, err := digestApp(t, config.TurnDigestConfig{}).
		GenerateTurnDigest(context.Background(), NewSessionContext("test"), client, toolTurn("Done. Ship it?"))
	if err == nil {
		t.Fatal("want the provider error reported to the caller")
	}
	if !digest.Empty() {
		t.Errorf("digest = %+v, want empty on failure", digest)
	}
}

func TestParseTurnDigestTolerance(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		recap      string
		suggestion string
	}{
		{"bare object", `{"recap":"a","suggestion":"b"}`, "a", "b"},
		{"fenced", "```json\n{\"recap\":\"a\",\"suggestion\":\"\"}\n```", "a", ""},
		{"prose around it", "Sure:\n{\"recap\":\"a\",\"suggestion\":\"b\"}\nHope that helps.", "a", "b"},
		{"multi-line recap collapses", "{\"recap\":\"a\\nb\",\"suggestion\":\"\"}", "a b", ""},
		{"multi-line suggestion keeps the first", "{\"recap\":\"\",\"suggestion\":\"do a\\ndo b\"}", "", "do a"},
		{"not json", "I could not do that", "", ""},
		{"empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTurnDigest(tc.raw)
			if got.Recap != tc.recap || got.Suggestion != tc.suggestion {
				t.Errorf("parseTurnDigest(%q) = %+v, want {%q %q}", tc.raw, got, tc.recap, tc.suggestion)
			}
		})
	}
}

func TestAsksUserIgnoresQuestionsFarFromTheEnd(t *testing.T) {
	if !asksUser("All set. Want me to run the suite?") {
		t.Error("a closing question is a question")
	}
	if !asksUser("好了。要我顺便跑一下测试吗？") {
		t.Error("a full-width question mark is a question")
	}
	if asksUser("Why is it slow? Because the index was missing." + strings.Repeat(" fixed.", 200)) {
		t.Error("a rhetorical question early in a long answer is not asking the user")
	}
}

func TestClippedTranscriptBoundsWhatTheDigestIsShown(t *testing.T) {
	huge := strings.Repeat("x", maxDigestMessageRunes*3)
	out := clippedTranscript([]llm.Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{Name: "read_file", Arguments: `{"path":"big.txt"}`}}},
		{Role: "tool", Content: huge},
	})
	if strings.Contains(out, huge) {
		t.Error("a large tool result reached the digest prompt unclipped")
	}
	if !strings.Contains(out, "[calls read_file]") {
		t.Errorf("tool call missing from transcript:\n%s", out)
	}
}

// A write turn re-sends the file body twice — once in the write's arguments and
// once when it was read — and neither is what a recap describes. The digest
// transcript must keep the path and drop the body.
func TestClippedTranscriptStripsWritePayloads(t *testing.T) {
	body := strings.Repeat("line of code\n", 500)
	out := clippedTranscript([]llm.Message{
		{Role: "user", Content: "add a helper"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{Name: "write_file", Arguments: `{"path":"helper.go","content":` + mustJSONString(body) + `}`},
		}},
		{Role: "tool", Content: "wrote 500 lines to helper.go"},
	})
	if strings.Contains(out, "line of code\nline of code") {
		t.Errorf("the file body reached the digest prompt:\n%s", out)
	}
	if !strings.Contains(out, `"path":"helper.go"`) {
		t.Errorf("the path a recap names was dropped:\n%s", out)
	}
	if !strings.Contains(out, "chars") {
		t.Errorf("the stripped payload left no size marker:\n%s", out)
	}
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
