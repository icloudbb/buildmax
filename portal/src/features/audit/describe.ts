import type { ApiAuditEvent } from "../../lib/api/types"

/** How one event should read in the trail. */
export interface AuditEventDescription {
  /** A sentence naming what happened, without the actor. */
  summary: string
  /** Denials get a distinct treatment: they are what shows someone probing. */
  denied: boolean
  /** Present when the event names an object worth showing. */
  target: string | null
}

/**
 * describeEvent turns a stored action into something readable.
 *
 * Actions are permanent strings, so an unknown one is a real possibility: a
 * newer server writing an action this Portal predates. It is shown verbatim
 * rather than dropped or relabelled "unknown", because hiding an audit entry a
 * reader cannot interpret is worse than showing them a name they can search
 * for.
 */
export function describeEvent(event: ApiAuditEvent): AuditEventDescription {
  const target = event.target_id ? event.target_id : null
  switch (event.action) {
    case "user.login":
      return { summary: `Signed in from ${event.target_id || "an unknown platform"}`, denied: false, target: null }
    case "space.member_added":
      return {
        summary: event.detail ? `Added a member as ${event.detail}` : "Added a member",
        denied: false,
        target,
      }
    case "space.member_removed":
      return { summary: "Removed a member", denied: false, target }
    case "space.member_invited":
      return {
        summary: event.detail ? `Invited a member as ${event.detail}` : "Invited a member",
        denied: false,
        target,
      }
    case "space.invitation_accepted":
      return {
        summary: event.detail ? `Accepted an invitation as ${event.detail}` : "Accepted an invitation",
        denied: false,
        target,
      }
    case "space.invitation_revoked":
      return { summary: "Revoked a pending invitation", denied: false, target }
    case "space.invitation_expired":
      // Not a denial in the access.denied sense, but the same reasoning
      // applies: an attempt against an expired invitation is worth noticing
      // the same way a refusal is.
      return { summary: "An invitation was accepted after it expired", denied: true, target }
    case "space.member_role_changed":
      return {
        summary: event.detail ? `Changed a member's role to ${event.detail}` : "Changed a member's role",
        denied: false,
        target,
      }
    case "space.ownership_transferred":
      return { summary: "Transferred ownership", denied: false, target }
    case "space.member_login_code_issued":
      return { summary: "Issued a login code for a member", denied: false, target }
    case "llm_model.created":
      return {
        summary: event.detail ? `Added the model ${event.detail}` : "Added a model",
        denied: false,
        target,
      }
    case "llm_model.enabled":
      return { summary: "Enabled a model", denied: false, target }
    case "llm_model.disabled":
      return { summary: "Disabled a model", denied: false, target }
    case "llm_model.credential_replaced":
      return {
        summary: event.detail ? `Replaced the key for ${event.detail}` : "Replaced a model's key",
        denied: false,
        target,
      }
    case "user.logout":
      return { summary: "Signed out", denied: false, target: null }
    case "user.password_set":
      return { summary: "Set a password", denied: false, target: null }
    case "auth.refresh_reuse":
      return {
        summary: "A refresh token was presented twice; the session was revoked",
        denied: true,
        target,
      }
    case "user.created":
      return { summary: "Created an account", denied: false, target }
    case "user.login_code_issued":
      return { summary: "Issued a login code", denied: false, target }
    case "user.disabled":
      return {
        summary: event.detail ? `Disabled an account — ${event.detail}` : "Disabled an account",
        denied: false,
        target,
      }
    case "user.enabled":
      return { summary: "Enabled an account", denied: false, target }
    case "user.sessions_revoked":
      return { summary: "Revoked every session of an account", denied: false, target }
    case "system.admin_granted":
      return {
        summary: event.detail
          ? `Granted ${event.detail} over the deployment`
          : "Granted deployment authority",
        denied: false,
        target,
      }
    case "system.admin_revoked":
      return {
        summary: event.detail
          ? `Revoked ${event.detail} over the deployment`
          : "Revoked deployment authority",
        denied: false,
        target,
      }
    case "audit.exported":
      return {
        summary: event.detail ? `Exported the audit trail — ${event.detail}` : "Exported the audit trail",
        denied: false,
        target: null,
      }
    case "audit.pruned":
      // Not a denial, but it is the one action that removes evidence, so it
      // gets the same treatment: a reader scanning the trail must not skim
      // past the row that explains why the trail starts where it does.
      return {
        summary: event.detail
          ? `Retention removed ${event.detail}`
          : "Retention removed expired events",
        denied: true,
        target: null,
      }
    case "quota.threshold_reached":
      return {
        summary: event.detail ? `Approaching the quota: ${event.detail}` : "Approaching the quota",
        denied: false,
        target: null,
      }
    case "quota.exceeded":
      return {
        summary: event.detail ? `Quota reached: ${event.detail}` : "Quota reached; work was refused",
        denied: true,
        target: null,
      }
    case "space.agent_instructions_set":
      return {
        summary: event.detail
          ? `Updated Space agent instructions — ${event.detail}`
          : "Updated Space agent instructions",
        denied: false,
        target,
      }
    case "space.created":
      return {
        summary: event.detail ? `Created the space on the ${event.detail} tier` : "Created the space",
        denied: false,
        target,
      }
    case "webhook_key.created":
      return { summary: "Created a webhook key", denied: false, target }
    case "webhook_key.revoked":
      return { summary: "Revoked a webhook key", denied: false, target }
    case "channel_link.created":
      return {
        summary: event.detail ? `Linked a ${event.detail} chat account` : "Linked a chat account",
        denied: false,
        target,
      }
    case "channel_link.removed":
      return { summary: "Unlinked a chat account", denied: false, target }
    case "agent.created":
      return {
        summary: event.detail ? `Created the agent ${event.detail}` : "Created an agent",
        denied: false,
        target,
      }
    case "agent.updated":
      return {
        summary: event.detail ? `Updated the agent ${event.detail}` : "Updated an agent",
        denied: false,
        target,
      }
    case "agent.deleted":
      return { summary: "Deleted an agent", denied: false, target }
    case "workflow.created":
      return {
        summary: event.detail ? `Created the workflow ${event.detail}` : "Created a workflow",
        denied: false,
        target,
      }
    case "workflow.updated":
      return {
        summary: event.detail ? `Updated the workflow ${event.detail}` : "Updated a workflow",
        denied: false,
        target,
      }
    case "workflow.published":
      return {
        summary: event.detail ? `Published the workflow ${event.detail}` : "Published a workflow",
        denied: false,
        target,
      }
    case "workflow.archived":
      return {
        summary: event.detail ? `Archived the workflow ${event.detail}` : "Archived a workflow",
        denied: false,
        target,
      }
    case "workflow.unpublished":
      return {
        summary: event.detail ? `Returned the workflow ${event.detail} to draft` : "Returned a workflow to draft",
        denied: false,
        target,
      }
    case "access.denied":
      return {
        summary: event.target_id ? `Was refused: ${event.target_id}` : "Was refused a request",
        denied: true,
        target: null,
      }
    default:
      return { summary: event.action, denied: false, target }
  }
}

/** actorLabel names who acted, distinguishing a person from the deployment. */
export function actorLabel(event: ApiAuditEvent, currentUserId?: string): string {
  if (event.actor_type === "system") return `${event.actor_id} (system)`
  if (event.actor_type === "worker") return `${event.actor_id} (worker)`
  if (currentUserId && event.actor_id === currentUserId) return "You"
  return event.actor_id
}

export function formatEventTime(rfc3339: string): string {
  if (!rfc3339) return "—"
  return new Date(rfc3339).toLocaleString()
}
