import { useEffect, useState } from "react"
import { BaseModal, Button } from "@buildmax/gui"
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
  const described = describeBoundary(boundary)
  return (
    <p className={`run-trace__boundary run-trace__boundary--${described.tone}`}>
      {described.text}
      {described.sources ? (
        <span className="run-trace__sources">Decided by: {described.sources}</span>
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
  const described = describeMCP(mcp)
  return (
    <p className={`run-trace__mcp run-trace__mcp--${described.tone}`}>{described.text}</p>
  )
}

function ToolRow({ tool }: { tool: ApiTraceToolCall }) {
  return (
    <li className={tool.denied ? "run-trace__tool run-trace__tool--denied" : "run-trace__tool"}>
      <span className="run-trace__tool-name">{tool.name}</span>
      {tool.path ? <span className="run-trace__tool-path">{tool.path}</span> : null}
      {tool.denied ? (
        <span className="run-trace__tool-denied">
          denied{tool.deny_reason ? ` · ${tool.deny_reason}` : ""}
        </span>
      ) : (
        <span className="run-trace__tool-duration">{formatDuration(tool.duration_ms)}</span>
      )}
    </li>
  )
}

function TraceBody({ trace }: { trace: ApiTaskRunTrace }) {
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
          This run wrote no terminal record — it was killed, or its trace was cut short.
          The figures below cover only what was written.
        </p>
      ) : null}

      {trace.error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">Failed</span>
          <pre className="run-trace__error-text">{trace.error}</pre>
        </div>
      ) : null}

      <dl className="run-trace__stats">
        <div>
          <dt>Model</dt>
          <dd>{trace.model || "—"}</dd>
        </div>
        <div>
          <dt>Duration</dt>
          <dd>{runElapsed(trace)}</dd>
        </div>
        <div>
          <dt>Model calls</dt>
          <dd>{trace.llm_calls}</dd>
        </div>
        <div>
          <dt>Tool calls</dt>
          <dd>{trace.tool_calls}</dd>
        </div>
        <div>
          <dt>Tokens</dt>
          <dd>
            {trace.prompt_tokens.toLocaleString()} in · {trace.completion_tokens.toLocaleString()} out
          </dd>
        </div>
        {trace.compactions > 0 ? (
          <div>
            <dt>Compactions</dt>
            <dd>{trace.compactions}</dd>
          </div>
        ) : null}
      </dl>

      {files.length > 0 ? (
        <section className="run-trace__section">
          <h3 className="run-trace__heading">Files changed</h3>
          <ul className="run-trace__files">
            {files.map((path) => (
              <li key={path}>{path}</li>
            ))}
          </ul>
        </section>
      ) : null}

      {tools.length > 0 ? (
        <section className="run-trace__section">
          <h3 className="run-trace__heading">Tool calls</h3>
          <ul className="run-trace__tools">
            {tools.map((tool, i) => (
              <ToolRow key={`${tool.name}-${i}`} tool={tool} />
            ))}
          </ul>
          {/* A short list must never be mistaken for a short run. */}
          {trace.tools_truncated ? (
            <p className="run-trace__truncated">
              Showing the first {tools.length} of {trace.tool_calls} calls.
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
function workspaceRestoreLabel(status?: string): string {
  switch (status) {
    case "restored":
      return "Restored from checkpoint"
    case "failed":
      return "Restore failed"
    case "pending":
      return "Restoring…"
    default:
      return "Not required"
  }
}

function workspaceCheckpointLabel(status?: string): string {
  switch (status) {
    case "committed":
      return "Committed"
    case "failed":
      return "Capture failed"
    case "pending":
      return "Capturing…"
    default:
      return "Not captured"
  }
}

/**
 * What happened to this run's workspace: whether it restored the base it was
 * given, and whether it committed a checkpoint of what it produced. Read-only —
 * the run recorded these as it ran. A failure carries the bounded reason.
 */
function WorkspaceSection({ workspace }: { workspace?: ApiTraceWorkspace }) {
  const ws = workspace ?? {}
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">Workspace</h3>
      <dl className="run-trace__stats">
        <div>
          <dt>Restore</dt>
          <dd>{workspaceRestoreLabel(ws.restore_status)}</dd>
        </div>
        <div>
          <dt>Checkpoint</dt>
          <dd>{workspaceCheckpointLabel(ws.checkpoint_status)}</dd>
        </div>
      </dl>
      {ws.restore_error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">Restore error</span>
          <pre className="run-trace__error-text">{ws.restore_error}</pre>
        </div>
      ) : null}
      {ws.checkpoint_error ? (
        <div className="run-trace__error" role="alert">
          <span className="run-trace__label">Checkpoint error</span>
          <pre className="run-trace__error-text">{ws.checkpoint_error}</pre>
        </div>
      ) : null}
    </section>
  )
}

function SpendCallRow({ call }: { call: ApiTaskRunLLMCall }) {
  const failed = call.status === "FAILED" || call.status === "CANCELED"
  const tokens =
    typeof call.total_tokens === "number"
      ? `${call.total_tokens.toLocaleString()} tokens`
      : // An unreported count is not a free call, so it says so rather than
        // showing a zero the provider never sent.
        "usage not reported"
  return (
    <li className={failed ? "run-trace__call run-trace__call--failed" : "run-trace__call"}>
      <span className="run-trace__call-model">{call.model || "—"}</span>
      <span className="run-trace__call-tokens">{tokens}</span>
      {failed ? (
        <span className="run-trace__call-failed">
          {call.status.toLowerCase()}
          {call.error_class ? ` · ${call.error_class}` : ""}
        </span>
      ) : (
        <span className="run-trace__call-duration">{callElapsed(call)}</span>
      )}
      {/* A per-call cache note appears only where the provider sent one, so a
          row without it means "not reported", not "missed". */}
      {(call.cache_read_tokens ?? 0) > 0 || (call.cache_write_tokens ?? 0) > 0 ? (
        <span className="run-trace__call-cache">
          cache {(call.cache_read_tokens ?? 0).toLocaleString()} r /{" "}
          {(call.cache_write_tokens ?? 0).toLocaleString()} w
        </span>
      ) : null}
      {typeof call.attempts === "number" && call.attempts > 1 ? (
        <span className="run-trace__call-attempts">{call.attempts} attempts</span>
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
  const note = describeSpend({ calls, error, trace })
  const summary = summarizeSpend(calls)
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">Managed model calls</h3>
      {note ? (
        <p className="run-trace__spend-note" role={error ? "alert" : undefined}>
          {note}
        </p>
      ) : (
        <>
          <dl className="run-trace__stats">
            <div>
              <dt>Accounted calls</dt>
              <dd>{summary.calls}</dd>
            </div>
            <div>
              <dt>Accounted tokens</dt>
              <dd>
                {summary.totalTokens.toLocaleString()}
                {summary.unreported > 0 ? (
                  <span className="run-trace__unreported">
                    {" "}
                    · {summary.unreported} call{summary.unreported === 1 ? "" : "s"} unreported
                  </span>
                ) : null}
              </dd>
            </div>
            {/* Shown only once a provider has reported cache counts. A
                permanent "0 / 0" would read as a measured miss on the many
                providers that report nothing at all. */}
            {summary.cacheReadTokens > 0 || summary.cacheWriteTokens > 0 ? (
              <div>
                <dt>Cached prompt (read / write)</dt>
                <dd>
                  {summary.cacheReadTokens.toLocaleString()} /{" "}
                  {summary.cacheWriteTokens.toLocaleString()}
                  {summary.cacheUnreported > 0 ? (
                    <span className="run-trace__unreported">
                      {" "}
                      · {summary.cacheUnreported} call
                      {summary.cacheUnreported === 1 ? "" : "s"} reported no cache
                    </span>
                  ) : null}
                </dd>
              </div>
            ) : null}
            {/* Cost is shown only where every rate needed for it was recorded.
                An estimate assembled from half a price list looks
                authoritative and is not. */}
            <div>
              <dt>Estimated cost</dt>
              <dd>
                {summary.cost ? (
                  <>
                    {formatAmount(summary.cost.total, summary.cost.currency)}
                    {summary.unpriced > 0 ? (
                      <span className="run-trace__unreported">
                        {" "}
                        · {summary.unpriced} call{summary.unpriced === 1 ? "" : "s"} unpriced
                      </span>
                    ) : null}
                  </>
                ) : (
                  <span className="run-trace__unreported">unavailable</span>
                )}
              </dd>
            </div>
            {/* Reported only when positive. A run that wrote cache entries
                nothing read back paid more than it would have uncached, and
                calling that a small saving would be a false claim. */}
            {summary.cost && cacheSaving(summary.cost) !== null ? (
              <div>
                <dt>Saved by caching</dt>
                <dd>
                  {formatAmount(cacheSaving(summary.cost) ?? 0, summary.cost.currency)}
                  <span className="run-trace__unreported">
                    {" "}
                    · {formatAmount(summary.cost.baseline, summary.cost.currency)} uncached
                  </span>
                </dd>
              </div>
            ) : null}
            {summary.failed > 0 ? (
              <div>
                <dt>Failed calls</dt>
                <dd>{summary.failed}</dd>
              </div>
            ) : null}
            {summary.inFlight > 0 ? (
              <div>
                <dt>Unfinished calls</dt>
                <dd>{summary.inFlight}</dd>
              </div>
            ) : null}
            {summary.retried > 0 ? (
              <div>
                <dt>Retries</dt>
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
                  {entry.calls} call{entry.calls === 1 ? "" : "s"} ·{" "}
                  {entry.totalTokens.toLocaleString()} tokens
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
  if (!provenance) {
    return (
      <section className="run-trace__section">
        <h3 className="run-trace__heading">Origin</h3>
        <p className="run-trace__spend-note" role={error ? "alert" : undefined}>
          {error ?? "Where this run came from was not recorded."}
        </p>
      </section>
    )
  }
  const origin = describeOrigin(provenance)
  const agent = describeAgent(provenance)
  const spaceInstructions = describeSpaceInstructions(provenance)
  const said = provenance.source_message
  const verbatim = inputMatchesMessage(provenance)
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">Origin</h3>
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
          <span className="run-trace__origin-label">Asked for as</span>
          <pre className="run-trace__quote">{said.content}</pre>
          {said.truncated ? (
            <p className="run-trace__truncated">
              Quoted to the first part of the message; the conversation has the rest.
            </p>
          ) : null}
        </>
      ) : (
        <p className="run-trace__spend-note">
          {origin.quote === "none-expected"
            ? origin.isRepeat
              ? "Nothing was said for this run — it repeats an earlier one with the same input."
              : "No message asked for this run; a runtime dispatched it."
            : "No message is recorded for this run, so what was asked for cannot be compared."}
        </p>
      )}
      <span className="run-trace__origin-label">Sent to the worker</span>
      <pre className="run-trace__quote">{provenance.input}</pre>
      {said && !said.truncated ? (
        <p className="run-trace__truncated">
          {verbatim
            ? "The request was passed through unchanged."
            : "The instruction was rewritten from the message above."}
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
  if (!pins || pins.length === 0) return null
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">Plugins</h3>
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
        Open Space Plugins
      </button>
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
  if (!artifacts || artifacts.length === 0) return null
  return (
    <section className="run-trace__section">
      <h3 className="run-trace__heading">Artifacts published</h3>
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

    // The two records are fetched together and fail apart. A run whose trace
    // expired from storage still has a ledger, and a deployment that accounts
    // no managed calls still has a trace — neither absence may hide the other.
    const traceRequest = getTaskRunTrace(spaceId, taskRunId, token)
      .then((result) => {
        if (!cancelled) setTrace(result)
      })
      .catch((err) => {
        // The server explains a missing trace precisely — never recorded, or
        // gone from storage. Those mean different things to an operator, so
        // pass its message through instead of substituting a generic failure.
        if (!cancelled) setError(getErrorMessage(err, "Failed to load this run's trace"))
      })
    const callsRequest = listTaskRunLLMCalls(spaceId, taskRunId, token)
      .then((result) => {
        if (!cancelled) setCalls(result)
      })
      .catch((err) => {
        if (!cancelled) {
          setCallsError(getErrorMessage(err, "Failed to load this run's model calls"))
        }
      })

    const provenanceRequest = getTaskRunProvenance(spaceId, taskRunId, token)
      .then((result) => {
        if (!cancelled) setProvenance(result)
      })
      .catch((err) => {
        if (!cancelled) {
          setProvenanceError(getErrorMessage(err, "Failed to load where this run came from"))
        }
      })

    void Promise.all([traceRequest, callsRequest, provenanceRequest]).finally(() => {
      if (!cancelled) setLoading(false)
    })
    return () => {
      cancelled = true
    }
  }, [open, spaceId, token, taskRunId])

  return (
    <BaseModal
      open={open}
      title="Run details"
      titleId="run-trace-title"
      onClose={onClose}
      className="modal--large"
    >
      <div className="modal__body">
        <p className="modal__hint">{taskRunId ?? ""}</p>
        {loading ? (
          <p className="page-activity__empty">Loading…</p>
        ) : (
          <>
            {/* First and unconditional: a run that wrote no trace still came
                from somewhere, and that is the question a reader opens with. */}
            <OriginSection provenance={provenance} error={provenanceError} />
            <PluginsSection pins={provenance?.plugin_pins} spaceId={spaceId} onClose={onClose} />
            {error ? (
              <p className="modal__error" role="alert">{error}</p>
            ) : trace ? (
              <TraceBody trace={trace} />
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
          Close
        </Button>
      </div>
    </BaseModal>
  )
}
