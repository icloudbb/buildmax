package audit

import (
	"context"
	"time"
)

// Audit actions. These strings are persisted, so they are permanent: renaming
// one rewrites history for every reader that filters on it.
const (
	// UserLogin records a successful login. Failures are not recorded
	// here — a failed login says nothing about who the actor was, and
	// recording attempts keyed by a supplied email would turn the audit log
	// into a place to write arbitrary strings.
	UserLogin = "user.login"
	// UserLogout records a session being revoked on purpose. It is the
	// counterpart to UserLogin: together they bound when a session could
	// have been used.
	UserLogout = "user.logout"
	// RefreshReuse records a refresh token presented after it had already
	// been exchanged. It means the credential existed in two places, and the
	// session was revoked in response. Unlike the actions above this is not a
	// user's intent — it is the server reporting what it saw.
	RefreshReuse = "auth.refresh_reuse"
	// PasswordSet records an account's password being set or changed. The
	// event says that it happened, never what it became.
	PasswordSet = "user.password_set"
	// UserCreated records an account coming into existence, and
	// LoginCodeIssued records a way into one being minted. They are
	// separate because they are separate decisions: creating an account gives
	// nobody access until a code or a password follows.
	UserCreated     = "user.created"
	LoginCodeIssued = "user.login_code_issued"
	// UserDisabled and UserEnabled record an account's access being
	// stopped and restored. Disabling is not deletion: nothing is removed, and
	// enabling reverses the state and nothing else.
	UserDisabled = "user.disabled"
	UserEnabled  = "user.enabled"
	// UserExternalIdentityLinked records a BuildMax account being bound to a
	// verified identity at an external IdP — on a first SSO sign-in that matched
	// an existing account by email, or as part of just-in-time provisioning. The
	// detail names the issuer, never the subject or email: the trail says an
	// account gained a corporate login, not which directory entry it was. Written
	// in the same transaction as the link, because the record of who an account
	// answers to should not be able to lag or be lost behind the change itself.
	UserExternalIdentityLinked = "user.external_identity_linked"
	// UserExternalIdentityUnlinked records an administrator removing such a link,
	// which is permitted only while the account is disabled and is recorded
	// atomically with the deletion. Unlinking removes the binding, never the
	// account, its memberships, its work, or its history.
	UserExternalIdentityUnlinked = "user.external_identity_unlinked"
	// SessionsRevoked records every live session of one account being
	// retired at once. It is separate from user.logout, which is a person
	// ending their own.
	SessionsRevoked = "user.sessions_revoked"
	// SessionRevoked records one login chain being retired by an administrator,
	// naming the chain by its session id. The plural above is revoke-all; this
	// is signing out one device.
	SessionRevoked = "user.session_revoked"
	// SpaceMemberAdded and SpaceMemberRemoved record changes to who
	// can reach a space's resources.
	SpaceMemberAdded   = "space.member_added"
	SpaceMemberRemoved = "space.member_removed"
	// SpaceMemberInvited, InvitationAccepted, InvitationRevoked, and
	// InvitationExpired record a pending membership offer's whole life. Unlike
	// a failed login, an invitation names a specific, already-resolved account
	// before anyone acts on it, so every outcome is worth recording -- see
	// docs/design/space-membership-lifecycle.md §5.1 and §8.
	SpaceMemberInvited = "space.member_invited"
	InvitationAccepted = "space.invitation_accepted"
	InvitationRevoked  = "space.invitation_revoked"
	InvitationExpired  = "space.invitation_expired"
	// MemberRoleChanged and OwnershipTransferred record promotion, demotion,
	// and the transfer that results from setting a target's role to owner.
	// Transfer gets its own action distinct from a role change, even though it
	// is implemented as one call, because an investigation asking "did
	// ownership ever move" should not have to infer it from two
	// member_role_changed rows. See
	// docs/design/space-membership-lifecycle.md §5.2-§5.3, §8 M3.
	MemberRoleChanged    = "space.member_role_changed"
	OwnershipTransferred = "space.ownership_transferred"
	// SpaceOwnershipRecovered records a System Administrator promoting an enabled
	// member to owner when every recorded owner of a shared Space is disabled.
	// Distinct from an ordinary transfer because the actor is an operator, not a
	// space owner, and the recovery is only legal against a Space no owner can
	// sign in to. See docs/proposals/personnel-deactivation-lifecycle.md §9.
	SpaceOwnershipRecovered = "space.ownership_recovered"
	// SpaceMemberLoginCodeIssued is distinct from the deployment-scoped
	// user.login_code_issued so a reader of the space's own trail (owner-only)
	// sees it without needing system_admin visibility into the deployment-wide
	// trail. See docs/design/space-membership-lifecycle.md §5.4, §8 M4.
	SpaceMemberLoginCodeIssued = "space.member_login_code_issued"
	// ModelCreated, ModelEnabled, and ModelDisabled record
	// changes to which models a deployment will call. The catalog holds
	// provider credentials, so a change to it is a change to what the
	// deployment can spend and where prompts go.
	ModelCreated  = "llm_model.created"
	ModelEnabled  = "llm_model.enabled"
	ModelDisabled = "llm_model.disabled"
	// AccessDenied records a refused request. This is the one action
	// written on failure rather than success: a denial is what shows someone
	// probing at a boundary. SpaceID is empty when the refused route was
	// deployment-scoped rather than space-scoped.
	AccessDenied = "access.denied"
	// SystemAdminGranted and SystemAdminRevoked record deployment
	// authority changing hands. They are not space-scoped, so SpaceID is empty.
	//
	// These are the two actions in this list where a dropped write costs the
	// most: a grant that was made and not recorded is exactly what an
	// investigation needs. The write is still best-effort, for the reason in
	// internal/service/audit — see docs/design/system-administration.md
	// section 9.
	SystemAdminGranted = "system.admin_granted"
	SystemAdminRevoked = "system.admin_revoked"
	// ArtifactCreated and ArtifactDeleted record a durable file
	// entering and leaving a space's keeping. They are metadata-only by
	// construction: the target is the artifact ID, and neither the storage key, the
	// content, nor an uploader-supplied description belongs in the trail.
	ArtifactCreated = "artifact.created"
	ArtifactDeleted = "artifact.deleted"
	// ArtifactExpired is the tombstone nobody asked for: retention applied an
	// artifact's own ExpiresAt. It names the artifact, because it is what a
	// reader finds instead of the artifact.deleted they would otherwise expect.
	ArtifactExpired = "artifact.expired"
	// ArtifactsPurged records a sweep reclaiming the objects of already
	// tombstoned artifacts, with the count and the bytes. It is per sweep
	// rather than per artifact: the tombstone that authorized each one is
	// already in the trail, and this says only that the bytes are now gone.
	ArtifactsPurged = "artifact.purged"
	// ArtifactShareCreated and ArtifactShareRevoked record a public link to an
	// artifact being opened and withdrawn. Metadata-only like the artifact
	// events: the target is the artifact ID and the detail is the share ID —
	// the token never appears, because a trail that held it would hand a reader
	// the very credential the link is.
	ArtifactShareCreated = "artifact.share_created"
	ArtifactShareRevoked = "artifact.share_revoked"
	// The plugin actions record changes to what a deployment's members can
	// install. A release is instructions that cause tool use, processes that
	// start with someone's credentials, and hooks that run local programs, so
	// publishing one is a change to what every machine that installs it will
	// do. The detail names the version and a digest prefix — never package
	// contents or configuration values.
	PluginCreated    = "plugin.created"
	PluginUpdated    = "plugin.updated"
	PluginArchived   = "plugin.archived"
	PluginUnarchived = "plugin.unarchived"
	PluginPublished  = "plugin.published"
	PluginYanked     = "plugin.yanked"

	// A space activating a release is the record that answers "why did this run
	// have this capability". The pin moves and the suspension are separate
	// actions because each is a different decision about a space's runs.
	PluginActivated     = "plugin.activated"
	PluginPinMoved      = "plugin.pin_moved"
	PluginSuspended     = "plugin.suspended"
	PluginResumed       = "plugin.resumed"
	SpacePluginCuration = "space.plugin_curation_set"
	// SpaceSandboxDefaultsSet records a space's default sandbox tiers changing --
	// the tiers an agent that declares neither inherits. See
	// docs/design/agent-sandbox-policy.md §9 M3.
	SpaceSandboxDefaultsSet = "space.sandbox_defaults_set"
	// SpaceAgentInstructionsSet records a change to the shared prompt layer.
	// The event carries only the revision, never the user-authored text.
	SpaceAgentInstructionsSet = "space.agent_instructions_set"
	// EventsExported records the trail itself being read out in bulk.
	// Reading every recorded action is a sensitive action, and an export that
	// left no trace would be the one way to consult the record without
	// appearing in it.
	EventsExported = "audit.exported"
	// EventsPruned records events expiring under the deployment's
	// retention window, naming the cutoff and how many rows went.
	//
	// It is what separates a gap that policy created from evidence that was
	// lost: without it, a trail that starts on a Tuesday is indistinguishable
	// from a trail somebody truncated. Recording the deletion in the same
	// table it deletes from is deliberate — the event survives its own sweep
	// until the window moves past it, and by then a later one says the same.
	EventsPruned = "audit.pruned"
	// TracesPruned records run traces expiring under the deployment's trace
	// retention window, naming the range and how many went. It plays the same
	// role for the run-trace store that EventsPruned plays for the audit trail:
	// it explains a trace that is gone by policy rather than by loss. It lands
	// in the audit trail because that is where an operator already looks for
	// what a retention sweep removed.
	TracesPruned = "traces.pruned"
	// QuotaThresholdReached records a space crossing a share of its quota,
	// and QuotaExceeded records work being refused because the limit was
	// reached. The first is a warning nobody was blocked by; the second is the
	// block. They are separate actions because they call for different
	// responses — one is a heads-up, the other is work not happening.
	//
	// Both are written at most once per limit per period, so a space that keeps
	// submitting does not turn its own trail into a log of retries.
	QuotaThresholdReached = "quota.threshold_reached"
	QuotaExceeded         = "quota.exceeded"
	// SpaceCreated records a new space coming into existence, with its quota
	// tier in the detail. A space is an authorization boundary, so its creation
	// is a governed act; and because a space's tier is only ever assigned at
	// creation — there is no reassignment path — this is also the one place the
	// quota tier a space runs under is decided, and so the one place worth
	// recording it.
	SpaceCreated = "space.created"
	// WebhookKeyCreated and WebhookKeyRevoked record a webhook credential being
	// minted and withdrawn. A webhook key admits work under its owner's
	// identity, so its life is worth the trail; the key material never is, so the
	// target is the key id and nothing else. These are account-scoped, not
	// space-scoped, so SpaceID is empty.
	WebhookKeyCreated = "webhook_key.created"
	WebhookKeyRevoked = "webhook_key.revoked"
	// AgentCreated, AgentUpdated, and AgentDeleted record changes to a space's
	// agent definitions. A definition is instructions plus a tool and model
	// selection that later runs execute, so a change to one changes what the
	// space's automation will do. The target is the agent id; the prompt and
	// configuration are not in the trail. A revision restore is an update.
	AgentCreated = "agent.created"
	AgentUpdated = "agent.updated"
	AgentDeleted = "agent.deleted"
	// The workflow actions record a space's reusable plans changing and moving
	// through their lifecycle. Create and update cover content; published,
	// archived, and unpublished cover the state that decides whether shared work
	// may run against the plan. They are distinct actions rather than one
	// state_changed with the state in the detail so that "was this ever
	// published" is a filter over the trail, not a scan of it.
	WorkflowCreated     = "workflow.created"
	WorkflowUpdated     = "workflow.updated"
	WorkflowPublished   = "workflow.published"
	WorkflowArchived    = "workflow.archived"
	WorkflowUnpublished = "workflow.unpublished"
	// SecretCreated, SecretDisabled, and SecretDestroyed record a space Secret
	// entering and leaving service. A Secret is a credential an agent granted it
	// can read, so its lifecycle is as security-relevant as a webhook key's;
	// disabling refuses new run grants and destroying erases the material, so
	// both are governed acts worth the trail. The target is the secret id, and
	// the detail is empty: item names, values, ciphertext, and hashes never
	// belong in the trail. Disable and destroy are distinct actions rather than
	// one state_changed so that "was this secret ever destroyed" is a filter over
	// the trail, not a scan of it.
	SecretCreated   = "secret.created"
	SecretDisabled  = "secret.disabled"
	SecretDestroyed = "secret.destroyed"
)

// ActorOperator is the ActorID for an action taken by an operator command
// rather than by a signed-in user. The command runs on the machine that holds
// the database credentials and has no session to name, so the record names the
// binary. That is less than naming a person and more than recording nothing.
const ActorOperator = "buildmax-server"

// Audit actor kinds.
const (
	ActorUser   = "user"
	ActorWorker = "worker"
	ActorSystem = "system"
)

// Event is one recorded action.
//
// It deliberately carries no prompts, no generated content, no tool output, and
// no credentials — only who did what to which object. Run diagnostics live in
// the durable run trace and per-call accounting in the llm_call ledger; this is
// the record that a meaningful action occurred, which is a different question
// with a different retention answer.
type Event struct {
	ID string `json:"id"`
	// SpaceID is empty for actions that are not space-scoped, such as a login.
	SpaceID    string `json:"space_id,omitempty"`
	ActorType  string `json:"actor_type"`
	ActorID    string `json:"actor_id"`
	Action     string `json:"action"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	// TaskRunID names the run an action was taken on behalf of, when there is
	// one — a worker writing an artifact, the gateway refusing a call. It is the
	// run's public handle, stored opaquely like the actor and the target rather
	// than as a resolved foreign key, so an investigation that starts at an event
	// can reach the run's trace and llm_call ledger by that one id. It is empty
	// for the actions a person takes directly, which are most of them.
	TaskRunID string `json:"task_run_id,omitempty"`
	// Detail is a short, non-sensitive note — a role name, a model alias. It
	// is not a place for request bodies.
	Detail    string    `json:"detail,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Filter narrows an audit search. Every field is optional; the zero value
// matches everything.
//
// There is no free-text field and there will not be one. The trail holds who
// did what to which object, and a search across it is a search over those —
// adding a text query would invite a `Detail LIKE` scan over a column whose
// whole purpose is to stay small and structured.
type Filter struct {
	// SpaceID matches events scoped to one space. It does not match the events
	// that have no space, such as a login or a grant — see WithoutSpace.
	SpaceID string
	// WithoutSpace matches only the deployment-scoped events. It exists because
	// an empty SpaceID already means "any space", so there would otherwise be no
	// way to ask for the ones a space-scoped reader can never see.
	WithoutSpace bool
	ActorID      string
	Action       string
	// TaskRunID matches the events recorded on behalf of one run. It is how an
	// investigation pivots the other way — from a run to every governed action
	// it caused — the counterpart to the id an event carries.
	TaskRunID string
	// Since and Until bound created_at, inclusive and exclusive respectively.
	// Zero means unbounded.
	Since time.Time
	Until time.Time
}

// Cursor is a position in the trail, used to walk it in bounded pages.
//
// It exists because offset paging is wrong for an export. An export reads the
// table over many round trips while rows are still being appended at one end
// and, under a retention window, removed at the other — and either shifts every
// offset behind it, so a page boundary silently skips records. A keyset cursor
// names where the last page stopped, so the next one continues from that record
// no matter what the table did meanwhile.
//
// The zero value starts at the newest event. `CreatedAt` alone is not enough to
// resume from: microsecond resolution narrows collisions but does not remove
// them, so the event's own identity breaks the tie.
//
// That identity is the public one. The row key that actually orders a tie lives
// below the store boundary, so a store resolves this handle to it rather than
// handing a database key to a caller.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// Zero reports whether the cursor is a fresh start rather than a resumption.
func (c Cursor) Zero() bool { return c.CreatedAt.IsZero() && c.ID == "" }

// Writer appends audit events.
//
// Most callers only ever write: an operator command, a handler recording a
// grant, the quota service noticing a limit. They take this rather than the
// full store so that recording an action never carries the ability to read the
// trail back, which is a wider permission than any of them needs.
type Writer interface {
	// RecordAuditEvent appends one event. Events are append-only; nothing here
	// updates or deletes one, because a record that can be edited is not
	// evidence.
	RecordAuditEvent(ctx context.Context, in Event) error
}

// Store persists audit events.
//
// Record takes no error-returning contract the caller must handle at the call
// site by design: see the recorder in internal/service/audit for why a failed
// write is logged rather than propagated, and what that costs.
//
// Nothing here updates or deletes an event. Retention expiry is the one thing
// that removes rows, and it lives in PruneStore rather than in this
// interface: it is a policy applied uniformly by age, not an edit anyone can
// make to a particular record, and every reader of this interface should stay
// unable to reach it.
type Store interface {
	Writer
	// ListAuditEvents returns a space's events, newest first.
	//
	// It stays alongside SearchAuditEvents rather than being replaced by it:
	// a space owner asks a narrower question, and giving that reader the wider
	// method is how a space-scoped route acquires a deployment-scoped answer.
	ListAuditEvents(ctx context.Context, spaceID string, limit, offset int) ([]Event, int, error)
	// SearchAuditEvents returns events across every space, newest first. It is
	// the deployment-scoped read, and only /api/admin routes may reach it.
	SearchAuditEvents(ctx context.Context, filter Filter, limit, offset int) ([]Event, int, error)
	// ExportSpaceAuditEvents returns one page of a space's events, newest first,
	// continuing from after. It answers the same question as ListAuditEvents
	// and differs only in how it pages, because an export walks the whole
	// trail rather than showing the first screen of it.
	ExportSpaceAuditEvents(ctx context.Context, spaceID string, after Cursor, limit int) ([]Event, error)
	// ExportAuditEvents is the deployment-scoped counterpart, and the same rule
	// applies as for SearchAuditEvents: only /api/admin routes may reach it.
	// The pair stays split for the reason ListAuditEvents gives — a space-scoped
	// route that can name its own filter is a route that can widen it.
	ExportAuditEvents(ctx context.Context, filter Filter, after Cursor, limit int) ([]Event, error)
}

// PruneStore expires audit events under a retention window.
//
// It is deliberately not part of Store. Every reader and writer of the
// trail holds that interface, and none of them should be able to remove a
// record; the one caller that legitimately can is the retention sweep, which
// takes this narrower one.
type PruneStore interface {
	// PruneAuditEvents deletes events recorded before the cutoff, at most
	// limit of them, and returns how many went. A bounded delete keeps one
	// sweep from holding the table while a backlog of an old deployment's
	// events is removed; the caller repeats until it returns fewer than limit.
	PruneAuditEvents(ctx context.Context, before time.Time, limit int) (int64, error)
	// OldestAuditEventAt reports the timestamp of the oldest event, or zero
	// when the table is empty. The sweep uses it to say what a prune actually
	// removed rather than only what it was allowed to.
	OldestAuditEventAt(ctx context.Context) (time.Time, error)
}
