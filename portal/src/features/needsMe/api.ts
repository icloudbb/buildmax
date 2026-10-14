import { getApiBase, requestJson } from "../../lib/api/client"
import { authHeaders } from "../../lib/api/common"
import type { ApiNeedsMeResponse } from "../../lib/api/types"

/**
 * What in the Space is waiting on the signed-in person, in four bounded
 * groups. The server owns the definition; see
 * docs/design/portal-navigation-and-space-context.md.
 */
export async function getNeedsMe(spaceId: string, token: string): Promise<ApiNeedsMeResponse> {
  return requestJson<ApiNeedsMeResponse>(`${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/needs-me`, {
    headers: authHeaders(token),
  })
}
