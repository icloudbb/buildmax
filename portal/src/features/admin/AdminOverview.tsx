import { useEffect, useState } from "react"
import type {
  ApiAdminMe,
  ApiAdminSpaceAttention,
  ApiAdminSpacesAttentionResponse,
  ApiAdminSystem,
} from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { buildHash } from "../../router"
import { getAdminConfig, getAdminMe, getAdminSystem, listAdminRuntimeSpaces } from "./api"
import {
  FAILURE_CLASSES,
  WORKFLOW_FAILURE_CLASSES,
  ageSince,
  failureLabel,
  orderedFailures,
  timeUntil,
  waitingSummary,
} from "./runtime"

function StatusPill({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className={ok ? "admin-pill admin-pill--ok" : "admin-pill admin-pill--bad"}>{label}</span>
  )
}

/** One failure class per row, with who acts on it. */
function FailureTable({
  failures,
  classes,
}: {
  failures: [string, number][]
  classes: Record<string, { label: string; owner: string }>
}) {
  return (
    <table className="admin-table">
      <thead>
        <tr>
          <th scope="col">Cause</th>
          <th scope="col">Runs</th>
          <th scope="col">Who acts</th>
        </tr>
      </thead>
      <tbody>
        {failures.map(([cls, n]) => (
          <tr key={cls}>
            <td>{failureLabel(cls, classes)}</td>
            <td>{n}</td>
            <td className="admin-table__muted">{classes[cls]?.owner ?? "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

/**
 * The requests waiting on a Space's members: how many, how long, when the
 * first expires, and the Workflow run to name to them. Never what was asked.
 */
function SpaceWaiting({
  space,
  serverTime,
}: {
  space: ApiAdminSpaceAttention
  serverTime: string
}) {
  const summary = waitingSummary(space.waiting_requests)
  if (!summary) return <>—</>
  const expiry = timeUntil(space.next_request_expiry_at, serverTime)
  return (
    <>
      {summary}
      <span className="admin-table__muted">
        {` · oldest ${ageSince(space.oldest_waiting_request_at, serverTime) ?? "—"}`}
        {expiry ? ` · expiry ${expiry}` : null}
      </span>
      {space.oldest_waiting_workflow_run_id ? (
        <div className="admin-table__muted">
          Workflow run <code>{space.oldest_waiting_workflow_run_id}</code>
        </div>
      ) : null}
    </>
  )
}

/** Task run and Workflow run failures in the window, each by its own classes. */
function SpaceFailures({ space }: { space: ApiAdminSpaceAttention }) {
  const taskRuns = orderedFailures(space.failures)
    .map(([cls, n]) => `${n} ${failureLabel(cls).toLowerCase()}`)
    .join(", ")
  const workflowRuns = orderedFailures(space.workflow_failures, WORKFLOW_FAILURE_CLASSES)
    .map(([cls, n]) => `${n} ${failureLabel(cls, WORKFLOW_FAILURE_CLASSES).toLowerCase()}`)
    .join(", ")
  if (!taskRuns && !workflowRuns) return <>—</>
  return (
    <>
      {taskRuns ? <div>{taskRuns}</div> : null}
      {workflowRuns ? (
        <div>
          {`Workflow: ${workflowRuns}`}
          {space.latest_failed_workflow_run_id ? (
            <span className="admin-table__muted">
              {" · latest "}
              <code>{space.latest_failed_workflow_run_id}</code>
            </span>
          ) : null}
        </div>
      ) : null}
    </>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className="admin-fact">
      <span className="admin-fact__label">{label}</span>
      <span className="admin-fact__value">{value}</span>
    </div>
  )
}

/**
 * AdminOverview answers "is this deployment all right", which is why it is the
 * first page rather than a list of accounts.
 *
 * A failed dependency is named and not explained. That matches the server,
 * which withholds the reason on purpose: connection errors carry DSNs,
 * endpoints, and bucket names, and they belong in the log where an operator
 * already has to be.
 */
export function AdminOverview({ token }: { token: string | null }) {
  const [system, setSystem] = useState<ApiAdminSystem | null>(null)
  const [config, setConfig] = useState<Record<string, unknown> | null>(null)
  const [me, setMe] = useState<ApiAdminMe | null>(null)
  const [attention, setAttention] = useState<ApiAdminSpacesAttentionResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!token) return
    let cancelled = false
    setLoading(true)
    Promise.all([
      getAdminSystem(token),
      getAdminConfig(token).catch(() => null),
      getAdminMe(token).catch(() => null),
      listAdminRuntimeSpaces(token, { limit: 20 }).catch(() => null),
    ])
      .then(([sys, cfg, mine, spaces]) => {
        if (cancelled) return
        setSystem(sys)
        setConfig(cfg)
        setMe(mine)
        setAttention(spaces)
      })
      .catch((err) => {
        if (!cancelled) setError(getErrorMessage(err, "Failed to load the deployment status"))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [token])

  if (loading) return <p className="admin-empty">Loading the deployment status…</p>
  if (error) {
    return (
      <p className="settings-section__error" role="alert">
        {error}
      </p>
    )
  }
  if (!system) return null

  const warnings = Array.isArray(config?.warnings) ? (config.warnings as string[]) : []
  const runStatuses = Object.entries(system.task_runs).sort(([a], [b]) => a.localeCompare(b))
  const runtime = system.runtime
  const failures = orderedFailures(runtime?.failures)
  const workflowFailures = orderedFailures(runtime?.workflow_failures, WORKFLOW_FAILURE_CLASSES)
  const myGrant = me?.grants?.[0]
  const grantedBy = myGrant
    ? myGrant.granted_by === "buildmax-server"
      ? "the operator command"
      : myGrant.granted_by
    : ""
  // The redacted config minus warnings, which have their own section. The server
  // has already reduced every credential to a set/not-set boolean, so this is
  // safe to render whole.
  const configEntries = Object.entries(config ?? {}).filter(([key]) => key !== "warnings")

  return (
    <div className="admin-sections">
      {myGrant ? (
        <section className="settings-page__section">
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">Your access</h2>
              <p className="settings-page__section-copy">
                Why you can see this area. Deployment authority is separate from any
                space role you also hold.
              </p>
            </div>
          </div>
          <div className="admin-facts">
            <Fact label="Role" value={myGrant.role} />
            <Fact label="Granted by" value={grantedBy} />
            <Fact label="Granted" value={new Date(myGrant.granted_at).toLocaleString()} />
          </div>
        </section>
      ) : null}

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Health</h2>
            <p className="settings-page__section-copy">
              What this server reports about itself. A failed check names the dependency
              and not the reason — the reason is in the server log.
            </p>
          </div>
          <StatusPill ok={system.ready} label={system.ready ? "Ready" : "Not ready"} />
        </div>
        <div className="admin-facts">
          {system.dependencies.length === 0 ? (
            <p className="admin-empty">This deployment registered no dependency checks.</p>
          ) : (
            system.dependencies.map((dep) => (
              <div key={dep.name} className="admin-fact">
                <span className="admin-fact__label">{dep.name}</span>
                <StatusPill ok={dep.status === "ok"} label={dep.status} />
              </div>
            ))
          )}
        </div>
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Deployment</h2>
            <p className="settings-page__section-copy">
              Version, execution mode, and how work is flowing.
            </p>
          </div>
        </div>
        <div className="admin-facts">
          <Fact label="Version" value={system.version} />
          <Fact label="Worker run mode" value={system.worker_run_mode ?? "unknown"} />
          <Fact label="Worker model transport" value={system.worker_llm_transport ?? "unknown"} />
          {/*
            Empty means no worker path reports a sandbox surface, which is every
            deployment today. It is not a claim that runs are unconfined — each
            run's details say how it was sandboxed — so the label says exactly
            what is missing: the report.
          */}
          <Fact
            label="Sandbox surface"
            value={system.sandbox_surface ? system.sandbox_surface : "not reported"}
          />
          <Fact label="Self-registration" value={system.allow_signup ? "open" : "closed"} />
          <Fact label="System administrators" value={String(system.system_admins)} />
        </div>
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Task runs</h2>
            <p className="settings-page__section-copy">Counts by status across every space.</p>
          </div>
        </div>
        <div className="admin-facts">
          {runStatuses.length === 0 ? (
            <p className="admin-empty">No runs yet.</p>
          ) : (
            runStatuses.map(([status, count]) => (
              <Fact key={status} label={status} value={String(count)} />
            ))
          )}
        </div>
      </section>

      <section className="settings-page__section" aria-labelledby="admin-work-progress">
        <div className="settings-page__section-head">
          <div>
            <h2 id="admin-work-progress" className="settings-page__section-title">
              Work progress
            </h2>
            <p className="settings-page__section-copy">
              Whether work is moving. Ages are measured by the server&apos;s clock; nothing
              here is a run&apos;s content.
            </p>
          </div>
        </div>
        {runtime ? (
          <>
            <div className="admin-facts">
              <Fact
                label="Oldest pending"
                value={ageSince(runtime.oldest_pending_at, system.server_time) ?? "none waiting"}
              />
              <Fact
                label="Oldest scheduled, not started"
                value={ageSince(runtime.oldest_unstarted_at, system.server_time) ?? "none waiting"}
              />
              <Fact
                label={`Running, silent over ${Math.round(runtime.stale_after_seconds / 60)} min`}
                value={String(runtime.stale_running)}
              />
              {/*
                A Workflow run waiting on a person holds no worker, so none of the
                task run numbers above show it. Any member of its Space may answer.
              */}
              <Fact
                label="Workflow requests waiting on Space members"
                value={waitingSummary(runtime.waiting_requests) ?? "none waiting"}
              />
              <Fact
                label="Oldest waiting request"
                value={ageSince(runtime.oldest_waiting_request_at, system.server_time) ?? "none waiting"}
              />
              <Fact
                label="Next request expiry"
                value={timeUntil(runtime.next_request_expiry_at, system.server_time) ?? "none set"}
              />
            </div>
            <h3 className="admin-subtitle">
              Task run failures in the last {runtime.failure_window_hours} hours
            </h3>
            {failures.length === 0 ? (
              <p className="admin-empty">No failed task runs.</p>
            ) : (
              <FailureTable failures={failures} classes={FAILURE_CLASSES} />
            )}
            <h3 className="admin-subtitle">
              Workflow run failures in the last {runtime.failure_window_hours} hours
            </h3>
            {workflowFailures.length === 0 ? (
              <p className="admin-empty">No failed Workflow runs.</p>
            ) : (
              <FailureTable failures={workflowFailures} classes={WORKFLOW_FAILURE_CLASSES} />
            )}
          </>
        ) : (
          <p className="admin-empty" role="status">
            Work progress is unavailable right now; the server could not read it.
          </p>
        )}

        <h3 className="admin-subtitle">Spaces needing attention</h3>
        {attention === null ? (
          <p className="admin-empty" role="status">
            The Spaces needing attention are unavailable right now.
          </p>
        ) : attention.spaces.length === 0 ? (
          <p className="admin-empty">No Space has waiting work, waiting requests, or recent failures.</p>
        ) : (
          <table className="admin-table">
            <thead>
              <tr>
                <th scope="col">Space</th>
                <th scope="col">Owners</th>
                <th scope="col">Active</th>
                <th scope="col">Oldest active</th>
                <th scope="col">Waiting on members</th>
                <th scope="col">Failed</th>
              </tr>
            </thead>
            <tbody>
              {attention.spaces.map((space) => (
                <tr key={space.space_id}>
                  <td>
                    <a href={buildHash({ name: "admin", section: "spaces", spaceId: space.space_id })}>
                      {space.name || space.space_id}
                    </a>
                    {space.personal ? <span className="admin-table__muted"> · personal</span> : null}
                  </td>
                  <td>{space.owners.map((o) => o.email || o.user_id).join(", ") || "—"}</td>
                  <td>
                    {Object.entries(space.active)
                      .map(([status, n]) => `${n} ${status.toLowerCase()}`)
                      .join(", ") || "—"}
                  </td>
                  <td>{ageSince(space.oldest_active_at, system.server_time) ?? "—"}</td>
                  <td>
                    <SpaceWaiting space={space} serverTime={system.server_time} />
                  </td>
                  <td>
                    <SpaceFailures space={space} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {attention && attention.total > attention.spaces.length ? (
          <p className="admin-empty">
            Showing the {attention.spaces.length} longest-waiting of {attention.total} Spaces.
          </p>
        ) : null}
      </section>

      {warnings.length > 0 ? (
        <section className="settings-page__section">
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">Configuration notes</h2>
              <p className="settings-page__section-copy">
                States worth knowing about. These are not errors — the server is running.
              </p>
            </div>
          </div>
          <ul className="admin-warnings">
            {warnings.map((warning) => (
              <li key={warning} className="admin-warnings__item">
                {warning}
              </li>
            ))}
          </ul>
        </section>
      ) : null}

      {configEntries.length > 0 ? (
        <section className="settings-page__section">
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">Effective configuration</h2>
              <p className="settings-page__section-copy">
                The resolved <code>server.yaml</code>, read-only. Every credential is
                shown only as whether it is set — never its value. Change it by editing
                the file and restarting the server.
              </p>
            </div>
          </div>
          <details className="admin-config">
            <summary className="admin-config__summary">Show configuration</summary>
            <div className="admin-facts">
              {configEntries.map(([key, value]) => (
                <Fact
                  key={key}
                  label={key}
                  value={typeof value === "object" ? JSON.stringify(value) : String(value)}
                />
              ))}
            </div>
          </details>
        </section>
      ) : null}

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">Schema</h2>
            <p className="settings-page__section-copy">
              Migrations applied beyond the additive schema the row structs own. Not a
              schema version.
            </p>
          </div>
        </div>
        {system.schema_migrations.length === 0 ? (
          <p className="admin-empty">No migrations have been applied.</p>
        ) : (
          <ul className="admin-list">
            {system.schema_migrations.map((migration) => (
              <li key={migration.id} className="admin-list__row">
                <span className="admin-list__main">{migration.id}</span>
                <time className="admin-list__meta">
                  {new Date(migration.applied_at).toLocaleString()}
                </time>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
