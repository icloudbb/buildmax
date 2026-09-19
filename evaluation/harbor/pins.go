// Package harbor holds what BuildMax needs to be measured by Harbor against
// Terminal-Bench 4.0: the versions a result depends on, and the Python agent
// Harbor loads to run the built CLI inside a task container.
//
// Harbor owns task materialization and official verification. BuildMax does not
// run a second copy of the benchmark and does not re-grade its outcomes; see
// docs/design/evaluation-system.md section 14.2.
package harbor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// PinsFile is the committed pin set, relative to this package.
const PinsFile = "pins.json"

// SchemaVersion is the pin format this build understands.
const SchemaVersion = 1

// Pins names every version a Terminal-Bench result depends on besides the
// subject itself.
//
// It exists because a benchmark score is only a measurement if the thing that
// produced it can be named. Harbor, the dataset, and this adapter all move
// independently of BuildMax, and a run that recorded only "Terminal-Bench 4.0"
// could not tell a product regression from a dataset correction or a harness
// upgrade.
type Pins struct {
	SchemaVersion int      `json:"schema_version"`
	Harbor        Harbor   `json:"harbor"`
	Dataset       Dataset  `json:"dataset"`
	Adapter       Adapter  `json:"adapter"`
	Protocol      Protocol `json:"protocol"`
	Canary        Canary   `json:"canary"`
}

// Canary is the named subset a change is validated on before the full
// benchmark is paid for.
//
// It is pinned rather than chosen per run because its job is comparison against
// itself: a subset picked fresh each time measures a different thing every
// time, and the first question after a canary is always whether something got
// worse. The tasks are five cheap, Linux-only Terminal-Bench 4.0 tasks spanning
// different task domains — media, finance/operations, and science — whose
// reference solutions the oracle passes and which finish promptly under a cheap
// model, chosen so a real-model run gives a fast local regression signal rather
// than a leaderboard score. Five tasks cannot estimate a score and are not
// meant to.
type Canary struct {
	Tasks []string `json:"tasks"`
}

// Harbor is the harness release. Its version pins the custom-Agent interface
// this repository's Python agent is written against, which changes between
// releases: 0.22.0 deprecated `--agent-import-path` in favour of `--agent`.
type Harbor struct {
	Version string `json:"version"`
	// Install is the exact command that produces this version, so a reader
	// reproducing a run does not have to know which package index it came from.
	Install string `json:"install"`
}

// Dataset is the task collection, pinned by the immutable ref the benchmark's
// own leaderboard configuration names rather than by a floating "latest".
type Dataset struct {
	Name string `json:"name"`
	Ref  string `json:"ref"`
	// Tasks is how many the release holds. A run that measured a different
	// number measured a different dataset, whatever it was called.
	Tasks int `json:"tasks"`
	// Source is where Ref was read from, so the next reader can check it
	// against the benchmark rather than against this file.
	Source string `json:"source"`
}

// Adapter is this repository's own contribution to a result. It versions
// separately from the product because a change to how the CLI is invoked moves
// scores without the CLI moving.
type Adapter struct {
	Version    int    `json:"version"`
	ImportPath string `json:"import_path"`
}

// Protocol is the benchmark's own comparison policy, copied so a run can be
// checked against it without network access. Deviating from it is allowed and
// reported; deviating from it silently is what makes a number incomparable.
type Protocol struct {
	Attempts   int    `json:"attempts"`
	MaxRetries int    `json:"max_retries"`
	Source     string `json:"source"`
}

// LoadPins reads and validates a pin file.
func LoadPins(path string) (Pins, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Pins{}, fmt.Errorf("read pins: %w", err)
	}
	var pins Pins
	decoder := json.NewDecoder(bytes.NewReader(data))
	// An unknown field is a pin this build does not implement. Ignoring it
	// would report a run as pinned by something it never applied.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pins); err != nil {
		return Pins{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := pins.validate(); err != nil {
		return Pins{}, fmt.Errorf("pins %s: %w", path, err)
	}
	return pins, nil
}

func (p Pins) validate() error {
	if p.SchemaVersion != SchemaVersion {
		return fmt.Errorf("have schema version %d, this build implements %d", p.SchemaVersion, SchemaVersion)
	}
	if p.Harbor.Version == "" {
		return fmt.Errorf("name no Harbor version, so the harness that produced a result is unidentifiable")
	}
	if p.Harbor.Install == "" {
		return fmt.Errorf("name no install command for Harbor %s", p.Harbor.Version)
	}
	if p.Dataset.Name == "" {
		return fmt.Errorf("name no dataset")
	}
	// A floating tag would let two runs a month apart report the same dataset
	// while measuring different tasks, which is the exact comparison section
	// 14.2 forbids.
	if !strings.HasPrefix(p.Dataset.Ref, "sha256:") {
		return fmt.Errorf("pin dataset %s to %q, which is not an immutable sha256 ref", p.Dataset.Name, p.Dataset.Ref)
	}
	if p.Dataset.Tasks <= 0 {
		return fmt.Errorf("state no task count for dataset %s", p.Dataset.Name)
	}
	if p.Adapter.Version <= 0 {
		return fmt.Errorf("give the adapter no version")
	}
	if p.Adapter.ImportPath == "" {
		return fmt.Errorf("name no adapter import path for Harbor to load")
	}
	if p.Protocol.Attempts <= 0 {
		return fmt.Errorf("state no attempt count, so no run could be checked against the benchmark's policy")
	}
	if len(p.Canary.Tasks) == 0 {
		return fmt.Errorf("name no canary tasks, so a change would have to be validated on the full benchmark")
	}
	// The dataset's org qualifies its tasks, and Harbor's task filter matches
	// nothing without it. A bare or misspelled name is a run that silently
	// covers less than it claims — or, with every name wrong, one that refuses
	// to start after the images are already pulled.
	org, _, _ := strings.Cut(p.Dataset.Name, "/")
	for _, task := range p.Canary.Tasks {
		if !strings.HasPrefix(task, org+"/") {
			return fmt.Errorf("canary task %q is not qualified with the dataset's org %q", task, org)
		}
	}
	return nil
}
