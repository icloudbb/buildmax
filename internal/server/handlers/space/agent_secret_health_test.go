package space

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// secretHealthFixture is an Agent consuming one healthy grant and one of each
// problem, in a Space with an owner and a member.
func secretHealthFixture(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	spaceID := "tm_1"
	spaceStore := &mock.MockSpaceStore{
		Spaces: []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u_owner"}},
		Members: []corespace.Member{
			{SpaceID: spaceID, UserID: "u_owner", Role: corespace.RoleOwner},
			{SpaceID: spaceID, UserID: "u_member", Role: corespace.RoleMember},
		},
	}
	secrets := newMemSecretStore()
	secrets.rows["sec_ok"] = &coresecret.Secret{ID: "sec_ok", SpaceID: spaceID, Name: "Healthy key", State: coresecret.StateActive, ItemNames: []string{"TOKEN"}}
	secrets.rows["sec_off"] = &coresecret.Secret{ID: "sec_off", SpaceID: spaceID, Name: "Prod deploy key", Description: "rotated quarterly", State: coresecret.StateDisabled, ItemNames: []string{"TOKEN"}}
	secrets.rows["sec_gone"] = &coresecret.Secret{ID: "sec_gone", SpaceID: spaceID, Name: "Retired key", State: coresecret.StateDestroyed}
	secrets.rows["sec_thin"] = &coresecret.Secret{ID: "sec_thin", SpaceID: spaceID, Name: "Thin key", State: coresecret.StateActive, ItemNames: []string{"OTHER", "UNRELATED_ITEM"}}
	secrets.rows["sec_elsewhere"] = &coresecret.Secret{ID: "sec_elsewhere", SpaceID: "tm_other", Name: "Another Space's key", State: coresecret.StateActive, ItemNames: []string{"TOKEN"}}

	agents := &mock.MockAgentStore{}
	created, err := agents.CreateAgentInSpace(t.Context(), agentdef.CreateInput{SpaceID: spaceID, UserID: "u_owner",
		Def: agentdef.Definition{Name: "Deployer"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	// Set on the stored row: the consumption is what is being read, and the
	// write path's validation would refuse the broken grants this test needs.
	agents.Agents[0].SecretConsumption = agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_ok", Item: "TOKEN", EnvName: "OK_TOKEN"},
		{Secret: "sec_off", Item: "TOKEN", EnvName: "DEPLOY_TOKEN"},
		{Secret: "sec_gone", Prefix: "OLD_", Optional: true},
		{Secret: "sec_thin", Item: "TOKEN", EnvName: "THIN_TOKEN"},
		{Secret: "sec_elsewhere", Item: "TOKEN", EnvName: "ELSEWHERE_TOKEN"},
	}}
	h := New(Config{JWTSecret: agentTestSecret, Spaces: spaceStore, Agents: agents, Secrets: secrets})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, "/api/spaces/" + spaceID + "/agents/" + created.ID
}

// Anyone who can run the Agent is told which grants a run would not be given
// and why, by the Secret ID and item the consumption already shows. Nothing a
// member may not read about the Secret -- its name, description, state, or
// item list -- is in the answer.
func TestAgentReadReportsSecretGrantProblemsToOwnersAndMembers(t *testing.T) {
	mux, path := secretHealthFixture(t)
	want := []secretGrantProblemResponse{
		{SecretID: "sec_off", Item: "TOKEN", Problem: "disabled"},
		{SecretID: "sec_gone", Problem: "unavailable", Optional: true},
		{SecretID: "sec_thin", Item: "TOKEN", Problem: "item_missing"},
		// Another Space's Secret reads exactly as a missing one.
		{SecretID: "sec_elsewhere", Item: "TOKEN", Problem: "unavailable"},
	}
	for _, user := range []string{"u_owner", "u_member"} {
		token := "Bearer " + testsupport.SignJWT(user, agentTestSecret)
		for _, route := range []string{path, path[:strings.LastIndex(path, "/")]} {
			rec := doJSON(t, mux, http.MethodGet, route, token, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s GET %s = %d: %s", user, route, rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			var got AgentResponse
			if strings.HasPrefix(body, "[") {
				var list []AgentResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
					t.Fatalf("decode list: %v %s", err, body)
				}
				got = list[0]
			} else if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !slices.Equal(got.SecretGrantProblems, want) {
				t.Errorf("%s GET %s problems = %+v, want %+v", user, route, got.SecretGrantProblems, want)
			}
			for _, withheld := range []string{"Healthy key", "Prod deploy key", "rotated quarterly", "Retired key", "Thin key", "Another Space's key", "UNRELATED_ITEM", "destroyed", `"state"`} {
				if strings.Contains(body, withheld) {
					t.Errorf("%s GET %s leaks %q: %s", user, route, withheld, body)
				}
			}
		}
	}
}

// Without a secret store no Agent can consume a Secret, and the read reports
// nothing rather than failing.
func TestAgentReadWithoutSecretStoreReportsNoProblems(t *testing.T) {
	spaceID := "tm_1"
	agents := &mock.MockAgentStore{}
	created, err := agents.CreateAgentInSpace(t.Context(), agentdef.CreateInput{SpaceID: spaceID, UserID: "u_owner",
		Def: agentdef.Definition{Name: "Plain"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	h := New(Config{JWTSecret: agentTestSecret, Agents: agents, Spaces: &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Space", CreatedBy: "u_owner"}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: "u_owner", Role: corespace.RoleOwner}},
	}})
	mux := http.NewServeMux()
	h.Register(mux)
	rec := doJSON(t, mux, http.MethodGet, "/api/spaces/"+spaceID+"/agents/"+created.ID, "Bearer "+testsupport.SignJWT("u_owner", agentTestSecret), "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "secret_grant_problems") {
		t.Errorf("GET = %d %s, want 200 with no problems", rec.Code, rec.Body.String())
	}
}
