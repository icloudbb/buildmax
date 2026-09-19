package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/evaluation/harbor"
)

func runPins(t *testing.T) harbor.Pins {
	t.Helper()
	pins, err := harbor.LoadPins(filepath.Join("..", "..", "evaluation", "harbor", harbor.PinsFile))
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	return pins
}

func builtBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "buildmax-linux-amd64")
	if err := os.WriteFile(path, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// The dataset is 66 tasks and every attempt costs money, so the expensive run
// has to be the one that was asked for.
func TestARunNeedsATaskSelection(t *testing.T) {
	opt := harborRunOptions{model: "openrouter/anthropic/claude-sonnet-5", binary: builtBinary(t)}
	_, err := opt.spec(runPins(t), nil)
	if err == nil {
		t.Fatal("a run with no selection was accepted")
	}
	if !strings.Contains(err.Error(), "--canary") {
		t.Errorf("error = %v, want it to name the ways to select tasks", err)
	}
}

func TestCanaryRunsTheTasksThePinFileNames(t *testing.T) {
	pins := runPins(t)
	opt := harborRunOptions{canary: true, model: "openrouter/anthropic/claude-sonnet-5", binary: builtBinary(t)}
	spec, err := opt.spec(pins, nil)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if len(spec.Tasks) != len(pins.Canary.Tasks) {
		t.Fatalf("canary selected %d task(s), pins.json names %d", len(spec.Tasks), len(pins.Canary.Tasks))
	}
	for i, task := range pins.Canary.Tasks {
		if spec.Tasks[i] != task {
			t.Errorf("canary task %d = %q, want %q", i, spec.Tasks[i], task)
		}
	}
}

func TestCanaryAndAllAreDifferentQuestions(t *testing.T) {
	opt := harborRunOptions{canary: true, all: true, model: "m", binary: builtBinary(t)}
	if _, err := opt.spec(runPins(t), nil); err == nil {
		t.Fatal("--canary --all was accepted, so one of them was silently ignored")
	}
}

// BuildMax has no house model, and the adapter refuses to guess one.
func TestARunNeedsAModel(t *testing.T) {
	opt := harborRunOptions{canary: true, binary: builtBinary(t)}
	if _, err := opt.spec(runPins(t), nil); err == nil {
		t.Fatal("a run with no model was accepted")
	}
}

// The artifact is uploaded from here, so a missing one has to stop the run
// before a container is pulled rather than after.
func TestARunNeedsTheBinaryItWouldUpload(t *testing.T) {
	opt := harborRunOptions{canary: true, model: "m", binary: filepath.Join(t.TempDir(), "absent")}
	_, err := opt.spec(runPins(t), nil)
	if err == nil {
		t.Fatal("a run naming no built binary was accepted")
	}
	if !strings.Contains(err.Error(), "linux/amd64") {
		t.Errorf("error = %v, want it to name the build that fixes it", err)
	}
}

func TestTheOracleNeedsNeitherModelNorBinary(t *testing.T) {
	opt := harborRunOptions{oracle: true, limit: 5, binary: filepath.Join(t.TempDir(), "absent")}
	spec, err := opt.spec(runPins(t), nil)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if spec.Agent != harbor.OracleAgent {
		t.Errorf("agent = %q, want the oracle", spec.Agent)
	}
	if spec.Model != "" || spec.Kwargs != nil {
		t.Errorf("oracle run carries model %q and kwargs %v", spec.Model, spec.Kwargs)
	}
}

func TestKwargsCarryOnlyWhatWasAskedFor(t *testing.T) {
	binary := builtBinary(t)
	opt := harborRunOptions{canary: true, model: "m", binary: binary, reasoning: "high"}
	spec, err := opt.spec(runPins(t), nil)
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if spec.Kwargs["binary"] != binary || spec.Kwargs["reasoning_effort"] != "high" {
		t.Errorf("kwargs = %v", spec.Kwargs)
	}
	// Unset takes the CLI's own default rather than pinning one here.
	if _, ok := spec.Kwargs["max_iterations"]; ok {
		t.Errorf("kwargs = %v, want no iteration cap when none was asked for", spec.Kwargs)
	}
}

// A repeated flag and a comma-separated one are both spellings a caller tries.
func TestTaskFlagAcceptsBothSpellings(t *testing.T) {
	var list stringList
	if err := list.Set("terminal-bench/pypi-server,terminal-bench/fix-git"); err != nil {
		t.Fatal(err)
	}
	if err := list.Set("terminal-bench/crack-7z-hash"); err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("list = %v, want three tasks", list)
	}
}
