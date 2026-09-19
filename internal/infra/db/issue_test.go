package db

import (
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/util"
)

// TestCreatedIssueCarriesTheVersionItWasWrittenWith covers the value a create
// response owes its caller.
//
// An update is refused unless it sends the version it read, and a zero counts
// as not sent. The create response is the only place a client has read that
// version from, so an issue created and then assigned — which is what Portal
// does, and what the deployment smoke's run-trace path does — depends entirely
// on this field surviving the return.
//
// It is asserted here rather than against the store because the store tests
// need a MySQL DSN and skip without one, which is every CI run. This is the
// same mapping with none of that.
func TestCreatedIssueCarriesTheVersionItWasWrittenWith(t *testing.T) {
	now := time.Now().UTC()
	row := &issueRow{
		PublicID:    "is_public",
		Title:       "Run trace probe",
		Description: "created by a test",
		Status:      coreissue.StatusTodo,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	got := createdIssue(row, "tm_1", "us_1", coreissue.CreateInput{ParentIssueID: util.Ptr("is_parent")})

	if got.Version != row.Version {
		t.Errorf("version = %d, want %d; a client cannot update an issue whose create response carried no version",
			got.Version, row.Version)
	}
	if got.ID != row.PublicID {
		t.Errorf("id = %q, want the public id the row was given, %q", got.ID, row.PublicID)
	}
	if got.Status != row.Status {
		t.Errorf("status = %q, want %q", got.Status, row.Status)
	}
	if got.Title != row.Title || got.Description != row.Description {
		t.Errorf("title/description = %q/%q, want %q/%q", got.Title, got.Description, row.Title, row.Description)
	}
	if !got.CreatedAt.Equal(row.CreatedAt) || !got.UpdatedAt.Equal(row.UpdatedAt) {
		t.Errorf("timestamps = %s/%s, want %s/%s", got.CreatedAt, got.UpdatedAt, row.CreatedAt, row.UpdatedAt)
	}
	if got.SpaceID != "tm_1" || got.CreatedBy != "us_1" || got.UserID != "us_1" {
		t.Errorf("ownership = space %q, created by %q, user %q", got.SpaceID, got.CreatedBy, got.UserID)
	}
	if got.ParentIssueID == nil || *got.ParentIssueID != "is_parent" {
		t.Errorf("parent = %v, want is_parent", got.ParentIssueID)
	}
}
