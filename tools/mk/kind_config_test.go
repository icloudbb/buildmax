package main

import (
	"os"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// The managed smoke config must match the direct one except for what makes it
// managed: the llm block and the worker's managed-inference settings. A
// setting dropped from only one of them leaves the cluster broken in that mode
// after a managed smoke — the missing secret.kek_file made every seeded model
// fail with "no deployment encryption key" once `kind smoke managed` had run.
func TestKindManagedConfigTracksTheDirectOne(t *testing.T) {
	load := func(path string) map[string]any {
		t.Helper()
		raw, err := os.ReadFile("../../" + path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var out map[string]any
		if err := yaml.Unmarshal(raw, &out); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return out
	}
	direct := load("deployment/smoke/server.kind.yaml")
	managed := load("deployment/smoke/server.kind.managed.yaml")

	// What differs on purpose.
	delete(managed, "llm")
	if worker, ok := managed["worker"].(map[string]any); ok {
		delete(worker, "llm")
		delete(worker, "run_token_ttl")
	}

	for key := range direct {
		if !reflect.DeepEqual(direct[key], managed[key]) {
			t.Errorf("%s differs: server.kind.yaml has %v, server.kind.managed.yaml has %v", key, direct[key], managed[key])
		}
	}
	for key := range managed {
		if _, ok := direct[key]; !ok {
			t.Errorf("%s is only in server.kind.managed.yaml", key)
		}
	}
}
