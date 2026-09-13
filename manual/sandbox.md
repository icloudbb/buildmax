# Sandbox

The sandbox confines the subprocesses started by the `Bash` tool: which paths they may read and write, and which network destinations they may reach. It is the strongest boundary BuildMax offers around model-chosen shell commands.

Two things to be clear about before you rely on it:

- **It covers `Bash` only.** Other tools (`Read`, `Write`, `Edit`, `Glob`, `Grep`) keep their own path checks — the workspace root boundary — and are not affected by these settings.
- **Local CLI/Desktop disable it by default.** Official worker images select an enabled,
  fail-closed worker baseline; an unmarked bare worker host inherits the local
  default unless configured otherwise. Turning it on locally is a deliberate act.

## Availability

| Platform | Backend | Status |
|---|---|---|
| macOS | Seatbelt (`sandbox-exec`) | Supported |
| Linux, WSL2 | `bwrap` (bubblewrap) | Supported |
| Native Windows | — | Unavailable |

Check your host before configuring anything:

```bash
buildmax sandbox deps      # are bwrap / sandbox-exec / socat present?
buildmax sandbox status    # resolved config, and which layer set each value
```

## Turn it on

For one TUI or print-mode run, without changing `settings.yaml`:

```bash
buildmax --sandbox
buildmax --sandbox --sandbox-mode regular
```

`--sandbox-mode` accepts `auto_allow` or `regular` and requires `--sandbox`. An explicit `--sandbox` fails the run at startup if the OS backend is missing; it never silently falls back to unsandboxed Bash. There is deliberately no `--no-sandbox` flag: a per-run convenience must not weaken a boundary required by configuration or operator policy.

To make sandboxing the user default:

```bash
buildmax sandbox enable
buildmax sandbox mode auto_allow
```

Or edit the `sandbox:` block in `<BUILDMAX_HOME>/settings.yaml` directly. When the sandbox is active the TUI footer shows the mode.

### Modes

| Mode | Behavior |
|---|---|
| `auto_allow` | A command that runs sandboxed skips the approval prompt — the boundary is the confinement, not your attention |
| `regular` | Normal approval behavior is kept even when sandboxed |

`auto_allow` is the mode that actually changes how the agent feels to use: you stop approving every command because the blast radius is already bounded.

## Boundaries

```yaml
# <BUILDMAX_HOME>/settings.yaml
sandbox:
  enabled: true
  fail_if_unavailable: false          # true = refuse to run bash unsandboxed
  auto_allow_bash_if_sandboxed: true
  allow_unsandboxed_commands: false   # gate for the per-call escape hatch
  excluded_commands: []               # commands that never get sandboxed

  filesystem:
    allow_write: ["."]
    deny_write:  ["~/.ssh", "~/.aws"]
    allow_read:  ["."]
    deny_read:   ["~/.ssh"]

  network:
    allowed_domains: ["api.github.com", "proxy.golang.org"]
    denied_domains:  []
    allow_local_binding: false
    allow_all_unix_sockets: false

  process:
    max_cpu_seconds: 0    # 0 = no limit from this layer
    max_memory_mb: 0
    max_processes: 0
    max_open_files: 0
```

Network control works by routing egress through a Go-side HTTP/SOCKS proxy, so domain rules apply to ordinary tools inside the sandbox without per-tool support. Environment variables that look like secrets (`*_TOKEN`, `*_KEY`, `*_SECRET`, and BuildMax's own credentials) are scrubbed from the child environment unless you list them explicitly.

`sandbox.process` bounds a sandboxed command's own resource use — CPU time, memory, process count, and open file descriptors. `max_memory_mb` has no effect on macOS: Darwin does not support limiting a process's virtual memory the way Linux does, so the setting is silently a no-op there.

## Operator policy

`<BUILDMAX_HOME>/policy.yaml` holds a sandbox block with the same shape and is the final authority. Sandbox precedence is `policy.yaml` > per-run CLI > `BUILDMAX_SANDBOX_ENABLED` > `settings.yaml` > surface default. In particular, an environment variable cannot turn off a sandbox that policy requires. Use the policy file when the machine's owner and the machine's user are different people.

Two keys make the policy layer authoritative rather than merely additive: `allow_managed_read_paths_only` and `allow_managed_domains_only` cause lower layers' `allow_read` and `allowed_domains` entries to be ignored.

## The escape hatch

A single call can request `dangerously_disable_sandbox`. It is honored **only** when `allow_unsandboxed_commands: true`. Leave that false and the flag is inert — which is the point of having it be config-gated rather than a runtime decision.

## Scope and limits

The sandbox confines `Bash` well today. Keep its deliberate boundaries in mind:

- it covers `Bash` (and the `command`/`http` hook transports, below) only — every other tool's boundary is the workspace root, not this config
- network egress is filtered by hostname, not by inspecting TLS
- on a worker it does not impose a pod-wide destination policy; that is an accepted, documented first-Beta limit, not this layer's job

There is deliberately no `buildmax sandbox overrides` subcommand: `allow_unsandboxed_commands` is an operator lock you set once in `policy.yaml`, not a per-session toggle, so you edit it there.

A `command` or `http` hook runs through the same confinement a sandboxed `Bash`/`WebFetch` call does — a hook cannot reach what the sandbox exists to contain — but hooks carry no `dangerously_disable_sandbox`-equivalent: they are config-authored automation, not an LLM-chosen call you watch turn by turn, so there is no per-invocation argument for one to opt out with.

Do not treat the sandbox as a substitute for reviewing what a deployment is allowed to reach.

## Related

- [Hooks](hooks.md) — blocking a command instead of confining it
- [Tool permissions](tool-permissions.md) — controlling whether a tool runs, rather than what it can reach
