import {
  apiFetch,
  getApiBase,
  parseErrorResponse,
  requestJson,
  throwIfNotOk,
} from "../../lib/api/client"
import { authHeaders } from "../../lib/api/common"
import type {
  ApiInvitation,
  ApiMemberLoginCode,
  ApiMemberRole,
  ApiServiceAccount,
  ApiSpace,
  ApiSpaceMember,
  ApiUsage,
} from "../../lib/api/types"

/**
 * The members a person can be picked from: everyone but the Space's service
 * accounts, which are listed in the roster but are never a person to assign.
 */
export function peopleOnly(members: ApiSpaceMember[]): ApiSpaceMember[] {
  return members.filter((member) => member.user_kind !== "service")
}

function serviceAccountsBase(spaceId: string): string {
  return `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/service-accounts`
}

export async function getServiceAccounts(spaceId: string, token: string): Promise<ApiServiceAccount[]> {
  return requestJson<ApiServiceAccount[]>(serviceAccountsBase(spaceId), { headers: authHeaders(token) })
}

export async function createServiceAccount(
  spaceId: string,
  body: { name: string; sponsor_user_id?: string },
  token: string
): Promise<ApiServiceAccount> {
  return requestJson<ApiServiceAccount>(serviceAccountsBase(spaceId), {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

/** Rename or re-sponsor. Taking sponsorship clears needs_sponsor. */
export async function updateServiceAccount(
  spaceId: string,
  userId: string,
  body: { name?: string; sponsor_user_id?: string },
  token: string
): Promise<ApiServiceAccount> {
  return requestJson<ApiServiceAccount>(`${serviceAccountsBase(spaceId)}/${encodeURIComponent(userId)}`, {
    method: "PATCH",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

export async function setServiceAccountState(
  spaceId: string,
  userId: string,
  disabled: boolean,
  token: string
): Promise<ApiServiceAccount> {
  return requestJson<ApiServiceAccount>(`${serviceAccountsBase(spaceId)}/${encodeURIComponent(userId)}/state`, {
    method: "PUT",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify({ disabled }),
  })
}

export async function getSpaces(token: string): Promise<ApiSpace[]> {
  return requestJson<ApiSpace[]>(`${getApiBase()}/api/spaces`, {
    headers: authHeaders(token),
  })
}

export async function createSpace(
  body: { name: string },
  token: string
): Promise<ApiSpace> {
  return requestJson<ApiSpace>(`${getApiBase()}/api/spaces`, {
    method: "POST",
    headers: { ...authHeaders(token), "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

export async function getSpaceMembers(spaceId: string, token: string): Promise<ApiSpaceMember[]> {
  return requestJson<ApiSpaceMember[]>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/members`,
    {
      headers: authHeaders(token),
    }
  )
}

export async function getSpaceUsage(spaceId: string, token: string): Promise<ApiUsage> {
  return requestJson<ApiUsage>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/usage`,
    {
      headers: authHeaders(token),
    }
  )
}

export async function removeSpaceMember(
  spaceId: string,
  userId: string,
  token: string
): Promise<void> {
  const res = await apiFetch(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/members/${encodeURIComponent(userId)}`,
    {
      method: "DELETE",
      headers: authHeaders(token),
    }
  )
  if (!res.ok) {
    throw new Error(await parseErrorResponse(res, "Failed to remove member"))
  }
  await throwIfNotOk(res)
}

/**
 * Change a member's role. Setting role to "owner" is ownership transfer: the
 * caller is demoted to admin in the same call, unilaterally and immediately.
 * See docs/design/space-membership-lifecycle.md.
 */
export async function setMemberRole(
  spaceId: string,
  userId: string,
  body: { role: string },
  token: string
): Promise<ApiMemberRole> {
  return requestJson<ApiMemberRole>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/members/${encodeURIComponent(userId)}`,
    {
      method: "PATCH",
      headers: { ...authHeaders(token), "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }
  )
}

/**
 * Issue a login code for a locked-out member of the caller's own space. The
 * code is shown once and recorded nowhere -- see
 * docs/design/space-membership-lifecycle.md §5.4.
 */
export async function issueMemberLoginCode(
  spaceId: string,
  userId: string,
  token: string
): Promise<ApiMemberLoginCode> {
  return requestJson<ApiMemberLoginCode>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/members/${encodeURIComponent(userId)}/login-code`,
    {
      method: "POST",
      headers: authHeaders(token),
    }
  )
}

/**
 * Invite an existing account to join the space. Bounded to an account that
 * already exists -- creating one is a system_admin operation, not a
 * space-scoped one. See docs/design/space-membership-lifecycle.md.
 */
export async function inviteMember(
  spaceId: string,
  body: { email: string; role?: string },
  token: string
): Promise<ApiInvitation> {
  return requestJson<ApiInvitation>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/invitations`,
    {
      method: "POST",
      headers: { ...authHeaders(token), "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }
  )
}

export async function getSpaceInvitations(spaceId: string, token: string): Promise<ApiInvitation[]> {
  return requestJson<ApiInvitation[]>(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/invitations`,
    {
      headers: authHeaders(token),
    }
  )
}

export async function revokeInvitation(
  spaceId: string,
  invitationId: string,
  token: string
): Promise<void> {
  const res = await apiFetch(
    `${getApiBase()}/api/spaces/${encodeURIComponent(spaceId)}/invitations/${encodeURIComponent(invitationId)}`,
    {
      method: "DELETE",
      headers: authHeaders(token),
    }
  )
  if (!res.ok) {
    throw new Error(await parseErrorResponse(res, "Failed to revoke the invitation"))
  }
  await throwIfNotOk(res)
}

/** What is pending for the signed-in caller, across every space. Not space-scoped. */
export async function getMyInvitations(token: string): Promise<ApiInvitation[]> {
  return requestJson<ApiInvitation[]>(`${getApiBase()}/api/invitations`, {
    headers: authHeaders(token),
  })
}

/**
 * Accept a pending invitation. Takes no code: the caller already reached
 * this session on their own, so this is authorized by the invitation being
 * their own pending row.
 */
export async function acceptInvitation(
  invitationId: string,
  token: string
): Promise<ApiInvitation> {
  return requestJson<ApiInvitation>(
    `${getApiBase()}/api/invitations/${encodeURIComponent(invitationId)}/accept`,
    {
      method: "POST",
      headers: authHeaders(token),
    }
  )
}
