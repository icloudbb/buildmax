package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/interface/client"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Signing in through the browser: Desktop starts a native sign-in request,
// opens the Portal page that confirms it, and polls until the person confirms
// or denies it there (design enterprise-identity-and-access §14.7). Polling
// runs here rather than in the page, so the outcome arrives as one event and
// the poll secret never reaches the webview.

// eventBrowserSignIn reports how a browser sign-in ended. A cancelled or
// superseded request reports nothing: the page already left it.
const eventBrowserSignIn = "desktop/browser-sign-in"

// Outcomes of eventBrowserSignIn.
const (
	browserSignInSignedIn = "signed_in"
	browserSignInDenied   = "denied"
	browserSignInExpired  = "expired"
	browserSignInFailed   = "failed"
)

// BrowserSignInResultPayload is eventBrowserSignIn's data.
type BrowserSignInResultPayload struct {
	// Outcome is "signed_in", "denied", "expired", or "failed".
	Outcome string `json:"outcome"`
	// Status is the new login, set only when Outcome is "signed_in".
	Status *AuthStatus `json:"status,omitempty"`
	// Message is the server's or the store's error when Outcome is "failed".
	Message string `json:"message,omitempty"`
}

// BrowserSignIn is what the waiting panel shows while the person confirms.
type BrowserSignIn struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn is seconds.
	ExpiresIn int64 `json:"expires_in"`
}

// methodsTimeout bounds reading a server's sign-in methods: the page waits on
// it, and an address that does not answer is the unreachable-server state.
const methodsTimeout = 10 * time.Second

// browserSignIns holds the one request this window is waiting on, and the two
// window operations it uses. Those are Wails runtime calls in the app and
// stand-ins in tests, which must never open a real browser.
type browserSignIns struct {
	mu      sync.Mutex
	pending *pendingSignIn
	open    func(ctx context.Context, url string)
	copy    func(ctx context.Context, text string) error
}

type pendingSignIn struct {
	cancel context.CancelFunc
	link   string
}

func newBrowserSignIns() browserSignIns {
	return browserSignIns{open: runtime.BrowserOpenURL, copy: runtime.ClipboardSetText}
}

// GetAuthMethods reads which ways in the server at serverURL offers, so the
// sign-in page can lead with the one that works for its accounts.
func (a *App) GetAuthMethods(serverURL string) (*client.AuthMethods, error) {
	ctx, cancel := context.WithTimeout(context.Background(), methodsTimeout)
	defer cancel()
	return client.NewClient(strings.TrimSpace(serverURL)).AuthMethods(ctx)
}

// StartBrowserSignIn starts a sign-in request at serverURL, opens the system
// browser at the page that confirms it, and waits for the decision in the
// background. Starting again replaces any request still waiting.
func (a *App) StartBrowserSignIn(serverURL string) (*BrowserSignIn, error) {
	serverURL = strings.TrimSpace(serverURL)
	c := client.NewClient(serverURL)
	started, err := c.StartNativeSignIn(context.Background(), "desktop", client.ClientLabel())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := &pendingSignIn{cancel: cancel, link: started.VerificationURIComplete}
	a.signIns.mu.Lock()
	if a.signIns.pending != nil {
		a.signIns.pending.cancel()
	}
	a.signIns.pending = req
	a.signIns.mu.Unlock()

	a.signIns.open(a.uiContext(), started.VerificationURIComplete)
	go a.awaitBrowserSignIn(ctx, req, c, serverURL, started)
	return &BrowserSignIn{
		UserCode:                started.UserCode,
		VerificationURI:         started.VerificationURI,
		VerificationURIComplete: started.VerificationURIComplete,
		ExpiresIn:               started.ExpiresIn,
	}, nil
}

// OpenBrowserSignInAgain reopens the page for the request being waited on,
// for a browser window the person closed or lost.
func (a *App) OpenBrowserSignInAgain() error {
	link, err := a.waitingLink()
	if err != nil {
		return err
	}
	a.signIns.open(a.uiContext(), link)
	return nil
}

// CopyBrowserSignInLink copies that page's address, for a browser on another
// device or profile. Go copies it because clipboard access from the webview
// is not reliable in the native window.
func (a *App) CopyBrowserSignInLink() error {
	link, err := a.waitingLink()
	if err != nil {
		return err
	}
	return a.signIns.copy(a.uiContext(), link)
}

// CancelBrowserSignIn stops waiting. The request is left to expire on the
// server; no session is created for it, because only this window's poll could
// redeem it.
func (a *App) CancelBrowserSignIn() {
	a.signIns.mu.Lock()
	defer a.signIns.mu.Unlock()
	if a.signIns.pending != nil {
		a.signIns.pending.cancel()
		a.signIns.pending = nil
	}
}

func (a *App) waitingLink() (string, error) {
	a.signIns.mu.Lock()
	defer a.signIns.mu.Unlock()
	if a.signIns.pending == nil {
		return "", errors.New("no browser sign-in is waiting")
	}
	return a.signIns.pending.link, nil
}

// awaitBrowserSignIn polls until the request ends, then saves the login the
// way a password sign-in does and reports the outcome, unless the page
// cancelled or replaced the request meanwhile.
func (a *App) awaitBrowserSignIn(ctx context.Context, req *pendingSignIn, c *client.Client, serverURL string, started *client.NativeSignIn) {
	lr, err := c.WaitForNativeSignIn(ctx, started)
	a.signIns.mu.Lock()
	current := a.signIns.pending == req && ctx.Err() == nil
	if current {
		a.signIns.pending = nil
	}
	a.signIns.mu.Unlock()
	req.cancel()
	if !current {
		if err == nil {
			// Redeemed in the instant before a cancellation: the person asked
			// not to be signed in, so end the session nobody will hold.
			revokeCtx, cancel := context.WithTimeout(context.Background(), methodsTimeout)
			defer cancel()
			_ = c.Logout(revokeCtx, lr.RefreshToken, lr.Access())
		}
		return
	}

	result := BrowserSignInResultPayload{}
	switch {
	case err == nil:
		status, saveErr := a.saveLogin(serverURL, lr)
		if saveErr != nil {
			result.Outcome, result.Message = browserSignInFailed, saveErr.Error()
		} else {
			result.Outcome, result.Status = browserSignInSignedIn, status
		}
	case errors.Is(err, client.ErrSignInDenied):
		result.Outcome = browserSignInDenied
	case errors.Is(err, client.ErrSignInExpired):
		result.Outcome = browserSignInExpired
	default:
		result.Outcome, result.Message = browserSignInFailed, err.Error()
	}
	a.emit(a.uiContext(), eventBrowserSignIn, result)
}

// uiContext is the window's context, nil before Startup.
func (a *App) uiContext() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}
