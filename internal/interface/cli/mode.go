package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/icloudbb/buildmax/internal/interface/auth"
)

// resolveModelSource fetches what this session's models are, turning a failure
// to reach them into an answerable question rather than a bare error.
//
// The session does not run on an expired login, and it does not quietly fall
// back to local models either: that would send a prompt to a provider the user
// did not choose for this session. What it does is name the one action that
// returns the client to local mode — `buildmax logout`, which removes the
// credentials that are the mode. See docs/design/client-modes.md section 8.
func resolveModelSource(ctx context.Context) (auth.ModelSource, error) {
	source, err := auth.ResolveModelSource(ctx)
	if err == nil {
		return source, nil
	}
	if next := modeNextStep(err); next != "" {
		return auth.ModelSource{}, fmt.Errorf("%w\n\n%s", err, next)
	}
	return auth.ModelSource{}, err
}

// modeNextStep says what a signed-in user can do about a failure to reach
// their deployment's models, or "" when the failure is not one of those.
//
// Each case names the actions that fit it and no others. An outage in
// particular is not an ended login: the login still works, so the first answer
// is to try again, not to sign out.
func modeNextStep(err error) string {
	switch {
	case errors.Is(err, auth.ErrLoginExpired):
		return "Sign in again with `buildmax login`, or run `buildmax logout` to use the\n" +
			"models in settings.yaml. Nothing here runs until one of those happens: a\n" +
			"session must not send prompts somewhere you did not choose."
	case errors.Is(err, auth.ErrAccountDisabled):
		return "An administrator of that deployment disabled this account; signing in\n" +
			"again will not help until one of them re-enables it. Run `buildmax logout`\n" +
			"to use the models in settings.yaml instead."
	case errors.Is(err, auth.ErrServerUnavailable):
		return "Your login still works. While you are signed in, prompts go only to that\n" +
			"deployment, so the models in settings.yaml are not used in its place. Try\n" +
			"again once it is back, or run `buildmax logout` to work locally."
	}
	return ""
}
