/**
 * Whether linked chat accounts act right now. A link acts only while the
 * account's last sign-in is within the server's window, so the list response
 * carries one deadline for all of them.
 */
export type ChatLinkActivity = { state: "active"; until: Date } | { state: "lapsed" }

export function chatLinkActivity(activeUntil: string | undefined, now: number): ChatLinkActivity | null {
  if (!activeUntil) return null
  const until = new Date(activeUntil)
  if (Number.isNaN(until.getTime())) return null
  return until.getTime() > now ? { state: "active", until } : { state: "lapsed" }
}
