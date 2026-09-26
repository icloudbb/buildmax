package db

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/util"
)

// TestPublicIDHelpersCanonicalizeOrBlank pins the converter contract: a handle
// canonicalizes to its stored lowercase form, anything else becomes "" rather
// than an error, and a nil handle stays nil.
func TestPublicIDHelpersCanonicalizeOrBlank(t *testing.T) {
	const canonical = "ivyoh5qcfu6ypfkhyedq"
	upper := strings.ToUpper(canonical)
	if got := canonicalPublicID(upper); got != canonical {
		t.Errorf("canonicalPublicID(%q) = %q, want %q", upper, got, canonical)
	}
	if got := canonicalPublicID("not a public id"); got != "" {
		t.Errorf("canonicalPublicID(malformed) = %q, want empty", got)
	}
	if got := optionalCanonicalPublicID(nil); got != nil {
		t.Errorf("optionalCanonicalPublicID(nil) = %v, want nil", got)
	}
	if got := optionalCanonicalPublicID(&upper); got == nil || *got != canonical {
		t.Errorf("optionalCanonicalPublicID(&%q) = %v, want %q", upper, got, canonical)
	}
	if derefPublicID(nil) != "" || derefPublicID(&upper) != upper {
		t.Error("derefPublicID must pass a present handle through and read nil as empty")
	}
}

// TestLookupKeyRefusesAMalformedHandleWithoutQuerying is the cheap half of the
// oracle rule: text that was never a public ID is answered as not-found before
// a query runs, so a malformed identifier cannot be told from a well-formed
// unknown one by how long the answer takes or by what the error says.
//
// The nil database is the assertion. A query would panic.
func TestLookupKeyRefusesAMalformedHandleWithoutQuerying(t *testing.T) {
	for _, bad := range []string{"", "t_9f3k2m8x1qwe7rt4zy", "not a public id", "IVYOH5QCFU6YPFKHYED!"} {
		if _, err := lookupKey(context.Background(), nil, "task", bad); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("lookupKey(%q) = %v, want ErrNotFound", bad, err)
		}
	}
}

// TestAForeignHandleIsIndistinguishableFromAnUnknownOne is the other half, and
// it needs a database because the two answers must come from the same code
// path.
//
// A space-scoped read is constrained by both the handle and the space, so a
// caller holding a real identifier for somebody else's row learns exactly what
// a caller holding a made-up one learns. Without that, a well-formed public ID
// becomes a way to ask whether a row exists in a space you cannot see.
func TestAForeignHandleIsIndistinguishableFromAnUnknownOne(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	owner := newTestUser(t, s, "oracle-owner")
	stranger := newTestUser(t, s, "oracle-stranger")
	ownerSpace := newTestSpace(t, s, owner)
	strangerSpace := newTestSpace(t, s, stranger)

	conv, err := s.CreateConversationInSpace(ctx, ownerSpace, owner, "portal", owner)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	issue, err := s.CreateIssueInSpace(ctx, ownerSpace, owner, coreissue.CreateInput{Title: "private"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}
	unknown, err := util.NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}

	// Listing another space's issue by its real handle, and by one that names
	// nothing, must produce the same answer.
	real, realTotal, err := s.ListIssuesBySpace(ctx, strangerSpace, coreissue.ListFilter{ParentIssueID: issue.ID}, 10, 0)
	if err != nil {
		t.Fatalf("ListIssuesBySpace(foreign parent): %v", err)
	}
	made, madeTotal, err := s.ListIssuesBySpace(ctx, strangerSpace, coreissue.ListFilter{ParentIssueID: unknown}, 10, 0)
	if err != nil {
		t.Fatalf("ListIssuesBySpace(unknown parent): %v", err)
	}
	if len(real) != len(made) || realTotal != madeTotal {
		t.Errorf("a foreign handle answered %d/%d and an unknown one %d/%d; the two must be one answer",
			len(real), realTotal, len(made), madeTotal)
	}

	// The same for a conversation the stranger has no claim to.
	if got, _, err := s.ListConversationsBySpace(ctx, strangerSpace, 10, 0); err != nil || len(got) != 0 {
		t.Errorf("stranger's space listed %d conversations (err %v); the owner's is not theirs", len(got), err)
	}
	if got, err := s.ListTasksByConversation(ctx, conv.ID, "desc"); err != nil || len(got) != 0 {
		t.Errorf("a conversation with no tasks listed %d (err %v)", len(got), err)
	}
}
