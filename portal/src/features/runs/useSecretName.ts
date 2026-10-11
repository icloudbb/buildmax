import { useEffect, useState } from "react"
import { useSpace } from "../../contexts/SpaceContext"
import { listSecrets } from "../spaceSecrets/api"

/**
 * The name of the Secret a failure names, for a viewer who may read Secret
 * metadata. Only owners and admins may, so nothing is requested for anyone
 * else -- the explanation then names the Secret by its id rather than a
 * request failing in the background.
 */
export function useSecretName(spaceId: string | null, token: string | null, secretId: string | null | undefined): string | null {
  const { currentUserRole } = useSpace()
  const canRead = currentUserRole === "owner" || currentUserRole === "admin"
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
