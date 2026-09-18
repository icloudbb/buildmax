# Harbor / Terminal-Bench 2.1

What BuildMax needs to be measured by [Harbor](https://www.harborframework.com)
against Terminal-Bench 2.1: the versions a result depends on, and the Python
agent Harbor loads to run the built CLI inside a task container.

Harbor owns task materialization and official verification. BuildMax does not
run a second copy of the benchmark and does not re-grade its outcomes. See
[design/evaluation-system.md](../../docs/design/evaluation-system.md) §14.2.

## Why there is Python here

Harbor is a Python package, and its custom-Agent boundary is a Python class.
This directory is the only Python in the repository and it is evaluation
tooling: it is not built, not shipped, not imported by any Go package, and not
part of any `./make check` scope. The Go core and the CLI stay a single binary
with no Python or Node, as [AGENTS.md](../../AGENTS.md) requires.

## Layout

| Path | What it is |
|---|---|
| `pins.json` | Every version a result depends on besides the subject. Read by Go (`pins.go`) and by a human. |
| [`runtime-flow-memo.zh-CN.md`](runtime-flow-memo.zh-CN.md) | Chinese implementation memo tracing a Job from `harbor run` through environment setup, Agent execution, verification, import, and the Kubernetes boundary. |
| `pins.go` | Loader and validator for the above. Refuses a floating dataset ref. |
| `src/buildmax_harbor/agent.py` | The class Harbor loads. Uploads the CLI, writes a trial home, runs one prompt, collects the trace. |
| `src/buildmax_harbor/settings.py` | Renders the trial home's `settings.yaml`. Imports no Harbor code. |
| `src/buildmax_harbor/envelope.py` | Reads BuildMax's print-mode result envelope. Imports no Harbor code. |
| `tests/` | Covers the two harness-free modules, so the credential rendering is checkable without installing Harbor. |
| `run.go` | Builds the `harbor run` command from the pins and starts it. The same builder writes the reproduction command on every bundle. |
| `job.go` | Reads a finished Harbor job directory: one `result.json` and `config.json` per trial. |
| `convert.go` | Turns those into the BuildMax trial contract: subject manifest, status, verdict, usage. |
| `import.go` | Writes the result as a bundle tree the rest of `evaluation/` can read. |

## Importing a finished run

```shell
./make eval harbor --job .artifacts/harbor/jobs/<job>

# Two jobs, paired on task and attempt, for a candidate against a baseline.
./make eval harbor --job runs/new --baseline-job runs/old
```

`./make eval harbor run` ends in this import, so it is needed on its own only
for a job someone else ran, a job imported again under another name, or a
comparison. A job directory is wherever Harbor was told to write one: runs
started here land under `.artifacts/harbor/jobs`, while `harbor run` on its own
defaults `--jobs-dir/-o` to `jobs/` in the working directory.

It builds no CLI and calls no model. The artifact that produced the job is named
by the evidence, not by whatever the tree compiles to now — building one here
could name a binary that never ran. It also measures rather than gates: a task
the subject did not solve is a score, and the command fails only when nothing
could be measured at all.

Running the benchmark and recording it are separate directions. BuildMax starts
the first — `./make eval harbor run` assembles the command and launches Harbor —
but owns none of what happens inside it: Harbor materializes the tasks, its
verifier decides each outcome, and its job directory keeps the trajectories. The
importer copies none of that and rewrites none of it. What it produces is the record that makes an external
result comparable with a local one: the subject tuple a qualification has to
name, and one bundle per attempt.

What each part of a bundle comes from:

| Bundle field | Source |
|---|---|
| Status, verdict | Harbor's verifier. A Terminal-Bench task writes `0` or `1` to `reward.txt`. |
| Initial state | The task checksum. BuildMax never materialized the workspace, so the benchmark's own content digest is what says where the attempt began. |
| Usage, cost, reply, trace | BuildMax's own `buildmax-result.json`, not Harbor's re-encoding of it — Harbor holds cost as a float in dollars, which cannot round-trip the runtime's integer nano-units. |
| Model, transport, reasoning, sandbox | The adapter's own record of what it resolved, carried on the trial's agent metadata. |
| Attempt index | Assigned by the importer. Harbor does not number attempts — a trial name is the task plus a random suffix — so they are numbered per task from a stable ordering by start time. |
| Artifact digest, host | Supplied by the caller. Harbor records the kwarg naming a binary path, which is not the digest of the file that ran. |

Failures stay apart, per
[design/evaluation-system.md](../../docs/design/evaluation-system.md) §7.4. An
agent timeout is the task's budget expiring and counts; a verifier timeout is
grading that could not finish and leaves the attempt unscored; a container that
would not start blames neither. An unknown Harbor exception is recorded as
infrastructure rather than guessed into a failure of the subject.

Reaching the iteration cap is not a status of its own: the verifier still judged
what the run left behind, so the verdict decides and the cap is the failure
class. That is what keeps a spent budget out of the capability reading.

## Running it

Harbor needs Docker (or a cloud sandbox) and a model API key. None of it is a
pull-request gate; these runs are explicit and cost money.

A Harbor Hub account is **not** needed to run the benchmark. Harbor's client is
anonymous when logged out and public reads keep working, so the pinned public
dataset downloads and trials run without one. Sign-in gates the calls that
resolve a user: publishing, `--upload`, org and key management, and private
datasets.

`./make doctor harbor` reports what is missing and prints the command that fixes
each one. Like the rest of doctor it installs nothing. `./make setup harbor`
runs those commands: it installs uv through Astral's own installer, installs the
pinned Harbor, cross-builds the Linux CLI, and finishes by re-running the
doctor report. It will not choose a trial sandbox for you — Docker or a
`DAYTONA_API_KEY` is yours to set up — and it never signs in to Hub.

`./make eval harbor run` starts the benchmark. It builds the command from
`pins.json` — the dataset with its immutable ref, the adapter's import path, and
the `PYTHONPATH` that lets Harbor import it — checks the toolchain the way
`doctor harbor` does, cross-builds the `linux/amd64` CLI if it is missing, and
imports the finished job. Harbor still owns the tasks, the containers, and the
verdict.

```shell
# 0. Prove the environment before spending anything on a model: the oracle runs
#    each task's own reference solution. Nothing to import from it.
./make eval harbor run --oracle --limit 5

# 1. One task.
./make eval harbor run \
  --task terminal-bench/pypi-server \
  --model anthropic/claude-opus-4-7 \
  --reasoning high

# 2. The canary subset pins.json names -- six tasks chosen to exercise
#    different paths through the adapter, not to estimate a score.
./make eval harbor run --canary --model anthropic/claude-opus-4-7

# 3. The whole dataset, at the leaderboard's five attempts. This is the
#    expensive one, which is why --all has to be asked for.
./make eval harbor run --all --attempts 5 --model anthropic/claude-opus-4-7

# 4. Through a gateway. The window and the prices are passed, because the trial
#    home holds only what this command puts in it. They sit after `--`, which
#    is where this command stops reading flags and Harbor starts.
export OPENROUTER_API_KEY=...
./make eval harbor run --canary \
  --model openrouter/openai/gpt-5.6-luna -- \
  --ak provider=openai \
  --ak context_window=1050000 \
  --ak 'pricing={"currency":"USD","input_per_mtok":"0.2","output_per_mtok":"1.2"}'
```

A run needs a task selection: `--task` (repeatable, or comma-separated),
`--canary`, `--limit`, or `--all`. There is no default, because the default
would be 89 tasks at whatever `--attempts` says.

The model credential is Harbor's to resolve, not this repository's: Harbor reads
the provider key for the `-m <provider>/<model>` it was given from your
environment, and the adapter writes it into the trial home. Your own
`~/.buildmax/settings.yaml` is never consulted — a benchmark that inherited it
would measure your local configuration along with the subject.

The variable is the provider's own, named by the first segment of `-m`:
`ANTHROPIC_API_KEY` for `anthropic/…`, `OPENROUTER_API_KEY` for `openrouter/…`.
A key Harbor cannot resolve stops the job at its first trial, before any
container is built. That check exists because the failure is otherwise
misdirected: Harbor hands back a provider's default endpoint only once it has
resolved that provider's key, so a missing key arrives downstream as a missing
endpoint — reported once per task, after every image has been pulled and its
system packages installed.

Other flags worth knowing: `--dry-run` prints the Harbor command and stops,
`--binary` names an artifact other than the one just built, `--jobs-dir` moves
where the job lands, `--no-import` leaves it unfiled, and anything after `--` is
passed to Harbor verbatim.

### What it runs

The command underneath, which `--dry-run` prints in full:

```shell
harbor run \
  -d terminal-bench/terminal-bench-2-1@sha256:<the pinned ref> \
  -a buildmax_harbor.agent:Buildmax \
  -m anthropic/claude-opus-4-7 \
  --include-task-name terminal-bench/pypi-server \
  -k 1 \
  -o .artifacts/harbor/jobs \
  --ak binary=bin/buildmax-linux-amd64 \
  --ak reasoning_effort=high
```

Typing it by hand works, with two things to get right that the wrapper does not
leave to chance. The dataset must carry its `@<ref>`: Harbor resolves a bare
name to `latest`, and the importer stamps the pinned digest on every bundle, so
a run without the ref files evidence under a version it did not measure. And
`src/` must be on `PYTHONPATH`, or Harbor cannot import the agent — running from
this directory satisfies that, but then `--ak binary=` needs an absolute path,
because Harbor resolves it, not `make`.

### Agent kwargs

Passed with `--ak key=value`, which is Harbor's flag rather than this
repository's: through `./make eval harbor run` they go after `--`.

| Kwarg | Required | Meaning |
|---|---|---|
| `binary` | yes | Path to the built Linux CLI. Uploaded to `/usr/local/bin/buildmax`. |
| `reasoning_effort` | no | `off`, `low`, `medium`, or `high`. Refused outside that set. |
| `max_iterations` | no | `--max-iterations` for the run. Unset takes the CLI's own default. |
| `provider` | no | The wire protocol to speak: `openai_compatible`, `openai`, `anthropic`, or `ollama`. Unset, it is inferred from the slug in `-m`. |
| `context_window` | no | The window the trial runs at, and part of the subject the bundle records. Unset takes the CLI's own default, which is well below what a long-context model declares. |
| `max_tokens` | no | Caps one response. Recorded on the subject as `max_output`. |
| `pricing` | no | A JSON price list, so the run reports what it spent. Unset, cost reports as unavailable rather than as zero. |

`--ak` parses its value as JSON, so pricing goes in as an object. It takes the
shape `settings.yaml` uses, and the rates are quoted decimals — a rate written
as a bare number becomes a float on the way through and a price in millionths
loses its last digits to that:

```shell
--ak 'pricing={"currency":"USD","input_per_mtok":"0.2",
               "cache_read_per_mtok":"0.02","output_per_mtok":"1.2"}'
```

It is passed rather than read from your own `settings.yaml` for the reason the
whole trial home is built rather than inherited: a cost that came from an
unversioned local file is not reproducible, and two people running the same
command would report different money. A rate that is missing, misspelled, or not
a number is refused before the job starts, because the alternative is a total
that looks exact and is quietly wrong.

A BuildMax provider names a wire protocol, not a vendor, and the slug in `-m`
names a vendor or a gateway. The adapter infers one from the other — anything it
does not recognise speaks Chat Completions, which is what `openai_compatible`
means — and `provider` is there for the case where that inference is not the
protocol you want measured. A gateway fronts several: OpenRouter answers Chat
Completions at `openrouter/…`, and it also serves the vendors' own shapes, which
BuildMax reaches as `openai` (the Responses API) or `anthropic` (Messages).
Naming the protocol is how a benchmark measures the path a deployment actually
takes rather than the one the slug suggests.

Harbor's leaderboard vocabulary for `reasoning_effort` is wider than BuildMax's
— it carries `xhigh` and `max` — and the adapter refuses those rather than
running them as `high`. A subject recorded at a level it did not run at cannot
be re-run against its own result.

## What the trial leaves behind

Under Harbor's `/logs/agent`, so it reaches the trial directory:

- `buildmax-result.json` — the print-mode envelope: reply, usage, cost, exit
  code, `trace_id`, `trace_path`.
- `sessions/` — the run's durable traces.

The trial home holding the provider credential lives at `/tmp/buildmax-home`
and is removed when the run ends. It is deliberately not under `/logs`, which
is collected.

## Exit codes

Every non-zero exit is a fault that propagates to Harbor and can be retried,
with one exception: `7`, the iteration cap. That is the agent deciding to stop
rather than the harness breaking, so the adapter swallows it and lets the task's
verifier judge the work that really happened. Retrying it would spend a second
attempt reaching the same place. `buildmax-result.json` and the trial's
metadata both record that it happened.

## Tests

```shell
pytest              # the harness-free modules; needs no Harbor install
./make test ./evaluation/harbor ./evaluation/adapter
```

The Go side carries two guards worth knowing about:
`evaluation/harbor/pins_test.go` holds the committed pins to the accepted
target, and `evaluation/adapter/settings_parity_test.go` holds this directory's
`settings.py` to the same key set the Go CLI adapter writes — the trial home has
two writers in two languages, and a key spelled wrongly on one side is silently
ignored rather than rejected.

## Status

The oracle smoke and a one-task canary have run; nothing wider has.

- Oracle smoke, 5 tasks: 5/5, reward 1.0, no exceptions. Docker, the anonymous
  dataset download, and the task images all work.
- Canary, `terminal-bench/pypi-server`, one attempt through this adapter: passed
  in 2m31s, and the job imported into a bundle tree with every field populated —
  verdict, task checksum as the initial state, model calls from the trace, tool
  calls and tokens from the envelope, and a reproduction command.

That is evidence the adapter drives the harness for one task. It is not evidence
about the other 88, about repeated attempts, or about any score. Four things the
canary found are fixed and described below; expect the first wider run to find
more.

Cost is reported when the run is given a price list — see the `pricing` kwarg
below. Without one it reads as unavailable, which is the honest answer rather
than zero.

## What the canary found

Six things, none of which the tests could have caught: four were assumptions
taken from reading Harbor's source rather than from a real job, and two only
appear when a trial goes wrong.

- **The trial result file is `result.json`, singular.** Harbor's own `TrialPaths`
  docstring calls it `results.json` in two places; its `result_path` property
  returns the singular name, and that is what a job directory holds.
- **A task is named `<org>/<name>`.** A bundle's task id becomes a path element,
  and the contract rejects a separator outright — so every write would have
  failed. `--include-task-name` wants the qualified name; a bare one matches
  nothing.
- **`task_checksum` is bare hex**, while the same tree's ref in the trial
  configuration is written `sha256:…`. Bundles label their digests.
- **A Bash command that leaves a background process behind hung the agent
  forever.** This one is a product bug, not an adapter detail, and it is
  described in the changelog. The canary sat on a single tool call for two hours
  under a documented 120-second timeout before it was found.
- **The task's own time budget did not bind.** Harbor cancels the agent phase at
  the task's `[agent] timeout_sec`, but cancelling a coroutine awaiting a
  `docker compose exec` ends the wait, not the process — so the container kept
  running the CLI, 98 minutes into a 30-minute budget. The adapter now reaps the
  process in the cleanup cancellation already runs. A run that outlives its
  budget cannot be compared with agents that were held to it.
- **An empty result envelope failed the whole import.** A killed subject leaves
  one, because the shell creates the file before the binary writes to it. Five
  good trials were lost to one killed container before the import learned to
  degrade a trial instead of refusing the job.

## Limits

- Linux tasks only. Terminal-Bench 2.1 is Linux; a Windows task would need the
  container paths resolved per task OS.
- The uploaded binary is unstripped and around 50 MB. A full 89-task, 5-attempt
  run uploads it once per trial.
- Community leaderboard submissions for 2.1 are closed at the time of writing;
  only maintainer-run submissions are added. Results here are still comparable
  against published rows, they just cannot be published as one.
