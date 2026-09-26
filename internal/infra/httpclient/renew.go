package httpclient

import (
	"io"
	"net/http"
	"strings"
)

// RenewFunc is told the bearer token the server just rejected and returns the
// one to retry with. It owns deciding whether a renewal is warranted; an error
// means there is nothing better to send.
type RenewFunc func(rejected string) (string, error)

// RenewOnUnauthorized wraps base so a request whose bearer token is refused
// with 401 is retried once with a renewed token.
//
// A client cannot see every reason a token stops working. Expiry it can read
// from the token itself, but a rotated signing secret or a server that no
// longer trusts the signature leaves the token looking valid until the server
// says otherwise. The 401 is that signal, and renewing on it is what lets a
// long-lived session survive a JWT secret rotation.
//
// One retry, never more: a second 401 goes back to the caller, whose own
// classification decides what to report. So does the first one when renewing
// fails or the body cannot be replayed, because the caller's view of the
// response should be the server's, not this layer's.
func RenewOnUnauthorized(base http.RoundTripper, renew RenewFunc) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &renewTransport{base: base, renew: renew}
}

type renewTransport struct {
	base  http.RoundTripper
	renew RenewFunc
}

func (t *renewTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	// Only a request that presented a credential has one to renew; an anonymous
	// endpoint answering 401 is not a session problem.
	rejected := bearerOf(req)
	if rejected == "" {
		return resp, nil
	}
	replayable := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	if !replayable {
		return resp, nil
	}
	token, renewErr := t.renew(rejected)
	if renewErr != nil || token == "" || token == rejected {
		return resp, nil
	}

	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil
		}
		retry.Body = body
	}
	retry.Header.Set("Authorization", "Bearer "+token)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
	_ = resp.Body.Close()
	return t.base.RoundTrip(retry)
}

func bearerOf(req *http.Request) string {
	const prefix = "Bearer "
	h := req.Header.Get("Authorization")
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return h[len(prefix):]
}
