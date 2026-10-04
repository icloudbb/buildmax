// Package space owns what a space's roles may do.
//
// The decision is here, and only here, because it has two enforcers: the HTTP
// guard refuses a request before a handler runs, and the space service refuses a
// command whatever called it. Defence in depth is deliberate; two definitions
// of the same rule were not, and the one nobody remembered to update would have
// been the permissive one.
//
// The role values, the Space it belongs to, and the store contract are in
// space.go beside it.
package space

// An Action is something a caller wants to do to a space, named at the coarseness
// the role rules actually distinguish. It is not one action per route: several
// routes share a permission, and naming the permission rather than the route is
// what stops a new route from arriving without one.
type Action string

const (
	// ActionManageSpaceMembers covers adding and removing members. Granting
	// ownership is not part of it: the service refuses any role but member, so
	// an escalation cannot look like a routine invitation.
	ActionManageSpaceMembers Action = "manage_space_members"
	// ActionInviteSpaceMember covers creating a pending invitation and revoking
	// one before it is accepted. It is deliberately separate from
	// ActionManageSpaceMembers: admin holds this one too, but only at the
	// member role -- a restriction Allows cannot express since it only knows
	// the caller's own role, so the service enforces it. See
	// docs/design/space-membership-lifecycle.md §5.1.
	ActionInviteSpaceMember Action = "invite_space_member"
	// ActionChangeMemberRole covers promoting or demoting a member, including
	// the transfer that results from setting a target's role to owner. It is
	// its own action rather than folded into ActionManageSpaceMembers so that a
	// future change to who may invite or remove does not silently also change
	// who may reshuffle roles. See docs/design/space-membership-lifecycle.md
	// §5.2.
	ActionChangeMemberRole Action = "change_member_role"
	ActionManageAgents     Action = "manage_agents"
	ActionManageWorkflows  Action = "manage_workflows"
	// ActionAssignIssueWorkflow is assigning work to a workflow, which is a
	// change to what the space automates rather than a use of it.
	ActionAssignIssueWorkflow Action = "assign_issue_workflow"
	ActionRunWorkflow         Action = "run_workflow"
	ActionReadAuditTrail      Action = "read_audit_trail"
	ActionCommentIssue        Action = "comment_issue"
	// ActionModerateIssueComments covers deleting a comment the caller did not
	// write. Editing another author's comment is permitted to nobody, so it is
	// not an action here — see internal/service/issue.
	ActionModerateIssueComments Action = "moderate_issue_comments"
	// ActionManageSecrets covers creating, editing, disabling, and destroying
	// Space Secrets. Owner-only: value authority stays with the owner until
	// BuildMax has finer space grants. See docs/design/space-secrets.md §10.
	ActionManageSecrets Action = "manage_secrets"
	// ActionReadSecrets covers listing Secret metadata and item names -- never a
	// value, which no role can read. Owner or admin, because an admin editing an
	// Agent needs to see which Secrets exist to configure its consumption. See
	// docs/design/space-secrets.md §10.
	ActionReadSecrets Action = "read_secrets"
	// ActionManageSchedules covers creating, editing, enabling, disabling, and
	// deleting recurring schedules. Any member, not owner-only: it is the same
	// permission tier as running work, because a schedule is a member arranging
	// for a run they could already start by hand. See
	// docs/design/scheduled-agent-execution.md §9.
	ActionManageSchedules Action = "manage_schedules"
	// ActionManageServiceAccounts covers creating, renaming, disabling,
	// re-enabling, and re-sponsoring the Space's service accounts. Owner or
	// admin, the same authority as the shared automation they run. See
	// docs/design/space-assistants.md §6.3.
	ActionManageServiceAccounts Action = "manage_service_accounts"
	// ActionManageAssistants covers defining, publishing, pausing, binding, and
	// deleting the Space's Assistants. Owner or admin: publishing one is a
	// disclosure decision for the Space. See docs/design/space-assistants.md.
	ActionManageAssistants Action = "manage_assistants"
)

// Actions returns every action, so a test can prove the matrix covers each one
// rather than only the ones somebody remembered.
func Actions() []Action {
	return []Action{
		ActionManageSpaceMembers,
		ActionInviteSpaceMember,
		ActionChangeMemberRole,
		ActionManageAgents,
		ActionManageWorkflows,
		ActionAssignIssueWorkflow,
		ActionRunWorkflow,
		ActionReadAuditTrail,
		ActionCommentIssue,
		ActionModerateIssueComments,
		ActionManageSecrets,
		ActionReadSecrets,
		ActionManageSchedules,
		ActionManageServiceAccounts,
		ActionManageAssistants,
	}
}

// EffectiveRole is what a membership row's stored role means.
//
// A row with no role is a member. The row is what says somebody belongs to the
// space; the role only says how much they may do, and the least of the three is
// what a row that never got one has been given. Reading it as "not a member"
// instead would make a data defect look like an absent membership, and reading
// it as anything higher would let a missing value grant something.
//
// Callers normalize before asking Allows, which answers about a stated role.
func EffectiveRole(role string) string {
	if role == "" {
		return RoleMember
	}
	return role
}

// EffectiveRoleOf returns the effective role of userID within members, or ""
// when the roster has no membership for them. EffectiveRole never answers "",
// so an empty result unambiguously means "not a member" rather than "a member
// whose stored role is blank".
func EffectiveRoleOf(members []Member, userID string) string {
	for i := range members {
		if members[i].UserID == userID {
			return EffectiveRole(members[i].Role)
		}
	}
	return ""
}

// Allows reports whether a member holding role may perform action.
//
// It answers about a stated role. An unknown role and an unknown action are
// both refused: a caller with neither has not been given permission, and
// defaulting either to true would make a typo an escalation. An empty role is
// not stated, so it is refused here too — EffectiveRole is what turns a stored
// row into the role to ask about.
func Allows(role string, action Action) bool {
	switch action {
	case ActionManageSpaceMembers, ActionChangeMemberRole, ActionReadAuditTrail, ActionModerateIssueComments, ActionManageSecrets:
		return role == RoleOwner
	case ActionManageAgents, ActionManageWorkflows, ActionAssignIssueWorkflow, ActionInviteSpaceMember, ActionReadSecrets,
		ActionManageServiceAccounts, ActionManageAssistants:
		return role == RoleOwner || role == RoleAdmin
	case ActionRunWorkflow, ActionCommentIssue, ActionManageSchedules:
		return role == RoleOwner || role == RoleAdmin || role == RoleMember
	default:
		return false
	}
}
