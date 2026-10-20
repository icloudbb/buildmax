import { Spinner, StatusLabel, useLocale } from "@buildmax/gui"
import { formatTimestamp } from "../../lib/dateFormat"
import { useEffect, useState } from "react"
import { CopyableId } from "../../components/CopyableId"
import { useStableT, useT } from "../../i18n"
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
  dependencyLabel,
  dependencyStatusLabel,
  formatCount,
  orderedRunStatuses,
  workerRunModeLabel,
  workerTransportLabel,
} from "./labels"
import { systemRoleLabel } from "./roles"
import {
  FAILURE_CLASSES,
  WORKFLOW_FAILURE_CLASSES,
  ageSince,
  failureLabel,
  failureNextStep,
  orderedFailures,
  timeUntil,
  waitingSummary,
} from "./runtime"

function StatusPill({ ok, label }: { ok: boolean; label: string }) {
  return (
    <span className={ok ? "admin-pill admin-pill--ok" : "admin-pill admin-pill--bad"}>{label}</span>
  )
}

/** One failure class per row, with who acts on it and what they check. */
function FailureTable({
  failures,
  classes,
}: {
  failures: [string, number][]
  classes: typeof FAILURE_CLASSES
}) {
  const t = useT()
  const { locale } = useLocale()
  return (
    <table className="admin-table admin-table--prose">
      <thead>
        <tr>
          <th scope="col">{t("admin.overview.cause")}</th>
          <th scope="col">{t("admin.overview.runs")}</th>
          <th scope="col">{t("admin.overview.nextStepCol")}</th>
        </tr>
      </thead>
      <tbody>
        {failures.map(([cls, n]) => (
          <tr key={cls}>
            <td className="admin-table__label">{failureLabel(cls, t, classes)}</td>
            <td className="admin-table__nowrap">{formatCount(n, locale)}</td>
            <td className="admin-table__muted">{failureNextStep(cls, t, classes)}</td>
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
  const t = useT()
  const summary = waitingSummary(space.waiting_requests, t)
  if (!summary) return <>—</>
  const expiry = timeUntil(space.next_request_expiry_at, serverTime, t)
  return (
    <>
      {summary}
      <span className="admin-table__muted">
        {t("admin.overview.waitingOldest", {
          age: ageSince(space.oldest_waiting_request_at, serverTime, t) ?? "—",
        })}
        {expiry ? t("admin.overview.waitingExpiry", { expiry }) : null}
      </span>
      {space.oldest_waiting_workflow_run_id ? (
        <div>
          <CopyableId label={t("admin.overview.oldestWaitingRun")} id={space.oldest_waiting_workflow_run_id} />
        </div>
      ) : null}
    </>
  )
}

/**
 * One failure class and its count, then who acts and what they check. English
 * lowercases the label after its count; Chinese keeps it as is, since
 * lowercasing would mangle the Latin terms it keeps (Agent, Worker).
 */
function FailureLines({
  failures,
  classes,
}: {
  failures: [string, number][]
  classes: typeof FAILURE_CLASSES
}) {
  const t = useT()
  const { locale } = useLocale()
  return (
    <ul className="admin-failures">
      {failures.map(([cls, count]) => {
        const label = failureLabel(cls, t, classes)
        return (
          <li key={cls}>
            <span className="admin-table__nowrap">
              {t("admin.overview.failureCount", {
                count: formatCount(count, locale),
                label: locale === "en" ? label.toLowerCase() : label,
              })}
            </span>
            <span className="admin-failures__next">{failureNextStep(cls, t, classes)}</span>
          </li>
        )
      })}
    </ul>
  )
}

/** Task run and Workflow run failures in the window, each by its own classes. */
function SpaceFailures({ space }: { space: ApiAdminSpaceAttention }) {
  const t = useT()
  const taskRuns = orderedFailures(space.failures)
  const workflowRuns = orderedFailures(space.workflow_failures, WORKFLOW_FAILURE_CLASSES)
  if (taskRuns.length === 0 && workflowRuns.length === 0) return <>—</>
  return (
    <>
      {taskRuns.length > 0 ? <FailureLines failures={taskRuns} classes={FAILURE_CLASSES} /> : null}
      {workflowRuns.length > 0 ? (
        <>
          <div className="admin-failures__group">{t("admin.overview.workflowRunFailures")}</div>
          <FailureLines failures={workflowRuns} classes={WORKFLOW_FAILURE_CLASSES} />
          {space.latest_failed_workflow_run_id ? (
            <CopyableId label={t("admin.overview.latestFailedRun")} id={space.latest_failed_workflow_run_id} />
          ) : null}
        </>
      ) : null}
    </>
  )
}

/** Run statuses by the shared vocabulary, each with its count. */
function StatusCounts({ counts }: { counts: Record<string, number> }) {
  const { locale } = useLocale()
  const entries = orderedRunStatuses(counts)
  if (entries.length === 0) return <>—</>
  return (
    <span className="admin-counts">
      {entries.map(([status, n]) => (
        <span key={status} className="admin-count">
          <StatusLabel status={status} />
          <span>{formatCount(n, locale)}</span>
        </span>
      ))}
    </span>
  )
}

/** A labelled value; `detail` is the raw configuration value, kept as secondary technical detail. */
function Fact({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div className="admin-fact">
      <span className="admin-fact__label">{label}</span>
      <span className="admin-fact__value">{value}</span>
      {detail ? <code className="admin-fact__detail">{detail}</code> : null}
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
  const t = useT()
  const stableT = useStableT()
  const { locale } = useLocale()
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
        if (!cancelled) setError(getErrorMessage(err, stableT("admin.overview.loadError")))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [token, stableT])

  if (loading) return <Spinner label={t("admin.overview.loading")} />
  if (error) {
    return (
      <p className="settings-section__error" role="alert">
        {error}
      </p>
    )
  }
  if (!system) return null

  const warnings = Array.isArray(config?.warnings) ? (config.warnings as string[]) : []
  const runStatuses = orderedRunStatuses(system.task_runs)
  const runtime = system.runtime
  const failures = orderedFailures(runtime?.failures)
  const workflowFailures = orderedFailures(runtime?.workflow_failures, WORKFLOW_FAILURE_CLASSES)
  const myGrant = me?.grants?.[0]
  const grantedBy = myGrant
    ? myGrant.granted_by === "buildmax-server"
      ? t("admin.overview.operatorCommand")
      : myGrant.granted_by
    : ""
  // The redacted config minus warnings, which have their own section. The server
  // has already reduced every credential to a set/not-set boolean, so this is
  // safe to render whole.
  const configEntries = Object.entries(config ?? {}).filter(([key]) => key !== "warnings")
  const noneWaiting = t("admin.overview.noneWaiting")

  return (
    <div className="admin-sections">
      {myGrant ? (
        <section className="settings-page__section">
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">{t("admin.overview.accessTitle")}</h2>
              <p className="settings-page__section-copy">{t("admin.overview.accessCopy")}</p>
            </div>
          </div>
          <div className="admin-facts">
            <Fact label={t("admin.overview.role")} value={systemRoleLabel(myGrant.role, t)} />
            <Fact label={t("admin.overview.grantedBy")} value={grantedBy} />
            <Fact
              label={t("admin.overview.granted")}
              value={formatTimestamp(myGrant.granted_at, locale)}
            />
          </div>
        </section>
      ) : null}

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.overview.healthTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.overview.healthCopy")}</p>
          </div>
          <StatusPill
            ok={system.ready}
            label={system.ready ? t("admin.overview.ready") : t("admin.overview.notReady")}
          />
        </div>
        <div className="admin-facts">
          {system.dependencies.length === 0 ? (
            <p className="admin-empty">{t("admin.overview.noDependencies")}</p>
          ) : (
            system.dependencies.map((dep) => (
              <div key={dep.name} className="admin-fact">
                <span className="admin-fact__label">{dependencyLabel(dep.name, t)}</span>
                <StatusPill ok={dep.status === "ok"} label={dependencyStatusLabel(dep.status, t)} />
              </div>
            ))
          )}
        </div>
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.overview.deploymentTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.overview.deploymentCopy")}</p>
          </div>
        </div>
        <div className="admin-facts">
          <Fact label={t("admin.overview.version")} value={system.version} />
          <Fact
            label={t("admin.overview.workerRunMode")}
            value={system.worker_run_mode ? workerRunModeLabel(system.worker_run_mode, t) : t("admin.overview.unknown")}
            detail={system.worker_run_mode}
          />
          <Fact
            label={t("admin.overview.workerTransport")}
            value={
              system.worker_llm_transport
                ? workerTransportLabel(system.worker_llm_transport, t)
                : t("admin.overview.unknown")
            }
            detail={system.worker_llm_transport}
          />
          {/*
            Empty means no worker path reports a sandbox surface, which is every
            deployment today. It is not a claim that runs are unconfined — each
            run's details say how it was sandboxed — so the label says exactly
            what is missing: the report.
          */}
          <Fact
            label={t("admin.overview.sandboxSurface")}
            value={system.sandbox_surface ? system.sandbox_surface : t("admin.overview.notReported")}
          />
          <Fact
            label={t("admin.overview.selfRegistration")}
            value={system.allow_signup ? t("admin.overview.open") : t("admin.overview.closed")}
          />
          <Fact label={t("admin.overview.systemAdmins")} value={formatCount(system.system_admins, locale)} />
        </div>
      </section>

      <section className="settings-page__section">
        <div className="settings-page__section-head">
          <div>
            <h2 className="settings-page__section-title">{t("admin.overview.taskRunsTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.overview.taskRunsCopy")}</p>
          </div>
        </div>
        <div className="admin-facts">
          {runStatuses.length === 0 ? (
            <p className="admin-empty">{t("admin.overview.noRuns")}</p>
          ) : (
            runStatuses.map(([status, count]) => (
              <div key={status} className="admin-fact">
                <StatusLabel status={status} />
                <span className="admin-fact__value">{formatCount(count, locale)}</span>
              </div>
            ))
          )}
        </div>
      </section>

      <section className="settings-page__section" aria-labelledby="admin-work-progress">
        <div className="settings-page__section-head">
          <div>
            <h2 id="admin-work-progress" className="settings-page__section-title">
              {t("admin.overview.progressTitle")}
            </h2>
            <p className="settings-page__section-copy">{t("admin.overview.progressCopy")}</p>
          </div>
        </div>
        {runtime ? (
          <>
            <div className="admin-facts">
              <Fact
                label={t("admin.overview.oldestPending")}
                value={ageSince(runtime.oldest_pending_at, system.server_time, t) ?? noneWaiting}
              />
              <Fact
                label={t("admin.overview.oldestUnstarted")}
                value={ageSince(runtime.oldest_unstarted_at, system.server_time, t) ?? noneWaiting}
              />
              <Fact
                label={t("admin.overview.staleRunning", {
                  minutes: Math.round(runtime.stale_after_seconds / 60),
                })}
                value={formatCount(runtime.stale_running, locale)}
              />
              {/*
                A Workflow run waiting on a person holds no worker, so none of the
                task run numbers above show it. Any member of its Space may answer.
              */}
              <Fact
                label={t("admin.overview.waitingRequests")}
                value={waitingSummary(runtime.waiting_requests, t) ?? noneWaiting}
              />
              <Fact
                label={t("admin.overview.oldestWaitingRequest")}
                value={ageSince(runtime.oldest_waiting_request_at, system.server_time, t) ?? noneWaiting}
              />
              <Fact
                label={t("admin.overview.nextExpiry")}
                value={
                  timeUntil(runtime.next_request_expiry_at, system.server_time, t) ??
                  t("admin.overview.noneSet")
                }
              />
            </div>
            <h3 className="admin-subtitle">
              {t("admin.overview.taskFailures", { hours: runtime.failure_window_hours })}
            </h3>
            {failures.length === 0 ? (
              <p className="admin-empty">{t("admin.overview.noTaskFailures")}</p>
            ) : (
              <FailureTable failures={failures} classes={FAILURE_CLASSES} />
            )}
            <h3 className="admin-subtitle">
              {t("admin.overview.workflowFailuresTitle", { hours: runtime.failure_window_hours })}
            </h3>
            {workflowFailures.length === 0 ? (
              <p className="admin-empty">{t("admin.overview.noWorkflowFailures")}</p>
            ) : (
              <FailureTable failures={workflowFailures} classes={WORKFLOW_FAILURE_CLASSES} />
            )}
          </>
        ) : (
          <p className="admin-empty" role="status">
            {t("admin.overview.progressUnavailable")}
          </p>
        )}

        <h3 className="admin-subtitle">{t("admin.overview.attentionTitle")}</h3>
        {attention === null ? (
          <p className="admin-empty" role="status">
            {t("admin.overview.attentionUnavailable")}
          </p>
        ) : attention.spaces.length === 0 ? (
          <p className="admin-empty">{t("admin.overview.noAttention")}</p>
        ) : (
          <table className="admin-table admin-table--prose">
            <thead>
              <tr>
                <th scope="col">{t("admin.overview.colSpace")}</th>
                <th scope="col">{t("admin.overview.colOwners")}</th>
                <th scope="col">{t("admin.overview.colActive")}</th>
                <th scope="col">{t("admin.overview.colOldestActive")}</th>
                <th scope="col">{t("admin.overview.colWaiting")}</th>
                <th scope="col">{t("admin.overview.colFailed")}</th>
              </tr>
            </thead>
            <tbody>
              {attention.spaces.map((space) => (
                <tr key={space.space_id}>
                  <td>
                    <a
                      className="admin-link"
                      href={buildHash({ name: "admin", section: "spaces", spaceId: space.space_id })}
                    >
                      {space.name || space.space_id}
                    </a>
                    {space.personal ? (
                      <span className="admin-table__muted">{t("admin.overview.personal")}</span>
                    ) : null}
                  </td>
                  <td>
                    {space.owners.map((o) => o.email || o.user_id).join(t("admin.listSeparator")) || "—"}
                  </td>
                  <td>
                    <StatusCounts counts={space.active} />
                  </td>
                  <td className="admin-table__nowrap">
                    {ageSince(space.oldest_active_at, system.server_time, t) ?? "—"}
                  </td>
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
            {t("admin.overview.showingLongest", {
              shown: attention.spaces.length,
              total: attention.total,
            })}
          </p>
        ) : null}
      </section>

      {warnings.length > 0 ? (
        <section className="settings-page__section">
          <div className="settings-page__section-head">
            <div>
              <h2 className="settings-page__section-title">{t("admin.overview.notesTitle")}</h2>
              <p className="settings-page__section-copy">{t("admin.overview.notesCopy")}</p>
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
              <h2 className="settings-page__section-title">{t("admin.overview.configTitle")}</h2>
              <p className="settings-page__section-copy">
                {t("admin.overview.configCopyBefore")}
                <code>server.yaml</code>
                {t("admin.overview.configCopyAfter")}
              </p>
            </div>
          </div>
          <details className="admin-config">
            <summary className="admin-config__summary">{t("admin.overview.showConfig")}</summary>
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
            <h2 className="settings-page__section-title">{t("admin.overview.schemaTitle")}</h2>
            <p className="settings-page__section-copy">{t("admin.overview.schemaCopy")}</p>
          </div>
        </div>
        {system.schema_migrations.length === 0 ? (
          <p className="admin-empty">{t("admin.overview.noMigrations")}</p>
        ) : (
          <ul className="admin-list">
            {system.schema_migrations.map((migration) => (
              <li key={migration.id} className="admin-list__row">
                <span className="admin-list__main">{migration.id}</span>
                <time className="admin-list__meta">
                  {formatTimestamp(migration.applied_at, locale)}
                </time>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
