package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/interface/client"
)

// AuthInfo is the caller-facing authentication view.
type AuthInfo struct {
	LoggedIn  bool   `json:"logged_in"`
	ServerURL string `json:"server_url,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	// Storage is StorageKeyring or StorageFile. A surface reports it because
	// the file mode is a downgrade, and one nobody is told about is one nobody
	// can act on.
	Storage string `json:"storage,omitempty"`
}

// Info returns the current caller-facing auth state.
func Info() (AuthInfo, error) {
	creds, err := Load(config.AuthPath())
	if err != nil {
		return AuthInfo{}, err
	}
	// Usable, not valid: a login whose access token has expired but whose
	// refresh token has not is still a login. Reporting it as signed out would
	// send someone to ask an operator for a code they do not need.
	if creds == nil || !creds.IsUsable() {
		return AuthInfo{LoggedIn: false}, nil
	}
	return AuthInfo{
		LoggedIn:  true,
		ServerURL: creds.ServerURL,
		UserID:    creds.UserID,
		Email:     creds.Email,
		Name:      creds.Name,
		Storage:   creds.Storage,
	}, nil
}

// IsLoggedIn reports whether valid credentials are present.
func IsLoggedIn() (bool, error) {
	info, err := Info()
	if err != nil {
		return false, err
	}
	return info.LoggedIn, nil
}

// RequireLogin returns an error when no valid credentials are present.
func RequireLogin() error {
	loggedIn, err := IsLoggedIn()
	if err != nil {
		return fmt.Errorf("load auth: %w", err)
	}
	if !loggedIn {
		return fmt.Errorf("not logged in")
	}
	return nil
}

// refreshSkew is how long before expiry the access token is renewed. A managed
// call can run for minutes, and it is authorized when it starts, so handing out
// a token that expires in the middle of one is worse than renewing early.
const refreshSkew = 2 * time.Minute

// refreshMu serializes refreshes inside this process.
//
// The server tolerates a second exchange of the same refresh token for a few
// seconds precisely because separate BuildMax processes share this file. That
// tolerance is for processes; goroutines in one process have a mutex and should
// use it rather than spend the allowance.
var refreshMu sync.Mutex

// TokenForServer returns a usable access token for calls to serverURL,
// renewing it first if it is spent or nearly so.
//
// It refuses to hand the credential to any other host. A managed model entry
// names its own server, and settings.yaml is editable by anything running as
// the user, so a mismatch would otherwise send a BuildMax token — and every
// prompt that follows — wherever that file said.
func TokenForServer(serverURL string) (string, error) {
	if serverURL == "" {
		return "", errors.New("managed model entry has no server_url")
	}
	creds, err := loadForServer(serverURL)
	if err != nil {
		return "", err
	}
	if !creds.needsRefresh(refreshSkew) {
		return creds.Token, nil
	}
	if creds.RefreshToken == "" {
		return "", fmt.Errorf("%w: its access token expired and there is nothing to renew it", ErrLoginExpired)
	}
	return refreshTokenForServer(serverURL, func(c *Credentials) bool { return c.needsRefresh(refreshSkew) })
}

// RenewRejected returns an access token to retry with after serverURL refused
// rejected, renewing the login unless another caller already has.
//
// It is the one place a native client reacts to a 401. Local expiry is not the
// only way a token dies: rotating the server's JWT secret invalidates every
// access token while each still looks unexpired here, and without this the
// login would read as ended until the token's own exp passed.
//
// Concurrent 401s carry the same rejected token, and the refresh token rotates
// on every exchange, so only the first may spend it. The comparison runs under
// refreshMu against the file re-read there: whoever comes second finds a
// different token already stored and is handed that one, which also covers a
// second BuildMax process that renewed first.
func RenewRejected(serverURL, rejected string) (string, error) {
	return refreshTokenForServer(serverURL, func(c *Credentials) bool {
		return c.Token == rejected || c.needsRefresh(refreshSkew)
	})
}

// HTTPClient returns the HTTP client for calls to serverURL made with the
// stored login: a request refused with 401 is retried once with the token
// RenewRejected returns.
func HTTPClient(serverURL string) *http.Client {
	return &http.Client{Transport: httpclient.RenewOnUnauthorized(http.DefaultTransport,
		func(rejected string) (string, error) { return RenewRejected(serverURL, rejected) })}
}

// ServerClient returns an API client for serverURL whose calls survive the
// server refusing a token that still looks valid. Every signed-in command uses
// it instead of client.NewClient.
func ServerClient(serverURL string) *client.Client {
	c := client.NewClient(serverURL)
	c.HTTPClient = HTTPClient(serverURL)
	return c
}

// CanAuthenticate reports whether a call to serverURL could authenticate,
// without making one.
//
// TokenForServer answers the same question by renewing when it has to, which
// contacts the server and rotates the stored refresh token. That is the wrong
// thing for a diagnostic to do: `buildmax doctor` is read-only, and a check
// that quietly spends a credential is not a check.
func CanAuthenticate(serverURL string) error {
	if serverURL == "" {
		return errors.New("managed model entry has no server_url")
	}
	creds, err := loadForServer(serverURL)
	if err != nil {
		return err
	}
	if !creds.IsUsable() {
		return errors.New("login has expired: run `buildmax login`")
	}
	return nil
}

func loadForServer(serverURL string) (*Credentials, error) {
	creds, err := Load(config.AuthPath())
	if err != nil {
		return nil, fmt.Errorf("load auth: %w", err)
	}
	if creds == nil || creds.Token == "" {
		return nil, errors.New("not logged in: run `buildmax login`")
	}
	if !sameServer(creds.ServerURL, serverURL) {
		return nil, fmt.Errorf("logged in to %s, but this model entry uses %s: run `buildmax login` against that server",
			creds.ServerURL, serverURL)
	}
	return creds, nil
}

// refreshTokenForServer exchanges the stored refresh token and persists the
// result, when stale says the stored access token should not be used.
//
// The file is re-read under the lock and stale is asked about what is there
// now. Another goroutine or BuildMax process may have refreshed while this one
// waited, in which case its token is already on disk and spending another
// exchange would only race it.
func refreshTokenForServer(serverURL string, stale func(*Credentials) bool) (string, error) {
	refreshMu.Lock()
	defer refreshMu.Unlock()

	creds, err := loadForServer(serverURL)
	if err != nil {
		return "", err
	}
	if !stale(creds) {
		return creds.Token, nil
	}
	if creds.RefreshToken == "" {
		return "", fmt.Errorf("%w: its access token is no longer usable and there is nothing to renew it", ErrLoginExpired)
	}

	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	rr, err := newClient(creds.ServerURL).Refresh(ctx, creds.RefreshToken)
	if err != nil {
		if errors.Is(err, client.ErrRefreshRejected) {
			// The session is over: spent, revoked, or reported as reused. The
			// login stays on disk anyway, because its presence is the mode:
			// clearing it here would make the next command run in local mode
			// on its own. Leaving is the user's call (docs/design/client-modes.md
			// section 8).
			return "", fmt.Errorf("%w: the server ended this session", ErrLoginExpired)
		}
		return "", classifyServerError(creds.ServerURL, fmt.Errorf("refresh login: %w", err))
	}

	creds.Token = rr.AccessToken
	if rr.RefreshToken != "" {
		creds.RefreshToken = rr.RefreshToken
	}
	if err := SaveCredentials(creds); err != nil {
		// The exchange succeeded, so the token in hand is good even though the
		// next process will not see it. Failing the call over a write error
		// would be worse than losing one rotation.
		slog.Warn("save refreshed credentials failed", "err", err)
	}
	return rr.AccessToken, nil
}

// refreshTimeout bounds the exchange. It runs inside whatever the caller was
// doing, so it must not hang there.
const refreshTimeout = 30 * time.Second

// newClient is a variable so tests can point the refresh at a stub server.
var newClient = func(serverURL string) refreshClient {
	return client.NewClient(serverURL)
}

// refreshClient is the part of the API client this package needs.
type refreshClient interface {
	Refresh(ctx context.Context, refreshToken string) (*client.RefreshResponse, error)
}

// sameServer compares two server URLs ignoring a trailing slash.
func sameServer(a, b string) bool {
	return strings.TrimRight(a, "/") == strings.TrimRight(b, "/")
}

// SaveCredentials persists a login result.
func SaveCredentials(creds *Credentials) error {
	return Save(creds, config.AuthPath())
}

// Logout clears the stored credentials.
func Logout() error {
	return Clear(config.AuthPath())
}

// LogoutAndRevoke clears the stored credentials and asks the server to revoke
// the session behind them.
//
// The local file is cleared whether or not the server can be reached. Someone
// who typed `buildmax logout` on a machine with no network is signed out of
// that machine; leaving the credentials behind because a call failed would be
// the opposite of what they asked for. The returned error reports what could
// not be revoked, and is worth printing but not worth failing on.
func LogoutAndRevoke() error {
	creds, loadErr := Load(config.AuthPath())
	clearErr := Clear(config.AuthPath())
	if clearErr != nil {
		return clearErr
	}
	if loadErr != nil || creds == nil || creds.ServerURL == "" {
		return nil
	}
	if creds.RefreshToken == "" && creds.Token == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	if err := client.NewClient(creds.ServerURL).Logout(ctx, creds.RefreshToken, creds.Token); err != nil {
		return fmt.Errorf("revoke session on %s: %w", creds.ServerURL, err)
	}
	return nil
}
