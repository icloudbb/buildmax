import { useEffect, useState } from "react"
import type { ApiArtifact } from "../../lib/api/types"
import { getTaskRunProvenance } from "../runs/api"
import { navigate } from "../../router"
import { sourceLabel } from "./display"
import { useT } from "../../i18n"

interface ArtifactOriginProps {
  artifact: ApiArtifact
  spaceId: string | null
  token: string | null
}

/**
 * ArtifactOrigin turns "Produced by a run" into a link to the task that
 * produced it, resolving source_id through the same task-run record the
 * run's own trace view reads. See docs/design/unified-artifacts.md section
 * 5.2: source_id deliberately holds the task run, from which the task (and,
 * from there, the issue) are reachable.
 *
 * A miss -- source_id naming a foreground session rather than a task run, or
 * a run this reader can no longer see -- is an ordinary outcome, not an
 * error: the label stays plain text and nothing is reported.
 */
export function ArtifactOrigin({ artifact, spaceId, token }: ArtifactOriginProps) {
  const t = useT()
  const [taskId, setTaskId] = useState<string | null>(null)

  useEffect(() => {
    setTaskId(null)
    if (!spaceId || !token || !artifact.source_id) return
    if (artifact.source_type !== "task_run" && artifact.source_type !== "agent") return
    let cancelled = false
    getTaskRunProvenance(spaceId, artifact.source_id, token)
      .then((run) => {
        if (!cancelled) setTaskId(run.task_id)
      })
      .catch(() => {
        // Not every source_id names a task run; see the doc comment above.
      })
    return () => {
      cancelled = true
    }
  }, [artifact.source_id, artifact.source_type, spaceId, token])

  const label = sourceLabel(artifact, t)
  if (!taskId || !spaceId) return <>{label}</>

  return (
    <button
      type="button"
      className="artifact-details__link"
      onClick={() => navigate({ name: "task", spaceId, taskId })}
    >
      {label}
    </button>
  )
}
