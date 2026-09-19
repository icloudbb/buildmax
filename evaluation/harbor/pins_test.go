package harbor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The committed pin file is the one every Terminal-Bench run is measured under,
// so it is loaded here rather than a fixture. A pin file that stopped parsing
// would otherwise be found by the first expensive run instead of by the tests.
func TestCommittedPinsLoad(t *testing.T) {
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	if pins.Dataset.Name != "terminal-bench/terminal-bench" {
		t.Errorf("dataset = %q, want the accepted 4.0 target", pins.Dataset.Name)
	}
	// Terminal-Bench 4.0 dropped and revised tasks from 3.0, so a 3.0 score and
	// a 4.0 score are not comparable as if only the agent had changed. The count
	// is what makes a silent dataset swap visible.
	if pins.Dataset.Tasks != 66 {
		t.Errorf("task count = %d, want 66", pins.Dataset.Tasks)
	}
	if pins.Protocol.Attempts != 5 {
		t.Errorf("attempts = %d, want the leaderboard's 5", pins.Protocol.Attempts)
	}
}

// The Python agent Harbor loads has to be the one the pins name. Nothing else
// checks the two agree, and a rename would send Harbor to an import path that
// does not exist only once a container was already running.
func TestPinnedImportPathResolves(t *testing.T) {
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	module, class, ok := strings.Cut(pins.Adapter.ImportPath, ":")
	if !ok {
		t.Fatalf("import path %q is not module:Class", pins.Adapter.ImportPath)
	}
	path := filepath.Join("src", filepath.FromSlash(strings.ReplaceAll(module, ".", "/"))+".py")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the pinned import path names %s, which does not exist: %v", path, err)
	}
	if !strings.Contains(string(body), "class "+class+"(") {
		t.Errorf("%s defines no class %s", path, class)
	}
}

// The Python adapter states its own version rather than reading pins.json,
// because an installed wheel has no repository beside it. This is what keeps
// the two equal: a bundle records the adapter version from the trial's
// metadata, and a subject naming a version the pin does not know would make an
// adapter change invisible in a comparison that spanned it.
func TestThePythonAdapterVersionMatchesThePin(t *testing.T) {
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	body, err := os.ReadFile(filepath.Join("src", "buildmax_harbor", "__init__.py"))
	if err != nil {
		t.Fatalf("read the adapter package: %v", err)
	}
	want := fmt.Sprintf("ADAPTER_VERSION = %d", pins.Adapter.Version)
	if !strings.Contains(string(body), want) {
		t.Errorf("__init__.py does not declare %q; pins.json says version %d",
			want, pins.Adapter.Version)
	}
}

// The canary is pinned so it compares against itself. A subset chosen fresh
// each run measures something different every time, and the first question
// after one is always whether something got worse.
func TestTheCanarySubsetIsPinnedAndQualified(t *testing.T) {
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	if len(pins.Canary.Tasks) < 2 {
		t.Fatalf("canary has %d task(s); one task exercises one path through the adapter",
			len(pins.Canary.Tasks))
	}
	seen := map[string]bool{}
	for _, task := range pins.Canary.Tasks {
		if seen[task] {
			t.Errorf("canary names %q twice, so it covers less than it appears to", task)
		}
		seen[task] = true
		if !strings.HasPrefix(task, "terminal-bench/") {
			t.Errorf("canary task %q is not qualified; Harbor's filter matches nothing without the org", task)
		}
	}
}

func TestLoadPinsRejectsACanaryTaskTheFilterWouldMiss(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema_version":1,
	  "harbor":{"version":"0.22.0","install":"x"},
	  "dataset":{"name":"terminal-bench/terminal-bench","ref":"sha256:abc","tasks":66,"source":"x"},
	  "adapter":{"version":1,"import_path":"m:C"},
	  "protocol":{"attempts":5,"max_retries":3,"source":"x"},
	  "canary":{"tasks":["pypi-server"]}}`
	path := filepath.Join(dir, "pins.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPins(path)
	if err == nil || !strings.Contains(err.Error(), "not qualified") {
		t.Fatalf("LoadPins error = %v, want a refusal naming the unqualified task", err)
	}
}

func TestLoadPinsRejectsAFloatingDatasetRef(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema_version":1,
	  "harbor":{"version":"0.22.0","install":"uv tool install harbor==0.22.0"},
	  "dataset":{"name":"terminal-bench/terminal-bench","ref":"latest","tasks":66,"source":"x"},
	  "adapter":{"version":1,"import_path":"buildmax_harbor.agent:Buildmax"},
	  "protocol":{"attempts":5,"max_retries":3,"source":"x"}}`
	path := filepath.Join(dir, "pins.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPins(path)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("LoadPins error = %v, want a refusal naming the floating ref", err)
	}
}

func TestLoadPinsRejectsAnUnknownField(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema_version":1,"model":"claude-opus-4-7",
	  "harbor":{"version":"0.22.0","install":"x"},
	  "dataset":{"name":"d","ref":"sha256:abc","tasks":89,"source":"x"},
	  "adapter":{"version":1,"import_path":"m:C"},
	  "protocol":{"attempts":5,"max_retries":3,"source":"x"}}`
	path := filepath.Join(dir, "pins.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPins(path); err == nil {
		t.Fatal("LoadPins accepted a pin this build does not implement")
	}
}
