package agent

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// ToolPolicy is the configured override layer: what a user asked for, ahead of
// what a tool declares about itself.
//
// The bool is whether the policy has an opinion at all. It exists because
// ToolActionAllow cannot carry that distinction: at every other layer Allow
// means "abstain, keep resolving", so a policy that returned it could never
// say "allow this, stop asking" — which is the whole point of configuring one.
//
// scope is the call's target when the tool dispatches somewhere (see
// grantScope), so a rule can name one MCP tool rather than every one.
type ToolPolicy interface {
	Check(name, scope string, args map[string]any) (llm.ToolAction, bool)
}

// ApprovalHandler is invoked when the resolved action is ToolActionAsk.
// If nil, Ask collapses to Deny.
//
// ctx is the run's context. A handler blocks a goroutine until a person
// answers, and a cancelled run may never get one -- so it must return on
// ctx.Done() rather than waiting for a prompt nobody will resolve.
//
// target is what ApprovalAllowSession would cover within the tool — an MCP
// server/tool, a browser origin — and the prompt shows it. Empty means the
// grant covers every call of the tool.
type ApprovalHandler interface {
	RequestApproval(ctx context.Context, name string, args map[string]any, target string) ApprovalDecision
}

// interactive reports whether a human can answer a permission prompt. Derived
// from the approval handler rather than carried as its own field: the handler's
// presence already is that fact, and a second source could only disagree.
func (o RunLoopOpts) interactive() bool { return o.Approval != nil }

// AllowAllPolicy returns a policy that defers every decision to the tool's own
// declaration.
func AllowAllPolicy() ToolPolicy { return allowAll{} }

type allowAll struct{}

func (allowAll) Check(_, _ string, _ map[string]any) (llm.ToolAction, bool) {
	return llm.ToolActionAllow, false
}

// defaultMaxRepeatedCalls is the loop guard threshold: the same tool+args combination
// is blocked after this many repetitions within one run.
const defaultMaxRepeatedCalls = 3

// loopGuard tracks per-fingerprint call counts to detect doom loops.
type loopGuard struct {
	counts map[string]int
	max    int
}

func newLoopGuard(max int) *loopGuard {
	return &loopGuard{counts: make(map[string]int), max: max}
}

// exceeded returns true when the tool+args fingerprint has been seen more than max times.
func (g *loopGuard) exceeded(name string, args map[string]any) bool {
	fp := toolFingerprint(name, args)
	g.counts[fp]++
	return g.counts[fp] > g.max
}

func toolFingerprint(name string, args map[string]any) string {
	b, _ := json.Marshal(args)
	sum := md5.Sum(append([]byte(name+":"), b...))
	return fmt.Sprintf("%x", sum)
}
