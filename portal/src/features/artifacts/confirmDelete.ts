import type { Translate } from "@buildmax/gui"
import type { ApiArtifact } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"
import { artifactLabel } from "./display"

/**
 * Ask before removing an artifact, in one place for every surface that offers
 * it.
 *
 * Deletion tombstones immediately at the authorization boundary, and content is
 * immutable — so re-uploading the same bytes produces a different reference,
 * and every link anyone saved to this one stays broken. That is worth stating
 * once, identically, rather than once per button.
 */
export function confirmArtifactDeletion(artifact: ApiArtifact, t: Translate<MessageKey>): boolean {
  return window.confirm(t("artifacts.confirmDelete", { label: artifactLabel(artifact) }))
}
