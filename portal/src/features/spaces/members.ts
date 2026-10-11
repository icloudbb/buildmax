import type { ApiInvitation } from "../../lib/api/types"

// Authority order of Space roles. A legacy row with no role is a member, as
// core/space.EffectiveRole reads it.
const ROLE_RANK: Record<string, number> = { member: 1, admin: 2, owner: 3 }

function rank(role: string): number {
  return ROLE_RANK[role] ?? ROLE_RANK.member
}

/** Whether moving a member from `from` to `to` grants more authority, which asks for confirmation first. */
export function raisesAuthority(from: string, to: string): boolean {
  return rank(to) > rank(from)
}

/** Who a pending invitation is for: name, then email, then -- if the account no longer resolves -- its ID. */
export function inviteeLabel(invitation: Pick<ApiInvitation, "user_name" | "user_email" | "user_id">): string {
  return invitation.user_name || invitation.user_email || invitation.user_id
}
