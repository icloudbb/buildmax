package harbor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/evaluation/contract"
)

const jobFixture = "testdata/job"

func loadFixture(t *testing.T) Conversion {
	t.Helper()
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	trials, err := LoadJob(jobFixture)
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return conversion
}

func testOptions() Options {
	return Options{
		Subject: SubjectInput{
			Name: "candidate",
			// The host is the machine that started the containers, and it is
			// the only subject fact a job directory cannot supply.
			Host: contract.HostProfile{OS: "darwin", Arch: "arm64", CPUs: 10},
		},
		ExperimentID: "ex_test",
		CreatedAt:    time.Date(2026, 8, 27, 11, 0, 0, 0, time.UTC),
	}
}

// fixtureTrials loads the job fixture for a test that mutates it before
// converting, which is how the refusals below are reached without a fixture per
// refusal.
func fixtureTrials(t *testing.T) (Pins, []Trial) {
	t.Helper()
	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	trials, err := LoadJob(jobFixture)
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	return pins, trials
}

func bundleFor(t *testing.T, c Conversion, taskID string, index int) contract.TrialBundle {
	t.Helper()
	for _, b := range c.Bundles {
		if b.TaskID == taskID && b.Index == index {
			return b
		}
	}
	t.Fatalf("no bundle for %s attempt %d", taskID, index)
	return contract.TrialBundle{}
}

// Harbor does not number attempts — a trial name is the task plus a random
// suffix — so the index a bundle carries is assigned by the importer. Two
// imports of the same job have to assign the same numbers, or a paired
// comparison would pair different attempts on every run.
func TestAttemptsAreNumberedStablyPerTask(t *testing.T) {
	first := loadFixture(t)
	second := loadFixture(t)

	if len(first.Bundles) != 4 {
		t.Fatalf("imported %d bundles, want 4", len(first.Bundles))
	}
	for i := range first.Bundles {
		a, b := first.Bundles[i], second.Bundles[i]
		if a.TaskID != b.TaskID || a.Index != b.Index || a.TrialID != b.TrialID {
			t.Fatalf("import %d differs: %s#%d (%s) vs %s#%d (%s)",
				i, a.TaskID, a.Index, a.TrialID, b.TaskID, b.Index, b.TrialID)
		}
	}
	// The earlier attempt is attempt zero, and each task numbers from zero of
	// its own — not from a running total across the job.
	if got := bundleFor(t, first, "build-cython-ext", 0); got.TrialID != "build-cython-ext__aaa1111" {
		t.Errorf("attempt 0 is %s, want the one that started first", got.TrialID)
	}
	if got := bundleFor(t, first, "bn-fit-modify", 0); got.TrialID != "bn-fit-modify__ccc3333" {
		t.Errorf("the second task does not number from zero: %s", got.TrialID)
	}
}

func TestAPassingTrialCarriesTheVerifiersVerdict(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "build-cython-ext", 0)

	if b.Status != contract.StatusPassed {
		t.Errorf("status = %s, want passed", b.Status)
	}
	if len(b.Graders) != 1 || b.Graders[0].Name != VerifierGrader {
		t.Fatalf("graders = %+v, want exactly the external verifier", b.Graders)
	}
	g := b.Graders[0]
	if g.Verdict != contract.VerdictPass || !g.Required {
		t.Errorf("verdict = %s required = %v, want a required pass", g.Verdict, g.Required)
	}
	if g.Score == nil || *g.Score != 1 {
		t.Errorf("score = %v, want the recorded reward", g.Score)
	}
	// Harbor names a packaged task `<org>/<name>`, and a task id becomes one
	// path element of the bundle tree. Keeping the org would fail every write.
	if b.TaskID != "build-cython-ext" {
		t.Errorf("task id = %q, want Harbor's name as one path element", b.TaskID)
	}
	// The task checksum is the initial state: BuildMax never materialized this
	// workspace, so the benchmark's own content digest is what says where the
	// attempt began. Harbor writes it as bare hex; a bundle labels its digests.
	if b.InitialStateDigest != "sha256:1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("initial state = %q, want the labelled task checksum", b.InitialStateDigest)
	}
	if b.Duration.Duration() != 4*time.Minute {
		t.Errorf("duration = %s, want 4m", b.Duration.Duration())
	}
}

// Cost is the reason the importer prefers BuildMax's own envelope over Harbor's
// re-encoding of it: the runtime holds cost as integer nano-units and Harbor
// holds it as a float in dollars, which cannot round-trip.
func TestUsageComesFromBuildMaxsOwnEnvelope(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "build-cython-ext", 0)

	if b.Usage.Cost == nil || *b.Usage.Cost != 1234567890 {
		t.Errorf("cost = %v, want the envelope's exact nano-units", b.Usage.Cost)
	}
	if b.Usage.CostIncomplete {
		t.Error("an exactly-priced run is reported as incompletely priced")
	}
	if b.Usage.ToolCalls != 31 {
		t.Errorf("tool calls = %d; Harbor's result carries none, so this can only come from the envelope", b.Usage.ToolCalls)
	}
	if b.Usage.CacheWriteTokens != 1200 {
		t.Errorf("cache write tokens = %d; Harbor's result carries none either", b.Usage.CacheWriteTokens)
	}
	if b.Reply == "" {
		t.Error("the reply is absent, and only the envelope carries one")
	}
	if want := filepath.Join("agent", "sessions", "s1", "traces", "trace-1.jsonl"); b.TracePath != want {
		t.Errorf("trace path = %q, want %q relative to the trial directory", b.TracePath, want)
	}
	// Model calls are in neither the envelope nor Harbor's result — only the
	// trace has them, and reliability reporting needs to tell one expensive
	// call from ten cheap ones.
	if b.Usage.LLMCalls != 1 {
		t.Errorf("model calls = %d, want the one the trace recorded", b.Usage.LLMCalls)
	}
}

// An attempt whose trace did not survive is still a graded attempt. Losing a
// diagnostic must not turn it into an unreadable one, so the count stays zero
// and the gap is recorded next to whatever else the trial reported.
func TestAnUnreadableTraceLeavesTheAttemptGraded(t *testing.T) {
	// A copy, with the trace removed but the envelope left in place: the point
	// is a trial that reported everything except the file the count needs.
	job := filepath.Join(t.TempDir(), "job")
	if err := os.CopyFS(job, os.DirFS(jobFixture)); err != nil {
		t.Fatalf("copy the job fixture: %v", err)
	}
	trace := filepath.Join(job, "build-cython-ext__aaa1111",
		"agent", "sessions", "s1", "traces", "trace-1.jsonl")
	if err := os.Remove(trace); err != nil {
		t.Fatalf("remove the trace: %v", err)
	}

	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	trials, err := LoadJob(job)
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}

	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	b := bundleFor(t, conversion, "build-cython-ext", 0)
	if b.Status != contract.StatusPassed {
		t.Errorf("status = %s, want the verdict the verifier already reached", b.Status)
	}
	if b.Usage.LLMCalls != 0 {
		t.Errorf("model calls = %d, want none counted", b.Usage.LLMCalls)
	}
	if !strings.Contains(b.Error, "model calls uncounted") {
		t.Errorf("error = %q, want the unreadable trace recorded", b.Error)
	}
}

// A subject killed mid-run leaves an empty envelope: the shell creates the file
// with `>` before the binary writes a byte. Treating that as a broken output
// contract threw away every other trial in the job — a canary of six lost five
// good imports to one killed container.
func TestAnEmptyEnvelopeDegradesOneTrialAndNotTheImport(t *testing.T) {
	job := filepath.Join(t.TempDir(), "job")
	if err := os.CopyFS(job, os.DirFS(jobFixture)); err != nil {
		t.Fatalf("copy the job fixture: %v", err)
	}
	envelope := filepath.Join(job, "build-cython-ext__aaa1111", "agent", AgentResultFile)
	if err := os.WriteFile(envelope, nil, 0o644); err != nil {
		t.Fatalf("truncate the envelope: %v", err)
	}

	pins, err := LoadPins(PinsFile)
	if err != nil {
		t.Fatalf("LoadPins: %v", err)
	}
	trials, err := LoadJob(job)
	if err != nil {
		t.Fatalf("LoadJob: %v", err)
	}
	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(conversion.Bundles) != 4 {
		t.Fatalf("imported %d bundles, want all 4", len(conversion.Bundles))
	}

	b := bundleFor(t, conversion, "build-cython-ext", 0)
	if !strings.Contains(b.Error, "wrote no result envelope") {
		t.Errorf("error = %q, want the missing envelope recorded on the trial", b.Error)
	}
	// The verifier still reached a verdict, so the attempt is still scored on
	// it. Only what the subject would have reported about itself is lost.
	if b.Status != contract.StatusPassed {
		t.Errorf("status = %s, want the verdict the verifier reached", b.Status)
	}
	// Usage falls back to Harbor's own record, which holds cost as a float in
	// dollars — so the recovered figure is rounded and says so. Reporting it as
	// exact would be the one thing worse than reporting nothing.
	if b.Usage.Cost == nil || !b.Usage.CostIncomplete {
		t.Errorf("usage = %+v, want Harbor's rounded cost marked incomplete", b.Usage)
	}
}

// A run that spent its iteration budget still produced a workspace, and the
// verifier judged it. The outcome is that verdict; the budget is the failure
// class, so a reader can tell a subject that failed the task from one that was
// still working when the money ran out.
func TestTheIterationCapIsAFailureClassNotAStatus(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "build-cython-ext", 1)

	if b.Status != contract.StatusFailed {
		t.Errorf("status = %s, want failed: the verifier reached a verdict", b.Status)
	}
	if b.FailureClass != "iteration-cap" {
		t.Errorf("failure class = %q, want iteration-cap", b.FailureClass)
	}
	if !b.Status.Scored() {
		t.Error("the attempt is unscored, so a real failure would not count")
	}
}

// Harbor runs the tests against whatever a cut-off agent left behind, so a
// timed-out attempt arrives carrying both an exception and a verdict. Reporting
// it as a plain failure would say execution completed and a grader said no,
// when execution did not complete — and a report of "could not do these" reads
// as a different problem from "ran out of time on these".
//
// Both statuses are scored, so nothing moves in or out of the pass rate.
func TestATimedOutAttemptWithAVerdictIsReportedAsTimedOut(t *testing.T) {
	pins, trials := fixtureTrials(t)
	for i, tr := range trials {
		if tr.Result.TaskName != "terminal-bench/build-cython-ext" {
			continue
		}
		// The attempt that already fails its verifier, now cut off at its budget.
		if tr.Result.VerifierResult != nil && tr.Result.VerifierResult.Rewards["reward"] == 0 {
			trials[i].Result.ExceptionInfo = &ExceptionInfo{
				Type:    "AgentTimeoutError",
				Message: "Agent execution timed out after 18.0 seconds",
			}
		}
	}

	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	b := bundleFor(t, conversion, "build-cython-ext", 1)
	if b.Status != contract.StatusTimedOut {
		t.Errorf("status = %s, want timed_out", b.Status)
	}
	if !b.Status.Scored() {
		t.Error("a timed-out attempt must stay in the pass rate's denominator")
	}
	// The verdict is still recorded; only the status it produced is overridden.
	if b.Graders[0].Verdict != contract.VerdictFail {
		t.Errorf("verdict = %s, want the verifier's own fail", b.Graders[0].Verdict)
	}
}

// A passing verdict is left alone whatever happened to the process afterwards:
// the work was done, and how the run ended does not unmake it.
func TestAPassingVerdictSurvivesATimeout(t *testing.T) {
	pins, trials := fixtureTrials(t)
	for i, tr := range trials {
		if tr.Result.VerifierResult != nil && tr.Result.VerifierResult.Rewards["reward"] == 1 {
			trials[i].Result.ExceptionInfo = &ExceptionInfo{Type: "AgentTimeoutError", Message: "cut off"}
		}
	}

	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	b := bundleFor(t, conversion, "build-cython-ext", 0)
	if b.Status != contract.StatusPassed {
		t.Errorf("status = %s, want passed", b.Status)
	}
}

// Grading that could not finish leaves the attempt unscored rather than failed.
// Section 7.4: a trial the harness could not judge says nothing about
// capability, and counting it as a failure understates the subject in
// proportion to how unreliable the harness was that day.
func TestAVerifierTimeoutIsAHarnessFaultNotAFailure(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "bn-fit-modify", 0)

	if b.Status != contract.StatusGraderError {
		t.Errorf("status = %s, want grader_error", b.Status)
	}
	if !b.Status.HarnessFault() {
		t.Error("a verifier timeout is not being counted against the harness")
	}
	if b.Graders[0].Verdict != contract.VerdictUnknown || b.Graders[0].Error == "" {
		t.Errorf("grader = %+v, want an explicit unknown with a reason", b.Graders[0])
	}
}

func TestAContainerThatNeverStartedIsInfrastructure(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "broken-env", 0)

	if b.Status != contract.StatusInfrastructureError {
		t.Errorf("status = %s, want infrastructure_error", b.Status)
	}
	if b.FailureClass != "EnvironmentStartTimeoutError" {
		t.Errorf("failure class = %q, want Harbor's own type", b.FailureClass)
	}
	// No envelope and no agent_result: the subject reported nothing because it
	// never ran. Zero usage is the truth here, not a lost number.
	if b.Usage.PromptTokens != 0 || b.Usage.Cost != nil {
		t.Errorf("usage = %+v, want nothing recorded for a trial that never ran", b.Usage)
	}
}

func TestStatusForExceptionKeepsTheTaxonomyApart(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want contract.TrialStatus
	}{
		{"AgentTimeoutError", contract.StatusTimedOut},
		{"AgentSetupTimeoutError", contract.StatusTimedOut},
		{"VerifierTimeoutError", contract.StatusGraderError},
		{"EnvironmentStartTimeoutError", contract.StatusInfrastructureError},
		{"NonZeroAgentExitCodeError", contract.StatusAgentError},
		{"ApiRateLimitError", contract.StatusAgentError},
		{"ModelNotFoundError", contract.StatusAgentError},
		// Unknown stays unknown. Guessing agent_error would let a fault this
		// build has never seen count against the subject.
		{"SomethingHarborAddedLater", contract.StatusInfrastructureError},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if got := statusForException(tc.kind); got != tc.want {
				t.Errorf("statusForException(%q) = %s, want %s", tc.kind, got, tc.want)
			}
		})
	}
}

// The subject tuple is what makes an external number a qualification result
// rather than a headline percentage. Section 14.2 lists what it has to name.
func TestTheSubjectNamesEveryVersionAResultDependsOn(t *testing.T) {
	c := loadFixture(t)
	s := c.Subject

	if s.Execution.Surface != contract.SurfaceHarbor {
		t.Errorf("surface = %s, want harbor", s.Execution.Surface)
	}
	if s.Execution.AdapterVersion == 0 {
		t.Error("the adapter version is unrecorded, so a comparison could span an adapter change")
	}
	if s.Model.Target != "anthropic/claude-opus-4-7" {
		t.Errorf("model = %q, want the provider-qualified name the run was given", s.Model.Target)
	}
	if s.Model.Transport != "anthropic" {
		t.Errorf("transport = %q; it must come from what the adapter resolved", s.Model.Transport)
	}
	if s.Model.Reasoning != "high" {
		t.Errorf("reasoning = %q, want the effort the run used", s.Model.Reasoning)
	}
	// The same binary against the same model is two subjects at two window
	// sizes: the smaller one compacts sooner, so what it scores is the model
	// plus a limit. A manifest that omits the window pairs them as one.
	if s.Model.ContextWindow != 200000 {
		t.Errorf("context window = %d, want the one the trial ran at", s.Model.ContextWindow)
	}
	if s.Model.MaxOutput != 32000 {
		t.Errorf("max output = %d, want the cap the trial ran under", s.Model.MaxOutput)
	}
	// All three come from evidence. The digest is what the adapter hashed
	// before uploading it; the version and commit are what the binary itself
	// reported inside the container. None of it is asserted by whoever ran the
	// import, who could assert the wrong thing with nothing able to check it.
	if s.Build.ArtifactDigest == "" {
		t.Error("no artifact digest: the result would name a revision rather than the thing that ran")
	}
	if s.Build.Version != "1.4.0" || s.Build.Commit != "abc1234" {
		t.Errorf("build = %q/%q, want the version the binary reported in the container",
			s.Build.Version, s.Build.Commit)
	}
	if !strings.HasPrefix(s.Dataset.Digest, "sha256:") {
		t.Errorf("dataset digest = %q, want the pinned immutable ref", s.Dataset.Digest)
	}
	if !strings.Contains(s.Dataset.Version, "harbor ") {
		t.Errorf("dataset version = %q, want the harness release that resolved the ref", s.Dataset.Version)
	}
	if s.Policy.Sandboxed == nil || *s.Policy.Sandboxed {
		t.Errorf("sandboxed = %v, want an explicit false from the adapter", s.Policy.Sandboxed)
	}
	if s.ID == "" {
		t.Error("the subject carries no content identity, so two runs could not be paired honestly")
	}
}

// Every bundle in one import measures the same configuration. A subject that
// varied per trial would let a comparison pair attempts that were not the same
// experiment.
func TestEveryBundleNamesTheSameSubject(t *testing.T) {
	c := loadFixture(t)
	for _, b := range c.Bundles {
		if b.SubjectID != c.Subject.ID {
			t.Errorf("bundle %s names subject %s, want %s", b.TrialID, b.SubjectID, c.Subject.ID)
		}
		if b.Suite == "" || b.Domain != contract.DomainCapability {
			t.Errorf("bundle %s: suite %q domain %q", b.TrialID, b.Suite, b.Domain)
		}
	}
}

// Filing a 3.0 run under the 4.0 pin would produce exactly the comparison
// section 14.2 forbids: the two differ by corrected tasks, not only by the
// agent.
func TestAJobFromAnotherDatasetIsRefused(t *testing.T) {
	pins, trials := fixtureTrials(t)
	other := "terminal-bench/terminal-bench-3"
	trials[0].Result.Source = &other

	_, err := Convert(trials, pins, testOptions())
	if err == nil || !strings.Contains(err.Error(), "pinned to") {
		t.Fatalf("Convert error = %v, want a refusal naming the pin", err)
	}
}

// An adapter older than this importer does not record the protocol it resolved,
// and deriving it here from Harbor's provider slug would re-implement the
// adapter's mapping in a second language.
func TestATrialWithoutTheAdaptersSubjectFactsIsRefused(t *testing.T) {
	pins, trials := fixtureTrials(t)
	for _, tr := range trials {
		if tr.Result.AgentResult != nil {
			delete(tr.Result.AgentResult.Metadata, metaProvider)
		}
	}

	_, err := Convert(trials, pins, testOptions())
	if err == nil || !strings.Contains(err.Error(), "buildmax_provider") {
		t.Fatalf("Convert error = %v, want a refusal naming the missing fact", err)
	}
}

func TestATrialWithoutAnArtifactDigestIsRefused(t *testing.T) {
	pins, trials := fixtureTrials(t)
	for _, tr := range trials {
		if tr.Result.AgentResult != nil {
			delete(tr.Result.AgentResult.Metadata, metaArtifact)
		}
	}

	_, err := Convert(trials, pins, testOptions())
	if err == nil || !strings.Contains(err.Error(), metaArtifact) {
		t.Fatalf("Convert error = %v, want a refusal naming the missing digest", err)
	}
}

// A job can begin with an attempt whose container never started. That one knows
// nothing about the subject, and reading it would refuse an import every other
// attempt in the same job could have supplied.
func TestTheSubjectComesFromTheFirstTrialThatReportedAnything(t *testing.T) {
	pins, trials := fixtureTrials(t)
	trials[0].Result.AgentResult = nil

	conversion, err := Convert(trials, pins, testOptions())
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if conversion.Subject.Model.Transport != "anthropic" {
		t.Errorf("transport = %q, want the one a later trial recorded", conversion.Subject.Model.Transport)
	}
}

// Metadata retention is what makes an export bounded rather than a copy of a
// private workspace. The numbers stay; the model's own words do not.
func TestMetadataRetentionDropsTheReply(t *testing.T) {
	pins, trials := fixtureTrials(t)
	opt := testOptions()
	opt.Retention = contract.RetentionMetadata

	conversion, err := Convert(trials, pins, opt)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	for _, b := range conversion.Bundles {
		if b.Reply != "" {
			t.Errorf("bundle %s kept a reply at metadata retention", b.TrialID)
		}
		if b.Retention != contract.RetentionMetadata {
			t.Errorf("bundle %s records retention %q", b.TrialID, b.Retention)
		}
	}
	// The reply is what retention removes; the usage is what it is for.
	if got := conversion.Bundles[0]; got.Usage.PromptTokens == 0 && got.Usage.ToolCalls == 0 {
		t.Error("metadata retention dropped the usage as well as the free text")
	}
}

func TestTheReproductionCommandIsDeterministic(t *testing.T) {
	b := bundleFor(t, loadFixture(t), "build-cython-ext", 0)
	joined := strings.Join(b.Reproduce.Command, " ")

	for _, want := range []string{
		"harbor run",
		"terminal-bench/terminal-bench@sha256:",
		"buildmax_harbor.agent:Buildmax",
		// Qualified, because that is what the filter matches: Harbor lists a
		// packaged task as <org>/<name> and refuses a bare one.
		"--include-task-name terminal-bench/build-cython-ext",
		"--ak binary=bin/buildmax-linux-amd64",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("reproduce command %q does not carry %q", joined, want)
		}
	}
	// Kwargs come out of a map, so they are sorted: an unsorted command would
	// differ between imports of the same job.
	if !strings.Contains(joined, "--ak binary=bin/buildmax-linux-amd64 --ak max_iterations=1000 --ak reasoning_effort=high") {
		t.Errorf("reproduce command %q does not order its kwargs", joined)
	}
	if b.Reproduce.Note == "" {
		t.Error("a reconstructed command is not labelled as one")
	}
}
