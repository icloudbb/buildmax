package agentapp

import (
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
)

// A schedule pins a model by name, so "is it offered" has to answer for the
// name alone — never for an empty one, which would quietly mean the default.
func TestOffersModel(t *testing.T) {
	a := &AgentApp{settings: config.Settings{Models: []config.ModelEntry{
		{Name: "Luna", Model: "openai/gpt-5.6-luna"},
		{Name: "Deep", Model: "anthropic/deep"},
	}}}
	for name, want := range map[string]bool{
		"Luna":                true,
		"openai/gpt-5.6-luna": true,
		"Deep":                true,
		"BuildMax smoke":      false,
		"":                    false,
	} {
		if got := a.OffersModel(name); got != want {
			t.Errorf("OffersModel(%q) = %v, want %v", name, got, want)
		}
	}
	var nilApp *AgentApp
	if nilApp.OffersModel("Luna") {
		t.Error("a nil app offers nothing")
	}
}
