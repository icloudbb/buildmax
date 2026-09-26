# Supported platforms & status

BuildMax is alpha. This page defines what the project tries to support today, what is best-effort, and what is deliberately out of scope for now.

A **Beta** label on one surface below describes that component's maturity; it does not mean the product has passed the release-level Beta gate.

## Maturity levels

| Level | Meaning |
|---|---|
| **Supported** | Intended to work for early users; covered by regular local or CI checks. |
| **Beta** | Usable, but interfaces or deployment shape may still change. |
| **Experimental** | Implemented enough to try, but not a stability promise. |
| **Best effort** | May work, but is not a release-blocking path today. |
| **Not supported** | Known gap or explicit non-goal. |

## Product surfaces

| Surface | Status | What to expect |
|---|---|---|
| CLI print mode, `buildmax -p` | **Supported** | Primary local entry point. Reads, edits, greps, and runs commands in one workspace. |
| TUI, `buildmax` | **Supported** | Primary interactive local experience: sessions, slash panels, streaming, model/workspace visibility. |
| `buildmax init` and `buildmax doctor` | **Supported** | First-run configuration and local setup checks. |
| Local sessions and run traces | **Supported** | Session persistence and bounded JSONL traces under `BUILDMAX_HOME`. |
| Desktop app | **Beta** | Local chat, workspace tabs, and in-app schedules using the shared runtime. Unsigned macOS arm64 and Windows amd64 downloads are published with releases; a source build is also available. |
| Portal frontend | **Beta** | Space UI for conversations, issues, workflows, agents, files, usage, and artifacts. Password and login-code flows work; wider public exposure remains unsupported. |
| Server + local-process worker | **Beta** | Useful for trusted private deployments and development. The Compose path is covered by a full TaskRun and artifact smoke test. |
| Kubernetes worker mode | **Beta** | The local kind path exercises MySQL, MinIO, Ingress, a worker Job, and artifact retrieval end to end. The worker control API is served on a separate internal listener over HTTPS, and the same smoke proves the boundary: a labelled worker pod reaches it, an unlabelled pod is denied by the NetworkPolicy, and `/api/worker` is `404` on the public Service. Deployment APIs may still change. |
| Inbound webhooks | **Beta** | Authenticated by per-user webhook keys; payload extraction is configurable. |
| [Remote Control](remote-control.md) | **Experimental** | Watch, prompt, approve, and stop a local CLI/TUI session from Portal on another device; execution stays local. CLI opt-in only; no push notifications. |
| [Chat apps](chat-apps.md) | **Experimental** | Telegram private chats reach a Space Conversation. Group chats and other platforms are not available. |

## Operating systems

| Platform | CLI/TUI | Server/worker | Desktop | Sandbox | Notes |
|---|---|---|---|---|---|
| macOS arm64 | **Supported** | **Supported** | **Beta** | **Supported** with Seatbelt | Primary local development platform. |
| macOS amd64 | **Supported** | **Supported** | **Beta** | **Supported** with Seatbelt | Release archive target. |
| Linux amd64 | **Supported** | **Supported** | Not supported | **Supported** with `bwrap` | Primary deployment target. |
| Linux arm64 | **Supported** | **Supported** | Not supported | **Supported** with `bwrap` | Release archive and container target. |
| Windows amd64 | **Beta** | **Beta** | **Beta** | Not supported | CI builds and tests Windows. Shell behavior differs from Unix; use WSL2 for setup/deployment workflows. |
| WSL2 | **Best effort** | **Best effort** | Not supported | **Supported** with `bwrap` | Recommended Windows path for Unix shell workflows. |

## Distribution

| Artifact | Status | Notes |
|---|---|---|
| Release archives for CLI/server/worker | **Supported** | Linux amd64/arm64, macOS amd64/arm64, Windows amd64. |
| `go install github.com/icloudbb/buildmax/cmd/buildmax@latest` | **Supported** | CLI only. Uses the module version, without release archive provenance metadata. |
| `ghcr.io/icloudbb/buildmax` | **Beta** | Contains CLI, server, and worker binaries. |
| `ghcr.io/icloudbb/buildmax-portal` | **Beta** | Static Portal image; API base URL is configured at container start. |
| Desktop binary releases | **Beta** | Unsigned macOS arm64 `.dmg` and Windows amd64 `.exe` downloads. Signed and notarized installers are not available. |
| npm package for `@buildmax/gui` | Not supported | The shared GUI package is consumed by this repository through local `file:` dependencies. |

## Runtime and model providers

| Area | Status | Notes |
|---|---|---|
| OpenAI-compatible chat completions | **Supported** | Configured with `models:` entries in `settings.yaml` or `server.yaml`. |
| OpenRouter | **Supported** | Default quickstart path. |
| OpenAI-compatible local gateways | **Beta** | Works when the endpoint implements compatible chat completion behavior. |
| OpenAI Responses API | **Supported** | Set `provider: openai`; text, tools, streaming, reasoning state, prompt-cache usage, and image input use the shared LLM contract. |
| Anthropic Messages API | **Supported** | Set `provider: anthropic`; the native adapter supports the same shared contract, including reasoning state and prompt caching. |
| Local app connectors | **Experimental** | CLI-only OAuth connections with fixed plugin-declared operations; writes need interactive confirmation. No Desktop connection UI or per-run Agent grant. |
| Remote MCP CLI | **Experimental** | Register and call HTTP/SSE servers from the CLI, with optional static Bearer credentials. MCP OAuth is not implemented. |
| Built-in model hosting | Not supported | Bring your own provider, gateway, or local inference server. |
| Web search | **Beta** | The built-in `WebSearch` tool queries the public web through Firecrawl; keyless by default, or with a configured key. |
| Browser verification | **Experimental** | Local CLI (headless) and Desktop (visible window) drive an installed Chrome/Edge through the `Browser` tools. Not available to workers; no interactive takeover. |
| Multi-modal generation or voice | Not supported | Runtime tools cover text, files, shell, web search, local browser verification, MCP, hooks, skills, and subagents. |

## Deployment and security

| Capability | Status | Notes |
|---|---|---|
| Local single-user CLI/TUI | **Supported** | Start in a git working tree you can diff and revert. |
| Trusted private server deployment | **Beta** | Suitable for local labs or trusted networks after reading deployment docs. |
| Docker Compose quickstart | **Beta** | Fast contributor and single-machine path. Uses a local-process worker and local filesystem storage. |
| Local kind deployment | **Beta** | Kubernetes contribution path, with its own MySQL and MinIO. A development environment, not a deployment template. |
| Private Kubernetes deployment against your own dependencies | **Beta** | `deployment/production/` is a plain-YAML reference plus the contract each dependency has to meet. Written to be read and adapted; not applied as-is, and not yet exercised against a real cloud account. |
| Public internet server exposure | Not supported | Password, login-code, and Portal OIDC sign-in exist, but login is not rate limited, there is no second factor, and OIDC has not completed real-provider qualification. Put an identity-aware and rate-limiting boundary in front before wider exposure. |
| Portal OIDC sign-in | **Experimental** | Okta is the first configured provider. Association, JIT provisioning, and local-login posture are implemented; a pinned real-Okta journey and rotation drills remain open. |
| Operator-issued login codes | **Beta** | Single-use account-claim and recovery credential, delivered out of band because BuildMax has no mail channel. |
| JWT user API and space membership authorization | **Beta** | User API uses JWT; space membership is the resource boundary. |
| Run-token worker auth | **Beta** | Every dispatched worker receives a credential scoped to one task run, and it is the only credential the worker routes accept. The old shared worker token is removed. Each worker route also enforces the run's lifecycle: everything but the status poll is refused unless the run is RUNNING, so a leaked but unexpired token cannot act before the claim or after the run is terminal. |
| Worker API network boundary | **Beta** | The worker control API (`/api/worker/*`) is served on a separate internal listener over TLS, off the public HTTP surface. On Kubernetes it is fronted by the `buildmax-worker-api` Service and a NetworkPolicy that admits only labelled worker pods; the public Ingress carries no worker route. The kind smoke exercises the HTTPS channel and proves the cross-pod denial. |
| Bash sandbox | **Beta** | Off by default for local CLI/Desktop. Official worker images select an enabled, fail-closed baseline; an unmarked bare worker host inherits local defaults. Covers `Bash` subprocesses, not every tool or process on the host. |
| Worker pod containment | **Beta** | Worker Jobs run with no service-account token, a read-only root filesystem, and every Linux capability dropped except `SYS_ADMIN` (which `bwrap`'s own sandbox needs to run at all) — not non-root, since a capability a container runtime adds to a non-root pod does not land in that pod's effective set at exec time. The boundary a Kubernetes deployment relies on is `bwrap`'s own workspace-scoped sandboxing of the worker's Bash calls, not the pod's own uid. Workers still receive object-storage credentials. |
| Runtime hooks | **Beta** | Can observe or block selected lifecycle/tool events. Hook failures fail open. |
| Durable run traces | **Supported** | On by default, bounded and redacted; failures do not break runs. |
| Audit log | **Beta** | Records sign-ins, membership changes, model catalog changes, and refused requests. Owner-only, in the API and in Portal. A failed write is logged and dropped, so it records what happened while the database was reachable rather than guaranteeing every action was recorded. |
| Space approval workflow | Not supported | Outside the first Beta scope; separate from local tool approvals and Space invitations. |

## Compatibility

What an upgrade may and may not do to a deployment. Where a promise does not exist yet, this says so rather than implying one.

### Database schema — forward only, and no binary rollback

The schema moves forward only; there are no down migrations. Alpha does not
promise compatibility with a previous binary or preservation of an older stored
shape. A change may remove or rename fields in the same release. These
migrations destroy data or an old shape:

| Release | Migration | Removes |
|---|---|---|
| 0.2.0-alpha.9 | `llm_model_credential_encryption` | Plaintext model credentials; affected models must be re-added |
| 0.2.0-alpha.9 | `issue_owner_executor_split` | Issue `assignee_kind`/`assignee_id`, after backfilling owner and executor |
| 0.2.0-alpha.13 | `workflow_step_run_to_node_run` | The `workflow_step_run` table and the step runs recorded in it |
| 0.2.0-alpha.15 | `schedule_agent_to_executor` | Schedule `agent_id`/`last_task_id`, after backfilling executor and last-fire reference |

Binary rollback is not supported. An older binary's startup re-adds what a
newer migration dropped: rolling 0.2.0-alpha.15 back to 0.2.0-alpha.14 re-adds
`schedule.agent_id NOT NULL` filled with 0, so every schedule disappears, and
creating one fails after rolling forward again. So `buildmax-server` — the
server and its `user` and `space` commands — refuses to start against a
database whose `schema_migration` records a migration it does not know. It
stops before changing anything and names those migrations. Releases up to and
including 0.2.0-alpha.15 predate that check: starting one against a newer
database still damages it.

Take a backup of the database and the object-storage bucket together before an
upgrade. To go back, restore both from that backup and run the binaries that
match it. `database.allow_newer_schema: true` in `server.yaml` starts an older
binary anyway; it exists for a deliberate recovery you have planned, and it can
damage the data in exactly the way above. Remove it afterward.

### HTTP API — no version, so expect change

There is no `/v1` prefix and no negotiated version. `openapi.json`, served at `/openapi.json`, describes every route the server registers and is tested against them in both directions — but describing a route is not promising to keep it during Beta.

Expect additions to be additive and safe. Do not assume a route, a field, or a status code will survive a release without reading the changelog. A breaking change will be called out there; it will not be prevented.

The one exception is the managed inference wire contract, which carries an explicit version (`llmwire.Version`) precisely because CLI, Desktop, and worker builds move independently of the server. A change to its shape needs a new version rather than a silent edit.

### Configuration — additive, with removals announced

New keys are added with defaults that preserve existing behaviour, and `config-examples/server.example.yaml` carries every key the server reads — a test fails when it does not.

Removals and renames are called out in the changelog. There is no deprecation period and no compatibility shim, so a key you rely on can disappear in one release with a note rather than two releases with a warning.

Credentials never gain defaults. A setting that decides *where* a deployment connects or *as whom* is left unset rather than pointed somewhere plausible.

### Stored data

Task/TaskRun metadata lives in MySQL; Artifact payloads, worker traces, and workspace checkpoints use object storage. Artifact deletion/expiry and retention sweeps can remove payloads, and checkpoint orphan cleanup removes unreferenced blobs. Backups must include both the database and bucket; storage is not an indefinite archive.

Audit retention is separately configured: setting `audit.retention_days` in `server.yaml` expires events older than that window, and each sweep records what it removed as an `audit.pruned` event. The default is to keep everything. The trail can be downloaded as CSV or JSONL — a space owner gets their own, a System Administrator gets the deployment's — and a download is itself recorded.

There is no export or import command for a deployment's data as a whole, so moving one means moving its database and its bucket together.

## Non-goals for the alpha

- Qualified production identity management. Portal OIDC sign-in is implemented but not yet qualified against a pinned real Okta tenant; SAML, native CLI/Desktop OIDC, and automatic login-code delivery are not implemented.
- Multi-tenant public SaaS hosting. BuildMax is aimed at local use and private deployments, not running an untrusted public shared service.
- A guarantee that model-selected code is safe. Treat every run as executing untrusted commands with your credentials and network access.
- Full sandboxing for every operation. The sandbox targets `Bash`; file tools, MCP tools, hooks, and provider calls have separate boundaries.
- A stable public plugin marketplace or hosted integration catalog. Use MCP, local skills, hooks, and configuration for extension today.
- A workflow engine replacement for Airflow, Temporal, GitHub Actions, or CI. Workflows are lightweight reusable agent plans, not a general orchestrator.
- A Git hosting or IDE replacement. Git is used for visibility and recovery; BuildMax does not replace review, merge, or repository management workflows.
- Native mobile apps, browser extensions, or signed and notarized desktop installers.

## How to read this page

When a path is **Supported**, it should be reasonable for an early adopter to try and report bugs against it. When a path is **Beta** or **Experimental**, bug reports are welcome, but fixes may include interface changes. When a path is listed as **Not supported**, issues are still useful as signal, but the project will not treat the gap as a release blocker until the roadmap says otherwise.
