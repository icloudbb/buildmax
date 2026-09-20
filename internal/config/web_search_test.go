package config

import (
	"os"
	"testing"
)

func TestLoadSettingsWebSearchKey(t *testing.T) {
	t.Setenv(EnvKeyBuildmaxHome, t.TempDir())
	if err := os.WriteFile(SettingsPath(), []byte("web_search:\n  api_key: search-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.WebSearch.APIKey != "search-key" {
		t.Errorf("web_search.api_key = %q", settings.WebSearch.APIKey)
	}
}
