package agentapp

import (
	"strings"
	"testing"
)

// A run a Space Assistant started is told whom it works for in its own layer;
// any other run hears nothing about a requester.
func TestRequesterPromptLayerNamesTheVerifiedPerson(t *testing.T) {
	dir := t.TempDir()
	if without := BuildEffectiveSystemPrompt(dir, "m", "", PromptCapabilities{}); strings.Contains(without, "Who this run is for") {
		t.Error("a run without a requester must not get the layer")
	}
	with, layers := BuildSystemPromptWithLayers(dir, "m", "", PromptCapabilities{Requester: &Requester{Name: "Bob Lee", Email: "bob@example.com"}})
	for _, want := range []string{"# Who this run is for", "- Name: Bob Lee", "- Email: bob@example.com", "not verified"} {
		if !strings.Contains(with, want) {
			t.Errorf("prompt lacks %q:\n%s", want, with)
		}
	}
	found := false
	for _, l := range layers {
		found = found || l.Name == "requester"
	}
	if !found {
		t.Error("the layer should be traced")
	}
}
