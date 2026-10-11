package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	identitysvc "github.com/icloudbb/buildmax/internal/service/identity"
)

// Native sign-in confirmed in the Portal (design §14). A CLI or Desktop starts
// a request and polls with its secret; a person confirms it in the Portal by
// its user code. Neither the code nor the secret is written to a log: the code
// arrives in a body or in the redacted `code` query key, the secret only in a
// body, and the request log records neither bodies nor unredacted queries.

// nativeSignInRetryAfter is what a start refused by the pending cap is told.
// Requests expire within ten minutes, so a slot frees well within that.
const nativeSignInRetryAfter = time.Minute

// NativeSignInStartRequest is the body of POST /api/auth/native-sign-ins.
type NativeSignInStartRequest struct {
	// Platform is "cli" or "desktop".
	Platform string `json:"platform"`
	// ClientLabel is what the client says it is, such as "alice-mbp · macOS".
	// Shown to the person confirming, marked as not verified.
	ClientLabel string `json:"client_label"`
}

// NativeSignInStartResponse tells the client what to show and how to poll.
type NativeSignInStartResponse struct {
	// UserCode is shown as XXXX-XXXX for the person to compare in the Portal.
	UserCode string `json:"user_code"`
	// PollSecret redeems the session. Only the client that started the request
	// holds it.
	PollSecret string `json:"poll_secret"`
	// ExpiresIn and Interval are seconds.
	ExpiresIn int64 `json:"expires_in"`
	Interval  int64 `json:"interval"`
	// VerificationURI is the Portal page where a code is typed;
	// VerificationURIComplete is the same page with this code filled in.
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
}

// NativeSignInPollRequest is the body of POST /api/auth/native-sign-ins/poll.
type NativeSignInPollRequest struct {
	PollSecret string `json:"poll_secret"`
}

// NativeSignInCodeRequest is the body of the confirm and deny routes.
type NativeSignInCodeRequest struct {
	Code string `json:"code"`
}

// NativeSignInView is what the Portal shows before a person decides.
type NativeSignInView struct {
	// Platform is "cli" or "desktop"; ClientLabel is the client's unverified
	// claim about itself.
	Platform    string    `json:"platform"`
	ClientLabel string    `json:"client_label,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	// SessionExpiresAt is when the session a confirmation leads to would end:
	// the confirming Portal session's own absolute expiry.
	SessionExpiresAt time.Time `json:"session_expires_at"`
}

// startNativeSignInHandler serves POST /api/auth/native-sign-ins.
func (h *Handler) startNativeSignInHandler(w http.ResponseWriter, r *http.Request) {
	var req NativeSignInStartRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	started, err := h.identityService().StartNativeSignIn(r.Context(), identitysvc.StartNativeSignInCmd{
		Platform: req.Platform, ClientLabel: req.ClientLabel,
	})
	if err != nil {
		if errors.Is(err, identitysvc.ErrTooManyPendingSignIns) {
			w.Header().Set("Retry-After", strconv.Itoa(int(nativeSignInRetryAfter/time.Second)))
		}
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "auth handler error", "handler", "native_sign_in_start")
		}
		return
	}
	base := h.verificationBase(r)
	w.Header().Set("Cache-Control", "no-store")
	httputil.WriteJSON(w, http.StatusOK, NativeSignInStartResponse{
		UserCode:                started.UserCode,
		PollSecret:              started.PollSecret,
		ExpiresIn:               int64(started.ExpiresIn / time.Second),
		Interval:                int64(started.Interval / time.Second),
		VerificationURI:         base + "/#/sign-in",
		VerificationURIComplete: base + "/#/sign-in/" + started.UserCode,
	})
}

// verificationBase is public_base_url when configured, otherwise the origin the
// client called. The URL goes back only to the caller that started the request
// and is never used for a server-side redirect, so a forged Host misleads only
// that caller.
func (h *Handler) verificationBase(r *http.Request) string {
	if base := strings.TrimRight(h.cfg.PublicBaseURL, "/"); base != "" {
		return base
	}
	return requestOrigin(r)
}

// pollNativeSignInHandler serves POST /api/auth/native-sign-ins/poll. The four
// non-success answers are RFC 8628's, as 400 with the name as the error code.
func (h *Handler) pollNativeSignInHandler(w http.ResponseWriter, r *http.Request) {
	var req NativeSignInPollRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	result, err := h.identityService().PollNativeSignIn(r.Context(), req.PollSecret)
	if err != nil {
		for _, answer := range []error{
			identitysvc.ErrAuthorizationPending, identitysvc.ErrSlowDown,
			identitysvc.ErrAccessDenied, identitysvc.ErrExpiredToken,
		} {
			if errors.Is(err, answer) {
				httputil.WriteJSONError(w, http.StatusBadRequest, answer.Error())
				return
			}
		}
		if !httputil.WriteServiceError(w, err) {
			httputil.WriteInternalError(w, err, "auth handler error", "handler", "native_sign_in_poll")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.WriteJSON(w, http.StatusOK, loginResponse(result))
}

// lookUpNativeSignInHandler serves GET /api/auth/native-sign-ins?code=.
func (h *Handler) lookUpNativeSignInHandler(w http.ResponseWriter, r *http.Request) {
	userID, sid, ok := h.codeCaller(w, r)
	if !ok {
		return
	}
	view, err := h.identityService().LookUpNativeSignIn(r.Context(), userID, sid, r.URL.Query().Get("code"))
	if err != nil {
		h.writeCodeError(w, userID, err, "native_sign_in_lookup")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, NativeSignInView{
		Platform:         view.Request.Platform,
		ClientLabel:      view.Request.ClientLabel,
		CreatedAt:        view.Request.CreatedAt,
		ExpiresAt:        view.Request.ExpiresAt,
		SessionExpiresAt: view.SessionExpiresAt,
	})
}

// confirmNativeSignInHandler serves POST /api/auth/native-sign-ins/confirm.
func (h *Handler) confirmNativeSignInHandler(w http.ResponseWriter, r *http.Request) {
	h.decideNativeSignIn(w, r, "native_sign_in_confirm", (*identitysvc.Service).ConfirmNativeSignIn)
}

// denyNativeSignInHandler serves POST /api/auth/native-sign-ins/deny.
func (h *Handler) denyNativeSignInHandler(w http.ResponseWriter, r *http.Request) {
	h.decideNativeSignIn(w, r, "native_sign_in_deny", (*identitysvc.Service).DenyNativeSignIn)
}

// decideNativeSignIn answers 204 once the decision is recorded. A wrong,
// expired, or already-decided code is one 404, whichever it was.
func (h *Handler) decideNativeSignIn(w http.ResponseWriter, r *http.Request, name string,
	decide func(s *identitysvc.Service, ctx context.Context, userID, sid, code string) error,
) {
	userID, sid, ok := h.codeCaller(w, r)
	if !ok {
		return
	}
	var req NativeSignInCodeRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if err := decide(h.identityService(), r.Context(), userID, sid, req.Code); err != nil {
		h.writeCodeError(w, userID, err, name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// codeCaller authenticates a route that takes a sign-in code and refuses an
// account that has already tried too many unknown ones.
func (h *Handler) codeCaller(w http.ResponseWriter, r *http.Request) (userID, sid string, ok bool) {
	userID, sid, ok = h.guard().ActiveUserSession(w, r)
	if !ok {
		return "", "", false
	}
	if h.codeAttempts.blocked(userID, h.now()) {
		httputil.WriteJSONError(w, http.StatusTooManyRequests, "too many sign-in code attempts; try again later")
		return "", "", false
	}
	return userID, sid, true
}

// writeCodeError answers a refused look-up or decision, counting an unknown
// code against the account.
func (h *Handler) writeCodeError(w http.ResponseWriter, userID string, err error, name string) {
	if errors.Is(err, identitysvc.ErrNativeSignInCodeInvalid) {
		h.codeAttempts.fail(userID, h.now())
	}
	if errors.Is(err, identitysvc.ErrDisabled) {
		httputil.WriteJSONError(w, http.StatusForbidden, access.DisabledMessage)
		return
	}
	if !httputil.WriteServiceError(w, err) {
		httputil.WriteInternalError(w, err, "auth handler error", "handler", name, "user_id", userID)
	}
}

// Unknown-code attempts per account, per process (design §14.6). Lookups are
// authenticated, so the count is attributable; it is per replica, which
// multiplies the bound by the replica count and still leaves one guess against
// 500 live 40-bit codes below 10^-9.
const (
	codeAttemptLimit  = 10
	codeAttemptWindow = 10 * time.Minute
	// codeAttemptSweepAt bounds the map: past this many accounts, entries whose
	// failures have all aged out are dropped.
	codeAttemptSweepAt = 10000
)

type codeAttempts struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newCodeAttempts() *codeAttempts {
	return &codeAttempts{failures: map[string][]time.Time{}}
}

// recent requires c.mu. It drops failures older than the window.
func (c *codeAttempts) recent(userID string, now time.Time) []time.Time {
	kept := c.failures[userID][:0]
	for _, at := range c.failures[userID] {
		if now.Sub(at) < codeAttemptWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(c.failures, userID)
		return nil
	}
	c.failures[userID] = kept
	return kept
}

func (c *codeAttempts) blocked(userID string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.recent(userID, now)) >= codeAttemptLimit
}

func (c *codeAttempts) fail(userID string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.failures) >= codeAttemptSweepAt {
		for id := range c.failures {
			c.recent(id, now)
		}
	}
	c.failures[userID] = append(c.recent(userID, now), now)
}
