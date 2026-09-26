package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseUpgradeDrillArgs(t *testing.T) {
	opts, err := parseUpgradeDrillArgs([]string{"--from", "v0.2.0-alpha.15", "--to", "0.2.0-alpha.16"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.from != "0.2.0-alpha.15" || opts.to != "0.2.0-alpha.16" {
		t.Errorf("parsed %+v; the v prefix should be dropped from both", opts)
	}
	if opts, err := parseUpgradeDrillArgs(nil); err != nil || opts != (upgradeDrillOptions{}) {
		t.Errorf("no flags = %+v, %v; want both empty so the drill picks its defaults", opts, err)
	}
	for _, args := range [][]string{
		{"--from", "latest"},
		{"--to", "ghcr.io/icloudbb/buildmax:dev"},
		{"0.2.0-alpha.15"},
		{"--bogus"},
	} {
		if _, err := parseUpgradeDrillArgs(args); err == nil {
			t.Errorf("parseUpgradeDrillArgs(%q) accepted it", args)
		}
	}
}

func TestReleaseVersionOrdersTheReleaseLine(t *testing.T) {
	ordered := []string{"0.1.0", "0.2.0-alpha.2", "0.2.0-alpha.9", "0.2.0-alpha.10", "0.2.0-beta.1", "0.2.0-rc.1", "0.2.0", "0.2.1", "1.0.0"}
	for i := range ordered {
		for j := range ordered {
			a, err := parseReleaseVersion(ordered[i])
			if err != nil {
				t.Fatal(err)
			}
			b, err := parseReleaseVersion(ordered[j])
			if err != nil {
				t.Fatal(err)
			}
			if got, want := a.less(b), i < j; got != want {
				t.Errorf("%s < %s = %v, want %v", ordered[i], ordered[j], got, want)
			}
		}
	}
	if _, err := parseReleaseVersion("dev"); err == nil {
		t.Error("a non-release tag parsed")
	}
}

func TestExpectRollback(t *testing.T) {
	source := []string{"a", "b"}
	tests := []struct {
		name      string
		tag       string
		candidate []string
		want      rollbackExpectation
		unknown   string
	}{
		{"same ledger", "0.2.0-alpha.20", []string{"a", "b"}, expectStart, ""},
		{"source predates the refusal but has nothing to refuse", "0.2.0-alpha.14", []string{"a", "b"}, expectStart, ""},
		{"a new migration", "0.2.0-alpha.20", []string{"a", "b", "c"}, expectRefusal, "c"},
		{"the first release with the refusal", newerSchemaRefusalSince, []string{"a", "b", "c"}, expectRefusal, "c"},
		{"a source that predates the refusal", "0.2.0-alpha.15", []string{"a", "c", "b", "d"}, expectUnguarded, "c,d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, unknown, err := expectRollback(tt.tag, source, tt.candidate)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want || strings.Join(unknown, ",") != tt.unknown {
				t.Errorf("expectRollback = %v %q, want %v %q", got, unknown, tt.want, tt.unknown)
			}
		})
	}
}

func TestJudgeRollback(t *testing.T) {
	unknown := []string{"schedule_agent_to_executor"}
	tests := []struct {
		expect  rollbackExpectation
		refused bool
		fails   bool
	}{
		{expectRefusal, true, false},
		{expectRefusal, false, true},
		{expectStart, false, false},
		{expectStart, true, true},
		{expectUnguarded, false, false},
		{expectUnguarded, true, false},
	}
	for _, tt := range tests {
		detail, err := judgeRollback(tt.expect, unknown, tt.refused)
		if (err != nil) != tt.fails {
			t.Errorf("judgeRollback(%v, refused=%v) = %q, %v; want failure %v", tt.expect, tt.refused, detail, err, tt.fails)
		}
		if err == nil && tt.expect != expectStart && !strings.Contains(detail, unknown[0]) {
			t.Errorf("judgeRollback(%v) detail %q does not name the unknown migration", tt.expect, detail)
		}
	}
}

func TestDrillRecordNamesTheFailedStep(t *testing.T) {
	record := drillRecord{
		source:    "ghcr.io/icloudbb/buildmax:0.2.0-alpha.15 sha256:abc",
		candidate: "built from abc1234",
		project:   "buildmax-upgrade-1234",
		started:   time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
		steps: []drillStep{
			{name: "Back up", took: 1500 * time.Millisecond, detail: "dump | volume\nserver.yaml"},
			{name: "Upgrade to the candidate", took: time.Second, err: errors.New("server unhealthy")},
		},
	}
	got := record.markdown()
	for _, want := range []string{
		"| Result | FAILED at Upgrade to the candidate |",
		"| Back up | passed | 2s | dump / volume server.yaml |",
		"| Upgrade to the candidate | FAILED | 1s | server unhealthy |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("record lacks %q:\n%s", want, got)
		}
	}
	record.steps = record.steps[:1]
	if !strings.Contains(record.markdown(), "| Result | passed |") {
		t.Errorf("a record with no failed step does not read passed:\n%s", record.markdown())
	}
}

// TestNewerSchemaRefusalMatchesServer keeps the text the drill looks for in the
// source's log in step with the error the server writes.
func TestNewerSchemaRefusalMatchesServer(t *testing.T) {
	source, err := os.ReadFile("../../internal/infra/db/migration.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `"`+newerSchemaRefusal+`: `) {
		t.Errorf("internal/infra/db/migration.go no longer opens NewerSchemaError with %q", newerSchemaRefusal)
	}
}

func TestFixtureScheduleReadsEitherAPIShape(t *testing.T) {
	for _, tt := range []struct {
		name, body, agent, lastFire string
	}{
		{"executor", `{"executor_kind":"agent","executor_id":"a1","last_fire_ref":"t1"}`, "a1", "t1"},
		{"before schedule_agent_to_executor", `{"agent_id":"a1","last_task_id":"t1"}`, "a1", "t1"},
		{"never fired", `{"executor_kind":"agent","executor_id":"a1","last_fire_ref":null}`, "a1", ""},
		{"a workflow executor is not the agent", `{"executor_kind":"workflow","executor_id":"a1"}`, "", ""},
	} {
		var s fixtureSchedule
		if err := json.Unmarshal([]byte(tt.body), &s); err != nil {
			t.Fatal(err)
		}
		if s.agent() != tt.agent || s.lastFire() != tt.lastFire {
			t.Errorf("%s: agent %q, last fire %q; want %q, %q", tt.name, s.agent(), s.lastFire(), tt.agent, tt.lastFire)
		}
	}
}
