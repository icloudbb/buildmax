package agentapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/util"
)

// Bounds on what the digest is shown and what it may write back. The transcript
// caps exist because a turn that read a large file would otherwise send it a
// second time to be summarized, which costs more than the summary is worth.
//
// Tool-call arguments and tool results are clipped far harder than the prose
// around them, because a recap names the action, not the payload it carried:
// the file body a write sends in its arguments, and the file a read returns in
// its result, are a turn's largest tokens and the least of what a recap is
// about. Assistant reasoning and the user's message keep the larger bound —
// they are the "why" a recap is asked to explain.
const (
	maxDigestTranscriptRunes = 12000
	maxDigestMessageRunes    = 1200
	maxDigestArgsRunes       = 200
	maxDigestToolResultRunes = 400
	maxRecapRunes            = 400
	maxSuggestionRunes       = 160
	// digestSelfNarrateCeiling is how long a reply has to get before a turn that
	// changed something is assumed to have already narrated the change. Above it
	// a recap would mostly repeat the reply, so the call is not worth its tokens.
	digestSelfNarrateCeiling = 1500
)

// TurnSummary is what one finished turn looked like, reduced to values.
//
// It is captured inside the turn rather than read back from the session
// afterwards: the session has exactly one writer, and a surface that went back
// for the messages once the next turn had started would race it.
type TurnSummary struct {
	// Prompt is what started the turn: the user's message, or the rendered
	// background event for a turn nobody typed.
	Prompt string
	// Reply is the assistant's final text.
	Reply string
	// Transcript is this turn's messages, clipped per message and in order.
	Transcript string
	// ToolCalls is how many tool calls the turn made.
	ToolCalls int
	// WriteToolCalls is how many of those actually changed state. A recap names
	// what changed, so a turn that changed nothing has nothing for it to say.
	WriteToolCalls int
}

// TurnDigest is what the side call produced. Either field may be empty, which
// means the turn earned nothing to say there.
type TurnDigest struct {
	// Recap is a short account of what the turn did, for the user only.
	Recap string
	// Suggestion is the answer the user is likely about to give, written in
	// their voice. Empty unless the reply ended by asking them to decide.
	Suggestion string
}

// Empty reports whether the digest carries nothing worth showing.
func (d TurnDigest) Empty() bool { return d.Recap == "" && d.Suggestion == "" }

const digestSystemPrompt = `You report on a coding agent's turn that has just finished. You are writing for the user watching the terminal, not for the agent: nothing you write is added to the conversation or read back by the model.

Reply with one JSON object and nothing else:

%s

Use "" for a field that does not apply. Never invent work the transcript does not show.`

const digestRecapRule = `"recap": at most two sentences, past tense, naming what actually changed — files written, commands run, what was found. Say why when the turn made a choice the user did not ask for. Use "" when the reply already is its own summary and a recap would only repeat it.`

const digestSuggestRule = `"suggestion": one line, under 120 characters, written as the user in first person, ready to send as their next message. Produce one only when the reply ends by asking the user to choose or confirm something, and only when the transcript makes one answer clearly the likely one. Use "" for anything else — a turn that asked nothing, or a question whose answer only the user knows.`

// GenerateTurnDigest asks the model to describe the finished turn. It returns
// the empty digest with no error whenever there is nothing to ask about, so a
// caller can call it unconditionally.
//
// It spends money, so it is bounded twice over: the config may switch either
// half off, and worthRecapping/asksUser keep the call off turns that could not
// produce anything. The usage it does spend is folded into the session like
// the title call's, because a cost the user cannot see in /stats is a cost
// BuildMax reported wrong.
func (a *AgentApp) GenerateTurnDigest(ctx context.Context, sess *SessionContext, client llm.LLMClient, summary TurnSummary) (TurnDigest, error) {
	if a == nil || client == nil || strings.TrimSpace(summary.Reply) == "" {
		return TurnDigest{}, nil
	}
	cfg := a.settings.Agent.TurnDigest
	wantRecap := config.TurnDigestRecap(cfg) && summary.worthRecapping()
	wantSuggestion := config.TurnDigestSuggest(cfg) && asksUser(summary.Reply)
	if !wantRecap && !wantSuggestion {
		return TurnDigest{}, nil
	}

	completion, err := client.ChatCompletionBlocking(ctx, llm.Request{
		Messages: digestMessages(summary, wantRecap, wantSuggestion),
		// A one-shot call about a turn, never sent again: asking for a cache
		// write here would be charged and never read.
		Profile: llm.ProfileProbe,
	})
	if err != nil {
		return TurnDigest{}, fmt.Errorf("turn digest: %w", err)
	}
	if sess != nil {
		addSessionCost(sess, completion.Usage, a.pricingFor(sess))
	}

	digest := parseTurnDigest(completion.Content)
	// The model was asked for only the fields that were wanted; drop anything
	// it volunteered anyway rather than showing what the config turned off.
	if !wantRecap {
		digest.Recap = ""
	}
	if !wantSuggestion {
		digest.Suggestion = ""
	}
	return digest, nil
}

// digestMessages asks only for the fields the caller can use. A field named in
// the shape gets filled in whether or not it was wanted — a model handed a
// "suggestion" slot writes a suggestion — so the shape is built rather than
// fixed, and the unwanted half costs no output tokens either.
func digestMessages(summary TurnSummary, wantRecap, wantSuggestion bool) []llm.Message {
	var fields, rules []string
	if wantRecap {
		fields = append(fields, `"recap": "..."`)
		rules = append(rules, digestRecapRule)
	}
	if wantSuggestion {
		fields = append(fields, `"suggestion": "..."`)
		rules = append(rules, digestSuggestRule)
	}
	system := fmt.Sprintf(digestSystemPrompt, "{"+strings.Join(fields, ", ")+"}") +
		"\n\n" + strings.Join(rules, "\n\n")
	return []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "The turn that just finished:\n\n" + summary.Transcript},
	}
}

// worthRecapping keeps the call off turns a recap could not improve on, so the
// tokens are spent only when there is likely something worth saying.
//
// A recap names what changed. A turn that changed no state — one that answered
// from what it knew, or only read and searched — has nothing for it to name;
// the reply is the whole deliverable and a recap could only repeat it. And when
// a turn that did change state answered at length, the reply has room to have
// already named the change, so paying to say it again is waste. The gate leaves
// the redundant-but-shorter case to the model, which is told to return "" when
// the reply is already its own summary.
func (s TurnSummary) worthRecapping() bool {
	if s.WriteToolCalls == 0 {
		return false
	}
	return utf8.RuneCountInString(s.Reply) < digestSelfNarrateCeiling
}

// asksUser reports whether the reply ends by putting a question to the user.
// Only the tail is examined: a question asked halfway through a long answer is
// usually rhetorical, and the decision the user has to make is the last thing
// said. Both the ASCII and the full-width question mark count.
func asksUser(reply string) bool {
	tail := reply
	if runes := []rune(reply); len(runes) > 500 {
		tail = string(runes[len(runes)-500:])
	}
	return strings.ContainsAny(tail, "?？")
}

// parseTurnDigest reads the model's JSON object, tolerating the code fence and
// the surrounding prose a model sometimes adds. A reply it cannot read yields
// the empty digest rather than an error: the digest is decoration, and failing
// the turn over it would trade something the user needs for something they do
// not.
func parseTurnDigest(raw string) TurnDigest {
	body := extractJSONObject(raw)
	if body == "" {
		return TurnDigest{}
	}
	var parsed struct {
		Recap      string `json:"recap"`
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return TurnDigest{}
	}
	return TurnDigest{
		Recap:      util.ClipRunes(collapseLines(parsed.Recap), maxRecapRunes),
		Suggestion: util.ClipRunes(firstLine(parsed.Suggestion), maxSuggestionRunes),
	}
}

// extractJSONObject returns the outermost {...} span of s, or "" when there is
// none.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// collapseLines folds a multi-line value onto one line so a recap cannot
// reflow the terminal into a wall of text.
func collapseLines(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// summarizeTurn reduces the messages one turn appended to the values the
// digest call needs.
func summarizeTurn(prompt, reply string, turnMessages []llm.Message, toolCalls, writeToolCalls int) TurnSummary {
	return TurnSummary{
		Prompt:         prompt,
		Reply:          reply,
		Transcript:     clippedTranscript(turnMessages),
		ToolCalls:      toolCalls,
		WriteToolCalls: writeToolCalls,
	}
}

// clippedTranscript renders the turn for the digest prompt. It is not
// note_checkpoint's transcript: that one must not lose detail, because the
// material is about to be discarded, while this one is describing work the
// session still holds and pays per token to look at.
func clippedTranscript(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch {
		case len(m.ToolCalls) > 0:
			if text := strings.TrimSpace(m.Content); text != "" {
				fmt.Fprintf(&b, "[assistant] %s\n", clip(text, maxDigestMessageRunes))
			}
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "[calls %s] %s\n", tc.Name, digestArgs(tc.Arguments))
			}
		case m.Role == "tool":
			fmt.Fprintf(&b, "[tool result] %s\n", clip(m.Content, maxDigestToolResultRunes))
		default:
			if text := strings.TrimSpace(m.Content); text != "" {
				fmt.Fprintf(&b, "[%s] %s\n", m.Role, clip(text, maxDigestMessageRunes))
			}
		}
	}
	// Clipped from the front: when a turn overruns the budget, its later
	// tool calls and its reply are what the recap is about.
	out := strings.TrimSuffix(b.String(), "\n")
	if runes := []rune(out); len(runes) > maxDigestTranscriptRunes {
		out = "[...earlier steps omitted...]\n" + string(runes[len(runes)-maxDigestTranscriptRunes:])
	}
	return out
}

func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return util.ClipRunes(s, limit) + " […]"
}

// bulkyArgKeys are tool-argument fields that carry a payload rather than name
// the action: the file body a write sends, the strings an edit swaps. On a
// write turn — the only kind a recap now runs on — they are the transcript's
// largest tokens and say nothing a recap needs, so they are replaced by their
// size. Everything else in the arguments (the path, the command, the pattern)
// is small and identifying, so it is kept.
var bulkyArgKeys = map[string]bool{
	"content":    true,
	"old_string": true,
	"new_string": true,
}

// digestArgs renders a tool call's arguments for the digest with the payload
// fields stripped to a size marker. Arguments that are not a JSON object, or
// that survive stripping, are clipped like anything else so a pathological call
// cannot flood the prompt.
func digestArgs(raw string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return clip(raw, maxDigestArgsRunes)
	}
	for k, v := range obj {
		if s, ok := v.(string); ok && bulkyArgKeys[k] {
			obj[k] = fmt.Sprintf("…%d chars", utf8.RuneCountInString(s))
		}
	}
	// Marshal sorts keys, so the rendering is stable regardless of the order the
	// model emitted the arguments in.
	trimmed, err := json.Marshal(obj)
	if err != nil {
		return clip(raw, maxDigestArgsRunes)
	}
	return clip(string(trimmed), maxDigestArgsRunes)
}
