import type { Translate } from "@buildmax/gui"
import type { ApiAuditEvent } from "../../lib/api/types"
import type { MessageKey } from "../../i18n"

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
export function describeEvent(event: ApiAuditEvent, t: Translate<MessageKey>): AuditEventDescription {
  const target = event.target_id ? event.target_id : null
  // The sentence for this action, naming the detail when the event has one.
  const say = (key: MessageKey, detailKey?: MessageKey) =>
    detailKey && event.detail ? t(detailKey, { detail: event.detail }) : t(key)
  switch (event.action) {
    case "user.login":
      return {
        summary: event.target_id
          ? t("audit.event.user.login", { platform: event.target_id })
          : t("audit.event.user.loginUnknown"),
        denied: false,
        target: null,
      }
    case "space.member_added":
      return {
        summary: say("audit.event.space.member_added", "audit.event.space.member_added.detail"),
        denied: false,
        target,
      }
    case "space.member_removed":
      return { summary: say("audit.event.space.member_removed"), denied: false, target }
    case "space.member_invited":
      return {
        summary: say("audit.event.space.member_invited", "audit.event.space.member_invited.detail"),
        denied: false,
        target,
      }
    case "space.invitation_accepted":
      return {
        summary: say("audit.event.space.invitation_accepted", "audit.event.space.invitation_accepted.detail"),
        denied: false,
        target,
      }
    case "space.invitation_revoked":
      return { summary: say("audit.event.space.invitation_revoked"), denied: false, target }
    case "space.invitation_expired":
      // Not a denial in the access.denied sense, but the same reasoning
      // applies: an attempt against an expired invitation is worth noticing
      // the same way a refusal is.
      return { summary: say("audit.event.space.invitation_expired"), denied: true, target }
    case "space.member_role_changed":
      return {
        summary: say("audit.event.space.member_role_changed", "audit.event.space.member_role_changed.detail"),
        denied: false,
        target,
      }
    case "space.ownership_transferred":
      return { summary: say("audit.event.space.ownership_transferred"), denied: false, target }
    case "space.member_login_code_issued":
      return { summary: say("audit.event.space.member_login_code_issued"), denied: false, target }
    case "llm_model.created":
      return {
        summary: say("audit.event.llm_model.created", "audit.event.llm_model.created.detail"),
        denied: false,
        target,
      }
    case "llm_model.enabled":
      return { summary: say("audit.event.llm_model.enabled"), denied: false, target }
    case "llm_model.disabled":
      return { summary: say("audit.event.llm_model.disabled"), denied: false, target }
    case "llm_model.credential_replaced":
      return {
        summary: say("audit.event.llm_model.credential_replaced", "audit.event.llm_model.credential_replaced.detail"),
        denied: false,
        target,
      }
    case "user.logout":
      return { summary: say("audit.event.user.logout"), denied: false, target: null }
    case "user.password_set":
      return { summary: say("audit.event.user.password_set"), denied: false, target: null }
    case "auth.refresh_reuse":
      return {
        summary: say("audit.event.auth.refresh_reuse"),
        denied: true,
        target,
      }
    case "user.created":
      return { summary: say("audit.event.user.created"), denied: false, target }
    case "user.login_code_issued":
      return { summary: say("audit.event.user.login_code_issued"), denied: false, target }
    case "user.disabled":
      return {
        summary: say("audit.event.user.disabled", "audit.event.user.disabled.detail"),
        denied: false,
        target,
      }
    case "user.enabled":
      return { summary: say("audit.event.user.enabled"), denied: false, target }
    case "user.sessions_revoked":
      return { summary: say("audit.event.user.sessions_revoked"), denied: false, target }
    case "system.admin_granted":
      return {
        summary: say("audit.event.system.admin_granted", "audit.event.system.admin_granted.detail"),
        denied: false,
        target,
      }
    case "system.admin_revoked":
      return {
        summary: say("audit.event.system.admin_revoked", "audit.event.system.admin_revoked.detail"),
        denied: false,
        target,
      }
    case "audit.exported":
      return {
        summary: say("audit.event.audit.exported", "audit.event.audit.exported.detail"),
        denied: false,
        target: null,
      }
    case "audit.pruned":
      // Not a denial, but it is the one action that removes evidence, so it
      // gets the same treatment: a reader scanning the trail must not skim
      // past the row that explains why the trail starts where it does.
      return {
        summary: say("audit.event.audit.pruned", "audit.event.audit.pruned.detail"),
        denied: true,
        target: null,
      }
    case "quota.threshold_reached":
      return {
        summary: say("audit.event.quota.threshold_reached", "audit.event.quota.threshold_reached.detail"),
        denied: false,
        target: null,
      }
    case "quota.exceeded":
      return {
        summary: say("audit.event.quota.exceeded", "audit.event.quota.exceeded.detail"),
        denied: true,
        target: null,
      }
    case "space.agent_instructions_set":
      return {
        summary: say("audit.event.space.agent_instructions_set", "audit.event.space.agent_instructions_set.detail"),
        denied: false,
        target,
      }
    case "space.created":
      return {
        summary: say("audit.event.space.created", "audit.event.space.created.detail"),
        denied: false,
        target,
      }
    case "space.quota_tier_changed":
      return {
        summary: say("audit.event.space.quota_tier_changed", "audit.event.space.quota_tier_changed.detail"),
        denied: false,
        target,
      }
    case "webhook_key.created":
      return { summary: say("audit.event.webhook_key.created"), denied: false, target }
    case "webhook_key.revoked":
      return { summary: say("audit.event.webhook_key.revoked"), denied: false, target }
    case "channel_link.created":
      return {
        summary: say("audit.event.channel_link.created", "audit.event.channel_link.created.detail"),
        denied: false,
        target,
      }
    case "channel_link.removed":
      return { summary: say("audit.event.channel_link.removed"), denied: false, target }
    case "agent.created":
      return {
        summary: say("audit.event.agent.created", "audit.event.agent.created.detail"),
        denied: false,
        target,
      }
    case "agent.updated":
      return {
        summary: say("audit.event.agent.updated", "audit.event.agent.updated.detail"),
        denied: false,
        target,
      }
    case "agent.deleted":
      return { summary: say("audit.event.agent.deleted"), denied: false, target }
    case "workflow.created":
      return {
        summary: say("audit.event.workflow.created", "audit.event.workflow.created.detail"),
        denied: false,
        target,
      }
    case "workflow.updated":
      return {
        summary: say("audit.event.workflow.updated", "audit.event.workflow.updated.detail"),
        denied: false,
        target,
      }
    case "workflow.published":
      return {
        summary: say("audit.event.workflow.published", "audit.event.workflow.published.detail"),
        denied: false,
        target,
      }
    case "workflow.archived":
      return {
        summary: say("audit.event.workflow.archived", "audit.event.workflow.archived.detail"),
        denied: false,
        target,
      }
    case "workflow.unpublished":
      return {
        summary: say("audit.event.workflow.unpublished", "audit.event.workflow.unpublished.detail"),
        denied: false,
        target,
      }
    case "service_account.created":
      return {
        summary: say("audit.event.service_account.created", "audit.event.service_account.created.detail"),
        denied: false,
        target,
      }
    case "service_account.renamed":
      return {
        summary: say("audit.event.service_account.renamed", "audit.event.service_account.renamed.detail"),
        denied: false,
        target,
      }
    case "service_account.disabled":
      return { summary: say("audit.event.service_account.disabled"), denied: false, target }
    case "service_account.enabled":
      return { summary: say("audit.event.service_account.enabled"), denied: false, target }
    case "service_account.sponsor_changed":
      return { summary: say("audit.event.service_account.sponsor_changed"), denied: false, target }
    case "assistant.created":
      return {
        summary: say("audit.event.assistant.created", "audit.event.assistant.created.detail"),
        denied: false,
        target,
      }
    case "assistant.updated":
      return {
        summary: say("audit.event.assistant.updated", "audit.event.assistant.updated.detail"),
        denied: false,
        target,
      }
    case "assistant.deleted":
      return {
        summary: say("audit.event.assistant.deleted", "audit.event.assistant.deleted.detail"),
        denied: false,
        target,
      }
    case "assistant.activated":
      return {
        summary: say("audit.event.assistant.activated", "audit.event.assistant.activated.detail"),
        denied: false,
        target,
      }
    case "assistant.paused":
      return {
        summary: say("audit.event.assistant.paused", "audit.event.assistant.paused.detail"),
        denied: false,
        target,
      }
    case "assistant.sponsor_changed":
      return { summary: say("audit.event.assistant.sponsor_changed"), denied: false, target }
    case "assistant.bound":
      return {
        summary: say("audit.event.assistant.bound", "audit.event.assistant.bound.detail"),
        denied: false,
        target,
      }
    case "assistant.unbound":
      return {
        summary: say("audit.event.assistant.unbound", "audit.event.assistant.unbound.detail"),
        denied: false,
        target,
      }
    case "assistant.requester_replied":
      // The target is the Issue; the detail is the Assistant that carried it.
      return {
        summary: say("audit.event.assistant.requester_replied", "audit.event.assistant.requester_replied.detail"),
        denied: false,
        target,
      }
    case "access.denied":
      return {
        summary: event.target_id
          ? t("audit.event.access.denied.target", { target: event.target_id })
          : t("audit.event.access.denied"),
        denied: true,
        target: null,
      }
    default:
      return { summary: event.action, denied: false, target }
  }
}

/** actorLabel names who acted, distinguishing a person from the deployment. */
export function actorLabel(event: ApiAuditEvent, t: Translate<MessageKey>, currentUserId?: string): string {
  if (event.actor_type === "system") return t("audit.actor.system", { actor: event.actor_id })
  if (event.actor_type === "worker") return t("audit.actor.worker", { actor: event.actor_id })
  if (currentUserId && event.actor_id === currentUserId) return t("audit.actor.you")
  return event.actor_id
}

export function formatEventTime(rfc3339: string): string {
  if (!rfc3339) return "—"
  return new Date(rfc3339).toLocaleString()
}
