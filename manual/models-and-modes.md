# Models & modes

BuildMax runs in one of two modes, and one command switches between them.

| | Local mode | Managed mode |
|---|---|---|
| You are | signed out | signed in |
| Models come from | `settings.yaml` on this machine | the deployment you signed in to |
| Provider credentials | yours, in `settings.yaml` | the deployment's; never sent to you |
| Prompts and tool results go | straight to each provider | to that deployment |

```bash
buildmax models       # which mode you are in, and what it offers
buildmax login        # switch to a deployment's models
buildmax logout       # switch back to settings.yaml
```

Nothing else configures this. There is no per-model setting for it, because a
session is in one mode or the other and every prompt in it goes to the same
place.

## Local mode

The default, and the whole product without a server: `settings.yaml` lists the
models, each with its own endpoint and API key, and the agent calls them from
this machine. Start one with [`buildmax init`](quickstart.md).

`default_model` names which entry a new session starts with:

```yaml
default_model: GPT-5.6 Luna
models:
  - model: openai/gpt-5.6-luna
    name: GPT-5.6 Luna
    # …
```

Leave it out and the first entry is the default. Inside a session, `/model`
switches for that conversation only — the next one starts from the default
again.

## Managed mode

Sign in and the models become the deployment's:

```bash
buildmax login
```

```text
Server URL [http://localhost:5678]: https://buildmax.example.com
Email: you@example.com
Password (leave blank to use a login code): ********
Logged in as you@example.com on https://buildmax.example.com
```

Every model that deployment offers is available to you — a space is who you
collaborate with, not what gates a model. `buildmax models` lists them and says
where prompts go:

```text
Signed in to https://buildmax.example.com. Prompts, tool schemas, and tool
results go there.

Models this deployment offers:
  NAME    CONTEXT   DEFAULT
  Fast    128000
  Deep    200000    yes
```

Your `settings.yaml` models are untouched and unused while you are signed in.
`buildmax logout` brings them back.

What you gain is that the deployment holds the provider credentials, so you
never have one on your machine, and it records what each call cost. Its prices
come with the model list, so the session footer, `buildmax info`, and
`buildmax usage` show what your signed-in sessions cost, and the Portal
**Usage** page totals them. What changes
is where your data goes: **prompts, tool schemas, and tool results pass through
that server.** That is the point of the mode, which is why every surface says
which one you are in — `buildmax models`, the `/model` panel, and the TUI footer.

## The two never mix

A signed-in session sees only the deployment's models. A signed-out one sees
only `settings.yaml`. Neither covers for the other:

- **A deployment that is down does not fall back to your local models.** The
  session refuses to start and says your login still works: try again once the
  deployment is back, or `buildmax logout` to work locally. Falling back would
  send a prompt you wrote for a governed deployment to a provider on your own
  key instead. Desktop shows a banner with **Retry** and clears it on its own
  when the deployment answers; it does not sign you out.
- **An expired or revoked login does not become local mode on its own.**
  BuildMax says the session ended and waits: sign in again, or
  `buildmax logout` to work locally. Your login stays in place until you pick
  one, even after the deployment has refused it.
- **A disabled account** gets its own message: signing in again does not help
  until an administrator re-enables it, so the choices are to ask them or to
  `buildmax logout`.
- **A conversation stays in the mode it began in.** Its first turn records where
  its prompts go. Resuming it after signing in or out is refused rather than
  replaying its history somewhere else; start a new session in the new mode.

Working offline is therefore `buildmax logout` — one command, and an explicit
decision about where your prompts go.

## Checking it

`buildmax doctor` reports the mode as its first check, along with whether the
models behind it actually work:

```text
[OK]   mode: signed in to https://buildmax.example.com: its models serve every prompt
[OK]   settings.yaml: not present, and not needed while signed in
```

Signed in, `settings.yaml` is optional and its models are not probed. Signed
out, it reports local mode and then checks each entry in `settings.yaml` — an
endpoint, a key, a local daemon that is not running.

`buildmax me` asks the deployment too, so a login it has revoked is reported as
unusable rather than as signed in.

## Related

- [Tools](tools.md) — the built-in tools every run gets
- [Sessions and traces](sessions-and-traces.md) — what a session records
