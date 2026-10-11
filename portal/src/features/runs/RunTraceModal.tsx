import { useEffect, useState } from "react"
import { BaseModal, Button, type Translate } from "@buildmax/gui"
import type {
  ApiRunProvenance,
  ApiTaskRunLLMCall,
  ApiTaskRunTrace,
  ApiTraceBoundary,
  ApiTraceMCP,
  ApiTraceToolCall,
  ApiTraceWorkspace,
} from "../../lib/api/types"
import { getErrorMessage } from "../../lib/errorMessage"
import { useStableT, useT, type MessageKey } from "../../i18n"
import { navigate } from "../../router"
import { formatSize } from "../artifacts"
import { getTaskRunProvenance, getTaskRunTrace, listTaskRunLLMCalls } from "./api"
import {
  describeAgent,
  describeOrigin,
  describeSpaceInstructions,
  inputMatchesMessage,
} from "./origin"
import { cacheSaving, callElapsed, describeSpend, formatAmount, summarizeSpend } from "./spend"
import { describeBoundary, describeMCP, formatDuration, runElapsed } from "./summary"
import { explainRunFailure } from "./failure"
import { FailureFixLink, RunFailureNotice } from "./RunFailureNotice"
import { useSecretName } from "./useSecretName"

interface RunTraceModalProps {
  open: boolean
  spaceId: string | null
  token: string | null
  taskRunId: string | null
  onClose: () => void
}

/**
 * Boundary is stated first and in words, not as a checkmark.
 *
 * The sandbox is off by default on every surface today, so most runs report
 * false. That is exactly why it is shown plainly: a viewer who cannot tell a
 * confined run from an unconfined one has no reason to trust either.
 */
function BoundaryLine({ boundary }: { boundary?: ApiTraceBoundary }) {
  const t = useT()
  const described = describeBoundary(boundary, t)
  return (
    <p className={`run-trace__boundary run-trace__boundary--${described.tone}`}>
      {described.text}
      {described.sources ? (
        <span className="run-trace__sources">{t("runs.boundary.decidedBy", { sources: described.sources })}</span>
      ) : null}
    </p>
  )
}

/**
 * The MCP treatment, shown beside the boundary because it answers the same
 * question about a different transport: what BuildMax launched, and what it
 * refused. Distinct copy for the profile-enforced, allowed, and unknown cases
 * keeps a trace that never recorded the treatment from reading as "allowed".
 */
function MCPLine({ mcp }: { mcp?: ApiTraceMCP }) {
  const t = useT()
  const described = describeMCP(mcp, t)
  return (
    <p className={`run-trace__mcp run-trace__mcp--${described.tone}`}>{described.text}</p>
  )
}

function ToolRow({ tool }: { tool: ApiTraceToolCall }) {
  const t = useT()
  return (
    <li className={tool.denied ? "run-trace__tool run-trace__tool--denied" : "run-trace__tool"}>
      <span className="run-trace__tool-name">{tool.name}</span>
      {tool.path ? <span className="run-trace__tool-path">{tool.path}</span> : null}
      {tool.denied ? (
        <span className="run-trace__tool-denied">
          {t("runs.trace.denied")}
          {tool.deny_reason ? ` · ${tool.deny_reason}` : ""}
        </span>
      ) : (
        <span className="run-trace__tool-duration">{formatDuration(tool.duration_ms)}</span>
      )}
    </li>
  )
}

function TraceBody({ trace }: { trace: ApiTaskRunTrace }) {
  const t = useT()
  const tools = trace.tools ?? []
  const files = trace.files_changed ?? []
  return (
    <>
      <BoundaryLine boundary={trace.boundary} />
      <MCPLine mcp={trace.mcp} />

      {/* An unfinished run is not a successful one. Say so before the numbers,
          which otherwise read as a complete accounting. */}
      {!trace.complete ? (
        <p className="run-trace__incomplete" role="status">
          {t("runs.trace.incomplete")}
        </p>
      ) : null}

      {trace.error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">{t("runs.trace.failed")}</span>
          <pre className="run-trace__error-text">{trace.error}</pre>
        </div>
      ) : null}

      <dl className="run-trace__stats">
        <div>
          <dt>{t("runs.trace.model")}</dt>
          <dd>{trace.model || "—"}</dd>
        </div>
        <div>
          <dt>{t("runs.trace.duration")}</dt>
          <dd>{runElapsed(trace)}</dd>
        </div>
        <div>
          <dt>{t("runs.trace.modelCalls")}</dt>
          <dd>{trace.llm_calls}</dd>
        </div>
        <div>
          <dt>{t("runs.trace.toolCalls")}</dt>
          <dd>{trace.tool_calls}</dd>
        </div>
        <div>
          <dt>{t("runs.trace.tokens")}</dt>
          <dd>
            {t("runs.trace.tokensValue", {
              prompt: trace.prompt_tokens.toLocaleString(),
              completion: trace.completion_tokens.toLocaleString(),
            })}
          </dd>
        </div>
        {trace.compactions > 0 ? (
          <div>
            <dt>{t("runs.trace.compactions")}</dt>
            <dd>{trace.compactions}</dd>
          </div>
        ) : null}
      </dl>

      {files.length > 0 ? (
        <section className="run-trace__section">
          <h3 className="run-trace__heading">{t("runs.trace.filesChanged")}</h3>
          <ul className="run-trace__files">
            {files.map((path) => (
              <li key={path}>{path}</li>
            ))}
          </ul>
        </section>
      ) : null}

      {tools.length > 0 ? (
        <section className="run-trace__section">
          <h3 className="run-trace__heading">{t("runs.trace.toolCalls")}</h3>
          <ul className="run-trace__tools">
            {tools.map((tool, i) => (
              <ToolRow key={`${tool.name}-${i}`} tool={tool} />
            ))}
          </ul>
          {/* A short list must never be mistaken for a short run. */}
          {trace.tools_truncated ? (
            <p className="run-trace__truncated">
              {t("runs.trace.toolsTruncated", { shown: tools.length, total: trace.tool_calls })}
            </p>
          ) : null}
        </section>
      ) : null}
    </>
  )
}

// workspaceRestoreLabel and workspaceCheckpointLabel turn the run's recorded
// status codes into a phrase a reader understands. An empty status is the common
// case, not an error: a first run restores nothing, and a reply-only run
// captures nothing.
function workspaceRestoreLabel(status: string | undefined, t: Translate<MessageKey>): string {
  switch (status) {
    case "restored":
      return t("runs.workspace.restored")
    case "failed":
      return t("runs.workspace.restoreFailed")
    case "pending":
      return t("runs.workspace.restoring")
    default:
      return t("runs.workspace.restoreNotRequired")
  }
}

function workspaceCheckpointLabel(status: string | undefined, t: Translate<MessageKey>): string {
  switch (status) {
    case "committed":
      return t("runs.workspace.committed")
    case "failed":
      return t("runs.workspace.captureFailed")
    case "pending":
      return t("runs.workspace.capturing")
    default:
      return t("runs.workspace.notCaptured")
  }
}

/**
 * What happened to this run's workspace: whether it restored the base it was
 * given, and whether it committed a checkpoint of what it produced. Read-only —
 * the run recorded these as it ran. A failure carries the bounded reason.
 */
function WorkspaceSection({ workspace }: { workspace?: ApiTraceWorkspace }) {
  const t = useT()
  const ws = workspace ?? {}
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.workspace.heading")}</h3>
      <dl className="run-trace__stats">
        <div>
          <dt>{t("runs.workspace.restore")}</dt>
          <dd>{workspaceRestoreLabel(ws.restore_status, t)}</dd>
        </div>
        <div>
          <dt>{t("runs.workspace.checkpoint")}</dt>
          <dd>{workspaceCheckpointLabel(ws.checkpoint_status, t)}</dd>
        </div>
      </dl>
      {ws.restore_error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">{t("runs.workspace.restoreError")}</span>
          <pre className="run-trace__error-text">{ws.restore_error}</pre>
        </div>
      ) : null}
      {ws.checkpoint_error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">{t("runs.workspace.checkpointError")}</span>
          <pre className="run-trace__error-text">{ws.checkpoint_error}</pre>
        </div>
      ) : null}
    </section>
  )
}

function SpendCallRow({ call }: { call: ApiTaskRunLLMCall }) {
  const t = useT()
  const failed = call.status === "FAILED" || call.status === "CANCELED"
  const tokens =
    typeof call.total_tokens === "number"
      ? t("runs.call.tokens", { tokens: call.total_tokens.toLocaleString() })
      : // An unreported count is not a free call, so it says so rather than
        // showing a zero the provider never sent.
        t("runs.call.usageNotReported")
  return (
    <li className={failed ? "run-trace__call run-trace__call--failed" : "run-trace__call"}>
      <span className="run-trace__call-model">{call.model || "—"}</span>
      <span className="run-trace__call-tokens">{tokens}</span>
      {failed ? (
        <span className="run-trace__call-failed">
          {t(call.status === "FAILED" ? "runs.call.failed" : "runs.call.canceled")}
          {call.error_class ? ` · ${call.error_class}` : ""}
        </span>
      ) : (
        <span className="run-trace__call-duration">{callElapsed(call)}</span>
      )}
      {/* A per-call cache note appears only where the provider sent one, so a
          row without it means "not reported", not "missed". */}
      {(call.cache_read_tokens ?? 0) > 0 || (call.cache_write_tokens ?? 0) > 0 ? (
        <span className="run-trace__call-cache">
          {t("runs.call.cache", {
            read: (call.cache_read_tokens ?? 0).toLocaleString(),
            write: (call.cache_write_tokens ?? 0).toLocaleString(),
          })}
        </span>
      ) : null}
      {typeof call.attempts === "number" && call.attempts > 1 ? (
        <span className="run-trace__call-attempts">{t("runs.call.attempts", { count: call.attempts })}</span>
      ) : null}
    </li>
  )
}

/**
 * What the deployment was asked to serve for this run, and on which model.
 *
 * This is a different record from the trace above it. The trace is what the
 * agent did, written by the run itself; this is the governance ledger, written
 * by the server as it served each call — the same rows a space's quota is
 * computed from. When the two disagree about how many calls a run made, that
 * gap is the point: it means the run reached a provider the server never saw.
 */
function SpendSection({
  calls,
  error,
  trace,
}: {
  calls: ApiTaskRunLLMCall[]
  error: string | null
  trace: ApiTaskRunTrace | null
}) {
  const t = useT()
  const note = describeSpend({ calls, error, trace }, t)
  const summary = summarizeSpend(calls)
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.spend.heading")}</h3>
      {note ? (
        <p className="run-trace__spend-note" role={error ? "alert" : undefined}>
          {note}
        </p>
      ) : (
        <>
          <dl className="run-trace__stats">
            <div>
              <dt>{t("runs.spend.accountedCalls")}</dt>
              <dd>{summary.calls}</dd>
            </div>
            <div>
              <dt>{t("runs.spend.accountedTokens")}</dt>
              <dd>
                {summary.totalTokens.toLocaleString()}
                {summary.unreported > 0 ? (
                  <span className="run-trace__unreported">
                    {" "}
                    · {t("runs.spend.unreported", { count: summary.unreported })}
                  </span>
                ) : null}
              </dd>
            </div>
            {/* Shown only once a provider has reported cache counts. A
                permanent "0 / 0" would read as a measured miss on the many
                providers that report nothing at all. */}
            {summary.cacheReadTokens > 0 || summary.cacheWriteTokens > 0 ? (
              <div>
                <dt>{t("runs.spend.cached")}</dt>
                <dd>
                  {summary.cacheReadTokens.toLocaleString()} /{" "}
                  {summary.cacheWriteTokens.toLocaleString()}
                  {summary.cacheUnreported > 0 ? (
                    <span className="run-trace__unreported">
                      {" "}
                      · {t("runs.spend.cacheUnreported", { count: summary.cacheUnreported })}
                    </span>
                  ) : null}
                </dd>
              </div>
            ) : null}
            {/* Cost is shown only where every rate needed for it was recorded.
                An estimate assembled from half a price list looks
                authoritative and is not. */}
            <div>
              <dt>{t("runs.spend.estimatedCost")}</dt>
              <dd>
                {summary.cost ? (
                  <>
                    {formatAmount(summary.cost.total, summary.cost.currency)}
                    {summary.unpriced > 0 ? (
                      <span className="run-trace__unreported">
                        {" "}
                        · {t("runs.spend.unpriced", { count: summary.unpriced })}
                      </span>
                    ) : null}
                  </>
                ) : (
                  <span className="run-trace__unreported">{t("runs.spend.unavailable")}</span>
                )}
              </dd>
            </div>
            {/* Reported only when positive. A run that wrote cache entries
                nothing read back paid more than it would have uncached, and
                calling that a small saving would be a false claim. */}
            {summary.cost && cacheSaving(summary.cost) !== null ? (
              <div>
                <dt>{t("runs.spend.savedByCaching")}</dt>
                <dd>
                  {formatAmount(cacheSaving(summary.cost) ?? 0, summary.cost.currency)}
                  <span className="run-trace__unreported">
                    {" "}
                    · {t("runs.spend.uncached", { amount: formatAmount(summary.cost.baseline, summary.cost.currency) })}
                  </span>
                </dd>
              </div>
            ) : null}
            {summary.failed > 0 ? (
              <div>
                <dt>{t("runs.spend.failedCalls")}</dt>
                <dd>{summary.failed}</dd>
              </div>
            ) : null}
            {summary.inFlight > 0 ? (
              <div>
                <dt>{t("runs.spend.unfinishedCalls")}</dt>
                <dd>{summary.inFlight}</dd>
              </div>
            ) : null}
            {summary.retried > 0 ? (
              <div>
                <dt>{t("runs.spend.retries")}</dt>
                <dd>{summary.retried}</dd>
              </div>
            ) : null}
          </dl>

          {/* Named even when there is one, because which model a run spent on
              is the governance question. */}
          <ul className="run-trace__models">
            {summary.byModel.map((entry) => (
              <li key={entry.model}>
                <span className="run-trace__call-model">{entry.model}</span>
                <span className="run-trace__call-tokens">
                  {t("runs.spend.modelUsage", { count: entry.calls, tokens: entry.totalTokens.toLocaleString() })}
                </span>
              </li>
            ))}
          </ul>

          <ul className="run-trace__calls">
            {calls.map((call) => (
              <SpendCallRow key={call.id} call={call} />
            ))}
          </ul>
        </>
      )}
    </section>
  )
}

/**
 * Where the run came from, and what was actually asked for.
 *
 * This sits above the trace because it is the question that comes first and the
 * one that survives every absence below it: a run that failed before an agent
 * started wrote no trace and still came from somewhere.
 *
 * The message and the instruction are shown together on purpose. The run input
 * is what Tier 1 decided to send a worker; the quote is what the person said. A
 * constraint present in one and missing from the other is exactly what a reader
 * is here to find, and it cannot be seen unless both are in front of them.
 */
function OriginSection({
  provenance,
  error,
}: {
  provenance: ApiRunProvenance | null
  error: string | null
}) {
  const t = useT()
  if (!provenance) {
    return (
      <section className="run-trace__section">
        <h3 className="run-trace__heading">{t("runs.origin.heading")}</h3>
        <p className="run-trace__spend-note" role={error ? "alert" : undefined}>
          {error ?? t("runs.origin.notRecorded")}
        </p>
      </section>
    )
  }
  const origin = describeOrigin(provenance, t)
  const agent = describeAgent(provenance, t)
  const spaceInstructions = describeSpaceInstructions(provenance, t)
  const said = provenance.source_message
  const verbatim = inputMatchesMessage(provenance)
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.origin.heading")}</h3>
      <p className="run-trace__origin-text">{origin.text}</p>
      {spaceInstructions ? (
        <p
          className={
            spaceInstructions.driftedSinceRun
              ? "run-trace__origin-text run-trace__origin-text--drifted"
              : "run-trace__origin-text"
          }
        >
          {spaceInstructions.text}
        </p>
      ) : null}
      {agent ? (
        <p
          className={
            agent.driftedSinceRun
              ? "run-trace__origin-text run-trace__origin-text--drifted"
              : "run-trace__origin-text"
          }
        >
          {agent.text}
        </p>
      ) : null}
      {said ? (
        <>
          <span className="run-trace__origin-label">{t("runs.origin.askedFor")}</span>
          <pre className="run-trace__quote">{said.content}</pre>
          {said.truncated ? (
            <p className="run-trace__truncated">
              {t("runs.origin.quoteTruncated")}
            </p>
          ) : null}
        </>
      ) : (
        <p className="run-trace__spend-note">
          {origin.quote === "none-expected"
            ? origin.isRepeat
              ? t("runs.origin.noneRepeat")
              : t("runs.origin.noneDispatched")
            : t("runs.origin.noneRecorded")}
        </p>
      )}
      <span className="run-trace__origin-label">{t("runs.origin.sentToWorker")}</span>
      <pre className="run-trace__quote">{provenance.input}</pre>
      {said && !said.truncated ? (
        <p className="run-trace__truncated">
          {verbatim
            ? t("runs.origin.verbatim")
            : t("runs.origin.rewritten")}
        </p>
      ) : null}
    </section>
  )
}

/**
 * The releases this run actually resolved -- fixed at dispatch, and not the
 * same question as what the agent currently names. See
 * docs/design/portal-data-and-plugin-surfaces.md.
 */
function PluginsSection({
  pins,
  spaceId,
  onClose,
}: {
  pins: ApiRunProvenance["plugin_pins"]
  spaceId: string | null
  onClose: () => void
}) {
  const t = useT()
  if (!pins || pins.length === 0) return null
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.plugins.heading")}</h3>
      <ul className="run-trace__tools">
        {pins.map((pin) => (
          <li key={pin.plugin_name} className="run-trace__tool">
            <span className="run-trace__tool-name">
              {pin.plugin_name}@{pin.version}
            </span>
          </li>
        ))}
      </ul>
      <button
        type="button"
        className="run-trace__link"
        onClick={() => {
          onClose()
          if (spaceId) navigate({ name: "space", spaceId, section: "plugins" })
        }}
      >
        {t("runs.plugins.open")}
      </button>
    </section>
  )
}

/**
 * Why a failed run ended, in the person's terms, with the fix it leads with.
 * Placed right after the origin: it is the question a reader of a failed run
 * opens this dialog to answer.
 */
function FailureSection({
  provenance,
  spaceId,
  token,
  onClose,
}: {
  provenance: ApiRunProvenance
  spaceId: string
  token: string | null
  onClose: () => void
}) {
  const t = useT()
  const secretName = useSecretName(spaceId, token, provenance.failure_cause?.secret_id)
  const explanation = explainRunFailure(
    {
      status: provenance.status,
      failureClass: provenance.failure_class,
      failureCause: provenance.failure_cause,
      agentId: provenance.agent?.id,
      errorMessage: provenance.error_message,
    },
    { agent: provenance.agent?.name, secret: secretName },
  )
  if (!explanation) return null
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.failure.heading")}</h3>
      <RunFailureNotice explanation={explanation} />
      {explanation.leadsWithFix && provenance.agent?.id ? (
        <div className="run-trace__failure-actions">
          <FailureFixLink explanation={explanation} spaceId={spaceId} agentId={provenance.agent.id} size="compact" onNavigate={onClose} />
        </div>
      ) : null}
    </section>
  )
}

/** What this run published, addressed by the artifact's own id. */
function ArtifactsSection({
  artifacts,
  onClose,
}: {
  artifacts: ApiRunProvenance["artifacts"]
  onClose: () => void
}) {
  const t = useT()
  if (!artifacts || artifacts.length === 0) return null
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">{t("runs.artifacts.heading")}</h3>
      <ul className="run-trace__tools">
        {artifacts.map((artifact) => (
          <li key={artifact.id} className="run-trace__tool">
            <button
              type="button"
              className="run-trace__link"
              onClick={() => {
                onClose()
                navigate({ name: "artifact", artifactId: artifact.id })
              }}
            >
              {artifact.title?.trim() || artifact.filename}
            </button>
            {typeof artifact.size_bytes === "number" ? (
              <span className="run-trace__tool-duration">{formatSize(artifact.size_bytes)}</span>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  )
}

/**
 * RunTraceModal answers where a run came from, what it used, touched, spent,
 * produced, why it ended, and what confined it.
 */
export function RunTraceModal({ open, spaceId, token, taskRunId, onClose }: RunTraceModalProps) {
  const t = useT()
  const stableT = useStableT()
  const [trace, setTrace] = useState<ApiTaskRunTrace | null>(null)
  const [provenance, setProvenance] = useState<ApiRunProvenance | null>(null)
  const [provenanceError, setProvenanceError] = useState<string | null>(null)
  const [calls, setCalls] = useState<ApiTaskRunLLMCall[]>([])
  const [callsError, setCallsError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !spaceId || !token || !taskRunId) {
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setTrace(null)
    setCalls([])
    setCallsError(null)
    setProvenance(null)
    setProvenanceError(null)

    // The records are fetched together and fail apart. A run whose trace
    // expired from storage still has a ledger, and a deployment that accounts
    // no managed calls still has a trace — neither absence may hide the other.
    const fetchTrace = () =>
      getTaskRunTrace(spaceId, taskRunId, token)
        .then((result) => {
          if (!cancelled) setTrace(result)
        })
        .catch((err) => {
          // The server explains a missing trace precisely — never recorded, or
          // gone from storage. Pass its message through.
          if (!cancelled) setError(getErrorMessage(err, stableT("runs.error.trace")))
        })
    const callsRequest = listTaskRunLLMCalls(spaceId, taskRunId, token)
      .then((result) => {
        if (!cancelled) setCalls(result)
      })
      .catch((err) => {
        if (!cancelled) {
          setCallsError(getErrorMessage(err, stableT("runs.error.calls")))
        }
      })

    // The run record says whether a trace exists, so a run that has none is
    // told so here rather than by a request that can only fail.
    const provenanceAndTrace = getTaskRunProvenance(spaceId, taskRunId, token).then(
      (result) => {
        if (cancelled) return
        setProvenance(result)
        if (result.trace_recorded) return fetchTrace()
      },
      (err) => {
        if (cancelled) return
        setProvenanceError(getErrorMessage(err, stableT("runs.error.origin")))
        return fetchTrace()
      },
    )

    void Promise.all([provenanceAndTrace, callsRequest]).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [open, spaceId, token, taskRunId, stableT])

  return (
    <BaseModal
      open={open}
      title={t("runs.title")}
      titleId="run-trace-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <p className="modal__hint">{taskRunId ?? ""}</p>
        {loading ? (
          <p className="page-activity__empty">{t("shell.loading")}</p>
        ) : (
          <>
            {/* First and unconditional: a run that wrote no trace still came
                from somewhere, and that is the question a reader opens with. */}
            <OriginSection provenance={provenance} error={provenanceError} />
            {provenance && spaceId ? (
              <FailureSection provenance={provenance} spaceId={spaceId} token={token} onClose={onClose} />
            ) : null}
            <PluginsSection pins={provenance?.plugin_pins} spaceId={spaceId} onClose={onClose} />
            {error ? (
              <p className="modal__error" role="alert">{error}</p>
            ) : trace ? (
              <TraceBody trace={trace} />
            ) : provenance && !provenance.trace_recorded ? (
              <p className="run-trace__spend-note">{t("runs.trace.notRecorded")}</p>
            ) : null}
            {/* What became of the run's workspace — restore and checkpoint — which
                the run records separately from its trace, so it shows whenever the
                trace does. */}
            {trace ? <WorkspaceSection workspace={trace.workspace} /> : null}
            <ArtifactsSection artifacts={provenance?.artifacts} onClose={onClose} />
            {/* Shown even when the trace could not be read: what a run spent is
                accounted server-side and survives a trace that did not. */}
            <SpendSection calls={calls} error={callsError} trace={trace} />
          </>
        )}
      </div>
      <div className="modal__actions">
        <Button variant="secondary" onClick={onClose}>
          {t("runs.close")}
        </Button>
      </div>
    </BaseModal>
  )
}
