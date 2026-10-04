package space

import (
	"testing"
)

func TestEffectiveRoleOf(t *testing.T) {
	members := []Member{
		{UserID: "u-owner", Role: RoleOwner},
		{UserID: "u-blank", Role: ""},
	}
	if got := EffectiveRoleOf(members, "u-owner"); got != RoleOwner {
		t.Errorf("owner: got %q, want %q", got, RoleOwner)
	}
	// A member with a blank stored role is still a member.
	if got := EffectiveRoleOf(members, "u-blank"); got != RoleMember {
		t.Errorf("blank role: got %q, want %q", got, RoleMember)
	}
	// Only a non-member yields "".
	if got := EffectiveRoleOf(members, "u-absent"); got != "" {
		t.Errorf("non-member: got %q, want empty", got)
	}
}

// TestAllowsMatrix is the whole rule, written out. A change to Allows that
// nobody meant to make has to edit this table to pass, which is the point: the
// two enforcers drive real requests and real commands, and neither of them can
// show the rule as one piece.
func TestAllowsMatrix(t *testing.T) {
	const (
		owner  = RoleOwner
		admin  = RoleAdmin
		member = RoleMember
	)
	want := map[Action]map[string]bool{
		ActionManageSpaceMembers:    {owner: true, admin: false, member: false},
		ActionInviteSpaceMember:     {owner: true, admin: true, member: false},
		ActionChangeMemberRole:      {owner: true, admin: false, member: false},
		ActionReadAuditTrail:        {owner: true, admin: false, member: false},
		ActionModerateIssueComments: {owner: true, admin: false, member: false},
		ActionManageAgents:          {owner: true, admin: true, member: false},
		ActionManageWorkflows:       {owner: true, admin: true, member: false},
		ActionAssignIssueWorkflow:   {owner: true, admin: true, member: false},
		ActionRunWorkflow:           {owner: true, admin: true, member: true},
		ActionCommentIssue:          {owner: true, admin: true, member: true},
		ActionManageSecrets:         {owner: true, admin: false, member: false},
		ActionReadSecrets:           {owner: true, admin: true, member: false},
		ActionManageServiceAccounts: {owner: true, admin: true, member: false},
		ActionManageSchedules:       {owner: true, admin: true, member: true},
	}
	for _, action := range Actions() {
		roles, ok := want[action]
		if !ok {
			t.Errorf("%s is an action with no row in this table", action)
			continue
		}
		for role, allowed := range roles {
			if got := Allows(role, action); got != allowed {
				t.Errorf("Allows(%q, %q) = %v, want %v", role, action, got, allowed)
			}
		}
	}
	if len(want) != len(Actions()) {
		t.Errorf("the table has %d rows for %d actions", len(want), len(Actions()))
	}
}

// TestEffectiveRoleReadsAnUnsetRoleAsMember pins the answer to a question the
// two enforcers used to answer differently: the HTTP guard read a membership
// row with no role as "not a member" and refused everything, while its own
// spaceRole helper read it as plain membership.
//
// Member is the reading, because the row is what says somebody belongs and
// member is the least the three roles can mean. Nothing can write such a row
// today -- the space service defaults an unset role before storing one -- so
// this is what a legacy row gets, not a path anything takes now.
func TestEffectiveRoleReadsAnUnsetRoleAsMember(t *testing.T) {
	if got := EffectiveRole(""); got != RoleMember {
		t.Errorf("EffectiveRole(\"\") = %q, want %q", got, RoleMember)
	}
	for _, role := range []string{RoleOwner, RoleAdmin, RoleMember} {
		if got := EffectiveRole(role); got != role {
			t.Errorf("EffectiveRole(%q) = %q, want it unchanged", role, got)
		}
	}
	// A role nobody implements is not rewritten into one that works: Allows
	// refuses it, and turning it into member here would be a silent grant.
	if got := EffectiveRole("root"); got != "root" {
		t.Errorf("EffectiveRole(%q) = %q, want it unchanged", "root", got)
	}
}

// TestEffectiveRoleGrantsOnlyMemberLevelActions is the consequence, stated so a
// widening of what member may do cannot quietly widen what an unset role may do
// without this failing too.
func TestEffectiveRoleGrantsOnlyMemberLevelActions(t *testing.T) {
	unset := EffectiveRole("")
	for _, action := range Actions() {
		want := Allows(RoleMember, action)
		if got := Allows(unset, action); got != want {
			t.Errorf("Allows(unset, %q) = %v, want %v (what a member gets)", action, got, want)
		}
	}
	if Allows(unset, ActionManageSpaceMembers) {
		t.Error("a row with no role may not change membership")
	}
	if Allows(unset, ActionManageAgents) {
		t.Error("a row with no role may not manage agents")
	}
}

// TestAllowsRefusesWhatItDoesNotKnow pins the direction an unknown value fails
// in. A role or an action nobody wrote a rule for is a rule nobody wrote, and
// answering true would turn a typo into an escalation.
func TestAllowsRefusesWhatItDoesNotKnow(t *testing.T) {
	for _, action := range Actions() {
		for _, role := range []string{"", "  ", "Owner", "OWNER", "root", "system_admin"} {
			if Allows(role, action) {
				t.Errorf("Allows(%q, %q) = true; only the three space roles are roles", role, action)
			}
		}
	}
	for _, action := range []Action{"", "manage", "manage_space_members ", "ManageSpaceMembers", "delete_space"} {
		for _, role := range []string{RoleOwner, RoleAdmin, RoleMember} {
			if Allows(role, action) {
				t.Errorf("Allows(%q, %q) = true; an unknown action is not permitted to anyone", role, action)
			}
		}
	}
}
