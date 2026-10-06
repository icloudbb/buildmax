/**
 * Save an authenticated response to a file.
 *
 * A plain link cannot do this. The Portal authenticates with a bearer token in
 * memory, not a cookie, so a browser-initiated navigation to an API URL arrives
 * anonymous and is refused. Fetching it through the shared client keeps the
 * token — and the 401-refresh-retry behind it — and hands the result to the
 * browser as a file.
 */

import { apiFetch, parseErrorResponse } from "./api/client"
import { authHeaders } from "./api/common"
import { detectLocale } from "@buildmax/gui"
import { translate } from "../i18n"

/** Filename the server asked for, or null when it did not ask. */
function filenameFromResponse(res: Response): string | null {
  const disposition = res.headers.get("Content-Disposition")
  if (!disposition) return null
  const quoted = /filename="([^"]+)"/.exec(disposition)
  if (quoted) return quoted[1]
  const bare = /filename=([^;]+)/.exec(disposition)
  return bare ? bare[1].trim() : null
}

/**
 * Fetch url with the session's credentials and save the body as a file.
 *
 * The server names the file — it knows what the export covers and when it was
 * taken — and `fallbackName` is used only if it did not.
 */
export async function downloadAuthenticated(
  url: string,
  token: string,
  fallbackName: string
): Promise<void> {
  const res = await apiFetch(url, { headers: authHeaders(token) })
  if (!res.ok) {
    throw new Error(await parseErrorResponse(res, translate(detectLocale(), "common.downloadFailed")))
  }
  const blob = await res.blob()
  const objectUrl = URL.createObjectURL(blob)
  try {
    const link = document.createElement("a")
    link.href = objectUrl
    link.download = filenameFromResponse(res) ?? fallbackName
    document.body.appendChild(link)
    link.click()
    link.remove()
  } finally {
    // Revoking immediately is safe: the click has already handed the blob to
    // the browser's download machinery, which holds its own reference.
    URL.revokeObjectURL(objectUrl)
  }
}
