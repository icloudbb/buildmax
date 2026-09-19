# Evaluation

How to measure a BuildMax build, and what you get back.

There are two paths. They share one contract, one bundle format, and one report
renderer, and they answer different questions:

| | Local suite | Harbor / Terminal-Bench 4.0 |
|---|---|---|
| Question | How reliably does a model drive behaviour this repository wrote? | How does BuildMax compare with Codex and Claude Code? |
| Runs | `./make eval` | Harbor runs it; `./make eval harbor` files the result |
| Costs | Minutes, a model API key | Tens of minutes to hours, plus Docker |
| Verdict | Graders in this repository | The benchmark's own verifier |
| On a task the agent fails | Exits non-zero — it is a gate | Exits zero — it is a score |

Neither is a pull-request gate. Both are explicit, cost money, and are run
deliberately. Everything that can be checked without a model API key — task
validity, oracles, graders, the adapters — runs in `./make test` instead, so a
task that measures nothing is caught before it costs anything.

Why the system is shaped this way is in
[docs/design/evaluation-system.md](../docs/design/evaluation-system.md). Which
suite to run for a change is in
[docs/contribute/testing.md](../docs/contribute/testing.md).

## Local suite

```shell
./make eval                              # every CLI task in suite/
./make eval --task local-summarize-data  # one task
./make eval --trials 5                   # five attempts each, for a tighter interval
./make eval --surface worker             # worker tasks, building the worker binary
./make eval --baseline bin/buildmax-old  # compare two builds, paired per attempt
./make eval --keep-failures              # leave a failed trial's workspace to look at
```

It builds the CLI and measures it as a black box. Every trial runs under a
`BUILDMAX_HOME` built from the subject alone, so your own settings, hooks, and
plugins cannot change what is measured. The model and its prices come from your
`settings.yaml`; `--model` picks an entry other than the first.

`./make eval --help` prints the runner's own flags.

### What a task looks like

```text
suite/local-summarize-data/
├── task.json           what is asked, what bounds it, what must be true after
├── state/sales.csv     the only directory copied into the trial workspace
├── graders/            checks the agent never sees
└── oracle/solve.sh     a reference solution, run by preflight
```

Only `state/` is materialized. A task cannot leak its answer by naming the wrong
path, because there is no path to name — and preflight additionally digests
everything under `graders/` and `oracle/` and fails the task if any of it turns
up in the workspace.

Preflight also runs the oracle before the model does: a task whose own reference
solution cannot satisfy its required graders is measuring its graders rather
than the agent, and is rejected without spending a token.

`task.json`, from that task:

```json
{
  "id": "local-summarize-data",
  "surface": "cli",
  "domain": "capability",
  "turns": ["Read sales.csv and write report.md. It must state the total revenue…"],
  "limits": { "wall_seconds": 180, "tool_calls": 40 },
  "trials": 3,
  "graders": [
    { "name": "files",   "required": true,  "config": { "exists": ["report.md"] } },
    { "name": "command", "required": true,  "config": { "run": ["./check-report.sh"] } },
    { "name": "trace",   "required": false, "config": { "max_tool_calls": 40 } }
  ],
  "oracle": ["sh", "./solve.sh"]
}
```

`turns` is a list because a task may be a conversation; a single-turn task has
one entry. `limits` are stop conditions, not targets — exhausting one is
recorded as `timed_out`, not as a grader failure. A `required: false` grader
reports a dimension without deciding the outcome.

### The three graders

| Name | Kind | Asserts on | Config |
|---|---|---|---|
| `files` | deterministic | Final workspace state | `exists`, `absent`, `matches`, `equals` |
| `command` | deterministic | Whatever a task-supplied script decides | `run`, `timeout_seconds`, `expect_exit` |
| `trace` | trace | What the run did, from its durable trace | `forbidden_tools`, `required_tools`, `max_tool_calls`, `require_denial`, `forbid_denial`, `max_compactions` |

`files` and `command` are outcome-first: a reply claiming a file was written is
not the file. `trace` is for what state cannot show — that a boundary held, that
a tool was never reached, that the run did not take fifty attempts to get there.

## Harbor / Terminal-Bench 4.0

Harbor owns the tasks, the containers, and the verdict; BuildMax is one of its
agents. See [harbor/README.md](harbor/README.md) for the flags, the agent
kwargs, and what the adapter does inside a container.

```shell
./make doctor harbor        # what is missing, and the command for each; installs nothing
./make setup harbor         # install those: uv, the pinned Harbor, the Linux CLI

./make eval harbor run --oracle --limit 5                    # prove the environment
./make eval harbor run --canary --model <provider>/<model>   # the pinned canary subset
./make eval harbor run --all --attempts 5 --model <provider>/<model>
```

`run` assembles the Harbor command from `harbor/pins.json` — dataset ref
included, which is what keeps a job from being filed under a version it did not
measure — and imports the finished job. Importing is also a command of its own,
for a job someone else ran:

```shell
./make eval harbor --job .artifacts/harbor/jobs/<job>   # file a finished job
./make eval harbor --job runs/new --baseline-job runs/old   # compare two
```

The import builds nothing and calls no model. It reads what Harbor already
produced and expresses it in the same contract as a local run, so an external
result and a local one carry the same subject tuple, failure taxonomy, and pass
rate with its uncertainty.

## What comes out

Both paths write the same tree, under `.artifacts/evaluation/` by default:

```text
<experiment>/<subject>/
├── experiment.json                 dataset, subjects, tasks, repetition count
└── trials/<task>/<attempt>/
    ├── bundle.json                 one attempt's evidence
    ├── trace.jsonl                 the run's durable trace
    └── artifacts/
```

A bundle is a directory rather than a file because most of a trial's evidence
already is files. It carries the subject that produced it, the status, the
grader verdicts, usage and cost, digests of the workspace before and after, and
a bounded reproduction path. Bundles stay on the machine that made them.

The report is a vector, never a single number:

```text
Pass rate   : 80% (4/5 scored)  95% CI [38%, 96%]
Consistency : 80% of tasks passed every attempt
Trials      : 6 run, 5 scored
Unscored    : 1 trial(s) — agent_error 1
Cost        : 0.2199 USD (incomplete — part of the run could not be priced)
```

`4/5`, not `4/6`. A trial the harness could not run says nothing about the
subject, so it is reported as its own rate rather than counted as a failure —
otherwise the less reliable the harness, the worse the agent looks.

### Reading a status

| Status | Means | In the rate? |
|---|---|---|
| `passed` / `failed` | Required graders decided | yes |
| `timed_out` | A stated budget expired — wall time, or the iteration cap | yes |
| `agent_error` | The runtime failed before producing a gradable outcome | no |
| `grader_error` | Grading could not complete, so the attempt is unscored | no |
| `infrastructure_error` | The environment failed, blaming neither side | no |
| `invalid_task` | Preflight rejected the task; nothing was measured | no |
| `canceled` | The experiment was stopped | no |

Keeping these apart is the point. A provider outage, a broken task, and an agent
that could not do the work are three different facts, and collapsing them into
pass/fail reports the first two as incapability.

`failure_class` refines a status in free text — `iteration-cap`,
`AgentTimeoutError`, `trace-unavailable`. A gate reads the status; a person
reads the class.
