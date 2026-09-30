package space

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const spaceTestSecret = "space-test-secret"

func mustListSpaceMembers(t *testing.T, store *mock.MockSpaceStore, spaceID string) []corespace.Member {
	t.Helper()
	list, err := store.ListSpaceMembers(context.Background(), spaceID)
	if err != nil {
		t.Fatalf("ListSpaceMembers: %v", err)
	}
	return list
}

func TestSpaceHandlers(t *testing.T) {
	personalSpaceID := "tm_personal_u1"
	sharedSpaceID := "tm_shared"
	spaceStore := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: personalSpaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1", CreatedAt: time.Unix(100, 0).UTC(), UpdatedAt: time.Unix(100, 0).UTC()},
			{ID: sharedSpaceID, Name: "Ops", CreatedBy: "u1", CreatedAt: time.Unix(200, 0).UTC(), UpdatedAt: time.Unix(200, 0).UTC()},
		},
		Members: []corespace.Member{
			{SpaceID: personalSpaceID, UserID: "u1", Role: corespace.RoleOwner, CreatedAt: time.Unix(100, 0).UTC()},
			{SpaceID: sharedSpaceID, UserID: "u1", Role: corespace.RoleOwner, CreatedAt: time.Unix(200, 0).UTC()},
			{SpaceID: sharedSpaceID, UserID: "u2", Role: corespace.RoleMember, CreatedAt: time.Unix(201, 0).UTC()},
		},
	}
	userStore := &mock.MockUserStore{
		ByEmail: map[string]*coreidentity.User{
			"u1@example.com": {ID: "u1", Email: "u1@example.com", Name: "Alice"},
			"u2@example.com": {ID: "u2", Email: "u2@example.com", Name: "Bob"},
			"u3@example.com": {ID: "u3", Email: "u3@example.com", Name: "Carol"},
			"u4@example.com": {ID: "u4", Email: "u4@example.com", Name: "Dana"},
		},
		ByID: map[string]*coreidentity.User{
			"u1": {ID: "u1", Email: "u1@example.com", Name: "Alice"},
			"u2": {ID: "u2", Email: "u2@example.com", Name: "Bob"},
			"u3": {ID: "u3", Email: "u3@example.com", Name: "Carol"},
			"u4": {ID: "u4", Email: "u4@example.com", Name: "Dana"},
		},
	}
	loginCodes := &mock.MockLoginCodeStore{}
	h := New(Config{
		JWTSecret:  spaceTestSecret,
		Users:      userStore,
		Spaces:     spaceStore,
		LoginCodes: loginCodes,
	})
	mux := http.NewServeMux()
	h.Register(mux)

	t.Run("GET list spaces", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var out []spaceResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("spaces len = %d, want 2", len(out))
		}
		if out[0].ID != personalSpaceID || out[1].ID != sharedSpaceID {
			t.Fatalf("spaces = %+v", out)
		}
	})

	t.Run("POST create space", func(t *testing.T) {
		body := bytes.NewBufferString(`{"name":"Design"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out spaceResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode create space: %v", err)
		}
		if out.Name != "Design" {
			t.Fatalf("space name = %q, want %q", out.Name, "Design")
		}
		list, err := spaceStore.ListSpacesByUser(req.Context(), "u1")
		if err != nil {
			t.Fatalf("ListSpacesByUser after create: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("spaces len after create = %d, want 3", len(list))
		}
	})

	t.Run("POST create space rejects whitespace and over-long names", func(t *testing.T) {
		for _, name := range []string{"   ", "", strings.Repeat("x", 300)} {
			payload, _ := json.Marshal(map[string]string{"name": name})
			req := httptest.NewRequest(http.MethodPost, "/api/spaces", bytes.NewReader(payload))
			req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("name %q status = %d, want 400 body=%s", name[:min(len(name), 8)], rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("GET space members", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+sharedSpaceID+"/members", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var out []spaceMemberResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		if len(out) != 2 {
			t.Fatalf("members len = %d, want 2", len(out))
		}
		if out[0].UserName == nil || *out[0].UserName != "Alice" {
			t.Fatalf("member[0].user_name = %+v, want Alice", out[0].UserName)
		}
		if out[1].UserEmail == nil || *out[1].UserEmail != "u2@example.com" {
			t.Fatalf("member[1].user_email = %+v, want u2@example.com", out[1].UserEmail)
		}
	})

	t.Run("POST invite by owner then accept", func(t *testing.T) {
		body := bytes.NewBufferString(`{"email":"u3@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/invitations", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out invitationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode invitation: %v", err)
		}
		if out.UserID != "u3" {
			t.Fatalf("invited user id = %q, want u3", out.UserID)
		}

		// Not yet a member: the invitation is pending, not active.
		list, err := spaceStore.ListSpaceMembers(req.Context(), sharedSpaceID)
		if err != nil {
			t.Fatalf("ListSpaceMembers after invite: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("members len after invite = %d, want 2 (still pending)", len(list))
		}

		acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+out.ID+"/accept", nil)
		acceptReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u3", spaceTestSecret))
		acceptRec := httptest.NewRecorder()
		mux.ServeHTTP(acceptRec, acceptReq)
		if acceptRec.Code != http.StatusOK {
			t.Fatalf("accept status = %d, want %d body=%s", acceptRec.Code, http.StatusOK, acceptRec.Body.String())
		}

		list, err = spaceStore.ListSpaceMembers(req.Context(), sharedSpaceID)
		if err != nil {
			t.Fatalf("ListSpaceMembers after accept: %v", err)
		}
		if len(list) != 3 {
			t.Fatalf("members len after accept = %d, want 3", len(list))
		}
	})

	t.Run("POST invite forbidden for non-owner-or-admin", func(t *testing.T) {
		body := bytes.NewBufferString(`{"email":"u4@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/invitations", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("POST invite requires existing user", func(t *testing.T) {
		body := bytes.NewBufferString(`{"email":"missing@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/invitations", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("DELETE revoke a pending invitation", func(t *testing.T) {
		body := bytes.NewBufferString(`{"email":"u4@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/invitations", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("invite status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
		}
		var out invitationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode invitation: %v", err)
		}

		delReq := httptest.NewRequest(http.MethodDelete, "/api/spaces/"+sharedSpaceID+"/invitations/"+out.ID, nil)
		delReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		delRec := httptest.NewRecorder()
		mux.ServeHTTP(delRec, delReq)
		if delRec.Code != http.StatusNoContent {
			t.Fatalf("revoke status = %d, want %d body=%s", delRec.Code, http.StatusNoContent, delRec.Body.String())
		}

		acceptReq := httptest.NewRequest(http.MethodPost, "/api/invitations/"+out.ID+"/accept", nil)
		acceptReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u4", spaceTestSecret))
		acceptRec := httptest.NewRecorder()
		mux.ServeHTTP(acceptRec, acceptReq)
		if acceptRec.Code != http.StatusConflict {
			t.Fatalf("accepting a revoked invitation status = %d, want %d body=%s", acceptRec.Code, http.StatusConflict, acceptRec.Body.String())
		}
	})

	t.Run("GET my invitations lists only my own", func(t *testing.T) {
		body := bytes.NewBufferString(`{"email":"u4@example.com"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/invitations", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		mux.ServeHTTP(httptest.NewRecorder(), req)

		listReq := httptest.NewRequest(http.MethodGet, "/api/invitations", nil)
		listReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u4", spaceTestSecret))
		listRec := httptest.NewRecorder()
		mux.ServeHTTP(listRec, listReq)
		if listRec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", listRec.Code, http.StatusOK, listRec.Body.String())
		}
		var mine []invitationResponse
		if err := json.Unmarshal(listRec.Body.Bytes(), &mine); err != nil {
			t.Fatalf("decode invitations: %v", err)
		}
		for _, inv := range mine {
			if inv.UserID != "u4" {
				t.Fatalf("GET /api/invitations returned someone else's invitation: %+v", inv)
			}
		}

		otherReq := httptest.NewRequest(http.MethodGet, "/api/invitations", nil)
		otherReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", spaceTestSecret))
		otherRec := httptest.NewRecorder()
		mux.ServeHTTP(otherRec, otherReq)
		var notMine []invitationResponse
		if err := json.Unmarshal(otherRec.Body.Bytes(), &notMine); err != nil {
			t.Fatalf("decode invitations: %v", err)
		}
		if len(notMine) != 0 {
			t.Fatalf("u2's invitations = %+v, want none", notMine)
		}
	})

	t.Run("PATCH promote then demote a member", func(t *testing.T) {
		var createdAt time.Time
		for _, m := range mustListSpaceMembers(t, spaceStore, sharedSpaceID) {
			if m.UserID == "u2" {
				createdAt = m.CreatedAt
			}
		}

		promote := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u2", promote)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("promote status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out memberRoleResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode role response: %v", err)
		}
		if out.Role != "admin" {
			t.Fatalf("role = %q, want admin", out.Role)
		}

		demote := bytes.NewBufferString(`{"role":"member"}`)
		demoteReq := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u2", demote)
		demoteReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		demoteReq.Header.Set("Content-Type", "application/json")
		demoteRec := httptest.NewRecorder()
		mux.ServeHTTP(demoteRec, demoteReq)
		if demoteRec.Code != http.StatusOK {
			t.Fatalf("demote status = %d, want %d body=%s", demoteRec.Code, http.StatusOK, demoteRec.Body.String())
		}

		for _, m := range mustListSpaceMembers(t, spaceStore, sharedSpaceID) {
			if m.UserID == "u2" && !m.CreatedAt.Equal(createdAt) {
				t.Fatalf("CreatedAt changed from %v to %v; a role change must not read as a fresh join", createdAt, m.CreatedAt)
			}
		}
	})

	t.Run("PATCH forbidden for non-owner", func(t *testing.T) {
		body := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u2", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("PATCH transfer ownership then transfer back", func(t *testing.T) {
		transfer := bytes.NewBufferString(`{"role":"owner"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u2", transfer)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("transfer status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		for _, m := range mustListSpaceMembers(t, spaceStore, sharedSpaceID) {
			if m.UserID == "u2" && m.Role != "owner" {
				t.Errorf("u2 role = %q, want owner", m.Role)
			}
			if m.UserID == "u1" && m.Role != "admin" {
				t.Errorf("u1 (former owner) role = %q, want admin", m.Role)
			}
		}

		back := bytes.NewBufferString(`{"role":"owner"}`)
		backReq := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u1", back)
		backReq.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", spaceTestSecret))
		backReq.Header.Set("Content-Type", "application/json")
		backRec := httptest.NewRecorder()
		mux.ServeHTTP(backRec, backReq)
		if backRec.Code != http.StatusOK {
			t.Fatalf("transfer back status = %d, want %d body=%s", backRec.Code, http.StatusOK, backRec.Body.String())
		}
	})

	t.Run("PATCH sole owner cannot demote themselves without transferring", func(t *testing.T) {
		body := bytes.NewBufferString(`{"role":"admin"}`)
		req := httptest.NewRequest(http.MethodPatch, "/api/spaces/"+sharedSpaceID+"/members/u1", body)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
	})

	t.Run("POST issue a login code for a locked-out member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/members/u2/login-code", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
		var out memberLoginCodeResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode login code response: %v", err)
		}
		if out.Code == "" {
			t.Fatal("code is empty")
		}
	})

	t.Run("POST login code forbidden for non-owner", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+sharedSpaceID+"/members/u2/login-code", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u2", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})

	t.Run("DELETE remove space member by owner", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/spaces/"+sharedSpaceID+"/members/u2", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
		}
		list, err := spaceStore.ListSpaceMembers(req.Context(), sharedSpaceID)
		if err != nil {
			t.Fatalf("ListSpaceMembers after remove: %v", err)
		}
		for _, member := range list {
			if member.UserID == "u2" {
				t.Fatalf("u2 should have been removed")
			}
		}
	})

	t.Run("DELETE remove self forbidden", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/spaces/"+sharedSpaceID+"/members/u1", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("GET space members forbidden when not a member", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/spaces/"+sharedSpaceID+"/members", nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u4", spaceTestSecret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
		}
	})
}
