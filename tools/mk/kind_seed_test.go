package main

import (
	"strings"
	"testing"
)

// The catalog table is aligned with spaces and a model name may contain them,
// so the columns are read from the header rather than split on whitespace.
func TestParseCatalogIDs(t *testing.T) {
	output := strings.Join([]string{
		`time=2026-08-23T10:00:00Z level=INFO msg="connected to mysql"`,
		"ID                    NAME             PROVIDER           MODEL                 API URL                      ENABLED",
		"gsyt7at6cjfr33d73mta  GPT-5.6 Luna     openai_compatible  openai/gpt-5.6-luna   https://openrouter.ai/api/v1  yes",
		"a2b3c4d5e6f7g2h3i4j5  Qwen3 8B local   ollama             qwen3:8b              http://host.docker.internal:11434  no",
		"",
	}, "\n")

	ids := parseCatalogIDs(output)
	if len(ids) != 2 {
		t.Fatalf("parseCatalogIDs() returned %d rows, want 2: %v", len(ids), ids)
	}
	if got := ids["GPT-5.6 Luna"]; got != "gsyt7at6cjfr33d73mta" {
		t.Errorf("a name with a space resolved to %q", got)
	}
	if got := ids["Qwen3 8B local"]; got != "a2b3c4d5e6f7g2h3i4j5" {
		t.Errorf("second row resolved to %q", got)
	}
}

func TestParseCatalogIDsEmptyCatalog(t *testing.T) {
	if ids := parseCatalogIDs("The catalog is empty. Add one with: buildmax-server model add --help"); len(ids) != 0 {
		t.Errorf("an empty catalog parsed as %v", ids)
	}
}

func TestAddedModelPattern(t *testing.T) {
	output := strings.Join([]string{
		"Added model gsyt7at6cjfr33d73mta (GPT-5.6 Luna)",
		"",
		`It is available immediately to signed-in users as "GPT-5.6 Luna".`,
		"",
		"  llm:",
		"    default_model: GPT-5.6 Luna",
		"",
	}, "\n")
	match := addedModelPattern.FindStringSubmatch(output)
	if match == nil {
		t.Fatal("the add command's output no longer yields a model ID")
	}
	if match[1] != "gsyt7at6cjfr33d73mta" || match[2] != "GPT-5.6 Luna" {
		t.Errorf("matched %q and %q", match[1], match[2])
	}
}

// A local runtime's address is loopback in the local settings file, which inside a
// pod is the pod. Everything else has to be left exactly as configured.
func TestKindReachableURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"http://localhost:11434", "http://" + kindHostAddress + ":11434"},
		{"http://127.0.0.1:1234/v1", "http://" + kindHostAddress + ":1234/v1"},
		{"http://localhost", "http://" + kindHostAddress},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api/v1"},
		{"", ""},
	}
	for _, c := range cases {
		if got := kindReachableURL(c.in, "test"); got != c.want {
			t.Errorf("kindReachableURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A managed entry names a catalog model, which is what seeding creates; there
// is nothing in it for a catalog to hold.
func TestDirectSettingsModelsDropsManaged(t *testing.T) {
	direct := directSettingsModels([]settingsModel{
		{id: "openai/gpt-5.6-luna", name: "Luna"},
		{id: "default", name: "Space Default", transport: "buildmax"},
	})
	if len(direct) != 1 || direct[0].id != "openai/gpt-5.6-luna" {
		t.Errorf("directSettingsModels() kept %+v", direct)
	}
}

func TestParseSettingsModelsReadsEveryCatalogField(t *testing.T) {
	text := `models:
  - model: anthropic/claude-sonnet-5
    name: Claude Sonnet 5
    provider: anthropic
    api_url: https://api.anthropic.com
    api_key: sk-test
    context_window: 1000000
    call_timeout: 300
    max_tokens: 8192
    reasoning: medium
    vision: true
    cache_control:
      mode: force
      ttl: 1h
    pricing:
      currency: USD
      input_per_mtok: "3"
      cache_read_per_mtok: "0.3"
      cache_write_per_mtok: "3.75"
      output_per_mtok: "15"

  - model: default
    name: Space Default
    transport: buildmax
    server_url: http://localhost:5678
    space_id: tm_example
`
	models := parseSettingsModels(text)
	if len(models) != 2 {
		t.Fatalf("parsed %d models, want 2", len(models))
	}
	got := models[0]
	want := settingsModel{
		id: "anthropic/claude-sonnet-5", name: "Claude Sonnet 5", provider: "anthropic",
		apiURL: "https://api.anthropic.com", apiKey: "sk-test",
		contextWindow: 1000000, callTimeout: 300, maxTokens: 8192,
		reasoning: "medium", vision: true,
		cacheMode: "force", cacheTTL: "1h",
		pricing: settingsPricing{
			currency: "USD", inputPerMTok: "3", cacheReadPerMTok: "0.3",
			cacheWritePerMTok: "3.75", outputPerMTok: "15",
		},
	}
	if got != want {
		t.Errorf("parsed %+v, want %+v", got, want)
	}
	if !models[1].isManaged() {
		t.Error("a transport: buildmax entry did not parse as managed")
	}
}

// Seeding is the only path that turns a local settings entry into a
// catalog row, so a field it forgets is a difference between what a
// contributor runs locally and what the kind deployment answers with.
func TestKindCatalogModelArgsCarryEveryConfiguredField(t *testing.T) {
	m := settingsModel{
		id: "anthropic/claude-sonnet-5", provider: "anthropic",
		apiURL: "https://api.anthropic.com", apiKey: "sk-test",
		contextWindow: 1000000, callTimeout: 300, maxTokens: 8192,
		reasoning: "medium", vision: true,
		cacheMode: "force", cacheTTL: "1h",
		pricing: settingsPricing{
			currency: "USD", inputPerMTok: "3", cacheReadPerMTok: "0.3",
			cacheWritePerMTok: "3.75", outputPerMTok: "15",
		},
	}
	args := kindCatalogModelArgs(m, "Claude Sonnet 5")
	line := strings.Join(args, " ")
	for _, want := range []string{
		"--name Claude Sonnet 5", "--model anthropic/claude-sonnet-5",
		"--api-url https://api.anthropic.com", "--provider anthropic",
		"--api-key -", "--context-window 1000000", "--call-timeout 300",
		"--max-tokens 8192", "--reasoning medium", "--cache-mode force",
		"--cache-ttl 1h", "--currency USD", "--input-price 3",
		"--cache-read-price 0.3", "--cache-write-price 3.75",
		"--output-price 15", "--vision",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("model add command is missing %q: %s", want, line)
		}
	}
	// The key rides standard input; an argument would show in process listings.
	if strings.Contains(line, "sk-test") {
		t.Errorf("the key is on the command line: %s", line)
	}
}

// A price list is validated as a set, so a currency-less one must not reach the
// command as loose rates it will reject.
func TestKindCatalogModelArgsSkipPricesWithoutACurrency(t *testing.T) {
	m := settingsModel{id: "x", pricing: settingsPricing{inputPerMTok: "3"}}
	if line := strings.Join(kindCatalogModelArgs(m, "X"), " "); strings.Contains(line, "--input-price") {
		t.Errorf("a price list with no currency was forwarded: %s", line)
	}
}

// A name already in the catalog keeps its row, and its key is replaced from
// settings.yaml, so a seed run with a wrong key can be corrected by fixing the
// file and seeding again. A placeholder key is never seeded.
func TestSeedKindCatalogRefreshesKeysAndSkipsPlaceholders(t *testing.T) {
	type call struct{ input, args string }
	var calls []call
	previous := kindServerCommand
	kindServerCommand = func(input string, args ...string) (string, error) {
		calls = append(calls, call{input, strings.Join(args, " ")})
		if args[1] == "add" {
			return "Added model newmodelid000000000000 (New)", nil
		}
		return "Replaced the key", nil
	}
	t.Cleanup(func() { kindServerCommand = previous })

	entries, err := seedKindCatalog([]settingsModel{
		{id: "vendor/old", name: "Old", apiKey: "sk-fixed"},
		{id: "vendor/new", name: "New", apiKey: "sk-new"},
		{id: "vendor/placeholder", name: "Placeholder", apiKey: "your-openrouter-api-key"},
	}, map[string]string{"Old": "oldmodelid000000000000"})
	if err != nil {
		t.Fatalf("seedKindCatalog: %v", err)
	}
	if len(entries) != 2 || !entries[0].refreshed || !entries[1].added {
		t.Fatalf("entries = %+v, want Old refreshed and New added", entries)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %+v, want a set-key and an add", calls)
	}
	if calls[0].args != "model set-key --id oldmodelid000000000000" || calls[0].input != "sk-fixed\n" {
		t.Errorf("refresh call = %+v", calls[0])
	}
	if calls[1].input != "sk-new\n" || strings.Contains(calls[1].args, "sk-new") {
		t.Errorf("add call = %+v, want the key on standard input only", calls[1])
	}
}
