import type { Translate } from "@buildmax/gui"
import type { MessageKey } from "../../i18n"

const SPACE_ROLES: Record<string, MessageKey> = {
  owner: "admin.role.owner",
  admin: "admin.role.admin",
  member: "admin.role.member",
  viewer: "admin.role.viewer",
}

/** A Space role in the interface language; a role this build does not know is shown verbatim. */
export function roleLabel(role: string, t: Translate<MessageKey>): string {
  const key = SPACE_ROLES[role]
  return key ? t(key) : role
}
