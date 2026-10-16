import { useAuth } from "../../contexts/AuthContext"

/** The system role that opens deployment administration. */
const SYSTEM_ROLE_ADMIN = "system_admin"

/**
 * useAdminAccess says whether this person may operate the deployment, from the
 * system roles their session carries.
 *
 * Asking GET /api/admin/me instead would answer almost everyone with a 403, an
 * expected refusal logged as an error on every page load. The session answers
 * every signed-in account. This is presentation, not enforcement: the server
 * refuses an ungranted account regardless of what this returns.
 */
export function useAdminAccess(): boolean {
  const { systemRoles } = useAuth()
  return systemRoles.includes(SYSTEM_ROLE_ADMIN)
}
