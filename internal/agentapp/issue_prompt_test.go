package agentapp

import (
	"strings"
	"testing"
)

// A run with no Issue is never told to use `buildmax issue`: removing the
// in-process tools left the prompt as the only signal that an Issue exists, so
// the layer must appear exactly when the run is working one.
func TestIssuePromptLayerFollowsTheCapability(t *testing.T) {
	dir := t.TempDir()
	without := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{})
	if strings.Contains(without, "buildmax issue") {
		t.Error("a run with no Issue must not be told to use `buildmax issue`")
	}

	with, layers := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{Issue: &IssueContext{}})
	if !strings.Contains(with, "buildmax issue show") || !strings.Contains(with, "buildmax issue comment") {
		t.Error("an issue-linked run should be pointed at `buildmax issue`")
	}
	var found bool
	for _, l := range layers {
		if l.Name == "issue" {
			found = true
		}
	}
	if !found {
		t.Error("the layer should be traced, so a finished run can say what it was told")
	}
}

// A worker run's commands take no id — the bridge resolves the run's one Issue —
// while a local run must name the id it is scoped to. The prompt reflects that.
func TestIssuePromptLayerCarriesTheLocalIDOnly(t *testing.T) {
	dir := t.TempDir()
	worker := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{Issue: &IssueContext{}})
	if strings.Contains(worker, "buildmax issue show i_") {
		t.Error("a worker run's issue commands take no id")
	}

	local := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{Issue: &IssueContext{ID: "i_abc"}})
	if !strings.Contains(local, "buildmax issue show i_abc") || !strings.Contains(local, "buildmax issue comment i_abc") {
		t.Error("a local run should be told the issue id its commands need")
	}
}
