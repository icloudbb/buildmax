import { useEffect, useState } from "react"
import { useSpace } from "../../contexts/SpaceContext"
import { listSecrets } from "../spaceSecrets/api"

/**
 * Whether the reader is a Space owner or admin: who can change an Agent, its
 * Secret grants, and the Space's plugin activations. A failure explanation
 * leads them to the fix and tells anyone else who can make it.
 */
export function useCanManageSpace(): boolean {
  const { currentUserRole } = useSpace()
  return currentUserRole === "owner" || currentUserRole === "admin"
}

/**
 * The name of the Secret a failure names, for a viewer who may read Secret
 * metadata. Only owners and admins may, so nothing is requested for anyone
 * else -- the explanation then describes the Secret rather than a request
 * failing in the background.
 */
export function useSecretName(spaceId: string | null, token: string | null, secretId: string | null | undefined): string | null {
  const canRead = useCanManageSpace()
  const [names, setNames] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!spaceId || !token || !secretId || !canRead) return
    let cancelled = false
    listSecrets(token, spaceId)
      .then((res) => {
        if (cancelled) return
        const out: Record<string, string> = {}
        for (const secret of res.secrets ?? []) out[secret.id] = secret.name
        setNames(out)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [spaceId, token, secretId, canRead])

  return secretId ? (names[secretId] ?? null) : null
}
