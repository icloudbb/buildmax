package scheduler

import (
	"context"
	"testing"
	"time"
)

type recordingCredentialStore struct {
	swept []string
}

func (s *recordingCredentialStore) DeleteExpiredLoginCodes(context.Context, time.Time) (int64, error) {
	s.swept = append(s.swept, "login_code")
	return 1, nil
}

func (s *recordingCredentialStore) DeleteExpiredRefreshTokens(context.Context, time.Time) (int64, error) {
	s.swept = append(s.swept, "user_refresh_token")
	return 0, nil
}

func (s *recordingCredentialStore) DeleteExpiredNativeSignIns(context.Context, time.Time) (int64, error) {
	s.swept = append(s.swept, "native_sign_in")
	return 2, nil
}

// TestCredentialCleanerSweepsEveryExpiringCredential: expired native sign-in
// requests are swept with the login codes and refresh tokens.
func TestCredentialCleanerSweepsEveryExpiringCredential(t *testing.T) {
	store := &recordingCredentialStore{}
	NewCredentialCleaner(store, 0).sweep()
	if len(store.swept) != 3 || store.swept[2] != "native_sign_in" {
		t.Errorf("swept %v", store.swept)
	}
}
