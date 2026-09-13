# Troubleshooting

Common problems running BuildMax and how to work out what a run actually did. Start with `doctor`, then match your symptom to a section below.

## Start with doctor

```bash
buildmax doctor
```

`doctor` checks local setup without contacting a model provider. It names the configuration file BuildMax is reading, catches placeholder API keys, warns when the workspace is not on a git branch, and points at sandbox dependency checks when sandboxing is enabled.

## `No model configured. Add a model to …`

There is no `models:` entry in `settings.yaml`. BuildMax does **not** read an API key from the environment — earlier versions did, and some older documentation still says so. Create the file as shown in [Quickstart](quickstart.md).

The message prints the exact path it looked at; if that path is not what you expect, `BUILDMAX_HOME` is set somewhere.

## Runs fail in bursts with HTTP 429

Free OpenRouter models rate-limit aggressively. This shows up as runs that work individually but fail when you run several in a row, or when an agent makes many tool-calling round trips. Switch to a paid model in `settings.yaml`, or slow down.

## `POST /api/auth/login` returns 503

The Server could not reach a user, password, or login-code store, or it has no JWT secret. Check the startup log and `server.yaml`, then create an account and issue its first login code. BuildMax has no mail delivery channel: an operator passes that single-use code to the user out of band.

## Tasks stay `PENDING` and never run

The scheduler claimed the run but could not start a worker. Check, in order:

1. `buildmax-worker` is on `PATH` or next to the server binary, matching `worker.binary` in `server.yaml`
2. `worker.server_url` is reachable **from the worker**, which is not always the same address the server binds
3. the scheduler can mint a run token — `jwt_secret` signs it, and it is the only credential a worker has for `/api/worker/*`
4. `workspaces_dir` exists and is writable by both processes
5. The `storage:` block is reachable from the worker, which talks to blob storage directly rather than through the server

The server log names the step that failed.

## Webhook returns 400

The prompt could not be extracted from the request body. `webhook.message_path` in `server.yaml` must match your payload's shape — the default is `message`, and a nested field is written `body.text`.

## The sandbox will not turn on

```bash
buildmax sandbox deps      # is bwrap / sandbox-exec / socat present?
buildmax sandbox status    # what is actually resolved, and from which layer
```

The sandbox needs Seatbelt on macOS or `bwrap` on Linux/WSL2. It is **unavailable on native Windows**. If `status` shows a value you did not set, check `<BUILDMAX_HOME>/policy.yaml` and `BUILDMAX_SANDBOX_ENABLED` — both override `settings.yaml`. See [Sandbox](sandbox.md).

## A hook never fires

Almost always the `matcher`. It is a regex against the **tool name**, and the names are capitalized: `Bash`, `Write`, `Edit`, `Read`, `Grep` — not `bash` or `writefile`. Check the current names with `/tools`, or in [Tools](tools.md).

Also: `matcher` only applies to `pre_tool_use`, `post_tool_use`, and `post_tool_use_failure`. On other events it is ignored.

Remember hooks **fail open** — a hook that times out or errors allows the action. Silence can mean "ran and failed", not "did not run".

## The desktop app refuses to start

It was built without the `desktop` build tag, so no frontend bundle is embedded. The binary tells you this rather than opening a blank window. Build with `./make build`, which builds the frontend and passes the tag.

## `go test ./...` behaves differently from `./make test`

`./make test` sets `BUILDMAX_HOME=./testing-sandbox` so tests never touch your real data directory. Run it rather than bare `go test` — some tests assume that isolation.

## Bash tool behaves oddly on Windows

Native Windows has no sandbox, and the bash tool falls back to `cmd /c`. CI builds, vets, and runs the test suite on Windows, but shell-dependent tests are skipped there. WSL2 is the supported path for anything involving the shell, `./make kind up`, or deployment.

## Something else

The trace tells you what the run actually did — every LLM call, every tool call, what was denied, and how it ended:

```bash
ls -t ~/.buildmax/sessions/<session-id>/traces/ | head -1
```

See [Sessions and traces](sessions-and-traces.md). Logs are file-only, under `<BUILDMAX_HOME>/logs/buildmax.log`; raise the detail with `log_level: debug` in `settings.yaml`.
