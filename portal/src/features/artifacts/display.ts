import type { Translate } from "@buildmax/gui"
import type { ApiArtifact } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

/** The name to lead with: the title if the producer gave one, else the file. */
export function artifactLabel(artifact: ApiArtifact): string {
  return artifact.title?.trim() || artifact.filename
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ["KB", "MB", "GB"]
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value < 10 ? value.toFixed(1) : Math.round(value)} ${units[unit]}`
}

/** What produced the file, in the words a reader uses rather than the stored enum. */
export function sourceLabel(artifact: ApiArtifact, t: Translate<MessageKey>): string {
  switch (artifact.source_type) {
    case "task_run":
      return t("artifacts.source.task_run")
    case "agent":
      return t("artifacts.source.agent")
    case "system":
      return t("artifacts.source.system")
    default:
      return t("artifacts.source.upload")
  }
}

/**
 * mayDelete mirrors the server: an admin or owner may remove anything the space
 * holds, and anyone else only what they uploaded themselves. It is duplicated
 * here to decide whether to offer the button, never to decide the outcome.
 */
export function mayDelete(
  artifact: ApiArtifact,
  userId?: string,
  role?: string | null
): boolean {
  if (role === "admin" || role === "owner") return true
  return artifact.created_by_type === "user" && !!userId && artifact.created_by_id === userId
}
