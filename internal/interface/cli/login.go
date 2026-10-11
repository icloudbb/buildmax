package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCommand() *cobra.Command {
	var password, noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to a BuildMax server",
		Long: `Sign in to a BuildMax server.

By default you confirm the sign-in in a browser, signed in to the server's
Portal however your account signs in there, including single sign-on. The
command opens the browser when this machine has a screen to open it on, and
always prints the link and the code, so you can confirm from another device.

--password asks for your email and a password or login code instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reader := bufio.NewReader(os.Stdin)
			serverURL := promptServerURL(reader)
			if password {
				return passwordLogin(cmd.Context(), reader, serverURL)
			}
			open := !noBrowser && browserOnThisScreen(os.Getenv, runtime.GOOS)
			return browserLogin(cmd.Context(), os.Stdout, serverURL, open)
		},
	}
	cmd.Flags().BoolVar(&password, "password", false, "sign in with your email and a password or login code instead of a browser")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the sign-in link and code without opening a browser")
	return cmd
}

func newLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out and clear stored credentials",
		RunE:  runLogout,
	}
}

func newMeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show current login status",
		RunE:  runMe,
	}
}

// promptServerURL asks which server to sign in to, offering the configured one.
func promptServerURL(reader *bufio.Reader) string {
	s, _ := config.LoadSettings()
	serverDefault := s.ServerURL
	if serverDefault == "" {
		serverDefault = client.DefaultServerURL
	}
	fmt.Fprintf(os.Stdout, "Server URL [%s]: ", serverDefault)
	if serverURL := readLine(reader); serverURL != "" {
		return serverURL
	}
	return serverDefault
}

// browserLogin signs in through a request confirmed in the Portal (design
// enterprise-identity-and-access §14.7). It works for every account the
// deployment admits, SSO included, and no password passes through the
// terminal. Ctrl-C stops waiting and leaves the request to expire.
func browserLogin(ctx context.Context, w io.Writer, serverURL string, openBrowser bool) error {
	c := client.NewClient(serverURL)
	started, err := c.StartNativeSignIn(ctx, "cli", client.ClientLabel())
	if err != nil {
		return fmt.Errorf("start the sign-in at %s: %w", serverURL, err)
	}
	fmt.Fprintf(w, "To sign in, open this page in a browser and confirm the request:\n\n  %s\n\n", started.VerificationURI)
	fmt.Fprintf(w, "Check that the page shows this code: %s\n", started.UserCode)
	fmt.Fprintf(w, "(The request expires in %s. Press Ctrl-C to cancel.)\n\n", expiryPhrase(started.ExpiresIn))
	if openBrowser {
		if err := openExternalURL(started.VerificationURIComplete); err != nil {
			fmt.Fprintf(w, "Could not open a browser (%v); open the page above instead.\n", err)
		} else {
			fmt.Fprintf(w, "Opened your browser at %s\n", started.VerificationURIComplete)
		}
	}
	fmt.Fprintln(w, "Waiting for confirmation…")

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	lr, err := c.WaitForNativeSignIn(ctx, started)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		fmt.Fprintln(os.Stderr, "Sign-in cancelled.")
		return &ExitError{Code: ExitUserCancelled}
	case errors.Is(err, client.ErrSignInDenied):
		return errors.New("sign-in was not allowed: it was denied in the browser, or the account that confirmed it can no longer sign in")
	case errors.Is(err, client.ErrSignInExpired):
		return errors.New("the sign-in request expired before it was confirmed; run `buildmax login` to start again")
	default:
		return fmt.Errorf("the server refused the sign-in: %w", err)
	}
	creds, err := saveLogin(serverURL, lr)
	if err != nil {
		return err
	}
	// Naming the account is how a person notices a code confirmed by the wrong
	// one (design §15).
	fmt.Fprintf(w, "Signed in to %s as %s.\n", serverURL, lr.User.Email)
	fmt.Fprintf(w, "Credentials: %s\n", auth.StorageDescription(creds.Storage))
	return nil
}

// expiryPhrase is the request's lifetime as a person would say it.
func expiryPhrase(seconds int64) string {
	if seconds >= 60 && seconds%60 == 0 {
		if seconds == 60 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", seconds/60)
	}
	return fmt.Sprintf("%d seconds", seconds)
}

// passwordLogin prompts for an email, then a password or a login code, against
// POST /api/auth/login. It is the path that does not depend on the Portal:
// operator break-glass, and scripted sign-in where local_login allows it.
func passwordLogin(ctx context.Context, reader *bufio.Reader, serverURL string) error {
	fmt.Fprint(os.Stdout, "Email: ")
	email := readLine(reader)
	if email == "" {
		return fmt.Errorf("email is required")
	}

	c := client.NewClient(serverURL)

	// Password first, since that is the everyday way in. An empty one falls
	// through to a login code, which is how someone claims a new account or
	// recovers a forgotten password — there is no mail channel, so an operator
	// issues that code by hand.
	password, err := readPassword("Password (leave blank to use a login code): ")
	if err != nil {
		return err
	}

	var lr *client.LoginResponse
	if password != "" {
		lr, err = c.LoginWithPassword(ctx, email, password, "cli", client.ClientLabel())
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stdout, "Ask an administrator for a login code: `buildmax admin user login-code "+email+"`")
		fmt.Fprint(os.Stdout, "Login code: ")
		otp := readLine(reader)
		if otp == "" {
			return fmt.Errorf("a password or a login code is required")
		}
		lr, err = c.Login(ctx, email, otp, "cli", client.ClientLabel())
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}

	creds, err := saveLogin(serverURL, lr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Logged in as %s on %s\n", lr.User.Email, serverURL)
	// Said at login rather than only on request: this is the moment someone
	// decides whether signing in on this machine is acceptable.
	fmt.Fprintf(os.Stdout, "Credentials: %s\n", auth.StorageDescription(creds.Storage))
	return nil
}

// saveLogin stores a login the same way whichever path produced it, so the
// credentials refresh and log out alike.
func saveLogin(serverURL string, lr *client.LoginResponse) (*auth.Credentials, error) {
	creds := &auth.Credentials{
		ServerURL:    serverURL,
		Token:        lr.Access(),
		RefreshToken: lr.RefreshToken,
		UserID:       lr.User.ID,
		Email:        lr.User.Email,
		Name:         lr.User.Name,
	}
	if err := auth.SaveCredentials(creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}
	return creds, nil
}

func runLogout(_ *cobra.Command, _ []string) error {
	err := auth.LogoutAndRevoke()
	fmt.Fprintln(os.Stdout, "Logged out.")
	if err != nil {
		// The credentials are gone either way. Say what did not happen rather
		// than reporting a failure for something that succeeded locally.
		fmt.Fprintf(os.Stderr, "warning: the session may still be active on the server: %v\n", err)
	}
	return nil
}

func runMe(cmd *cobra.Command, _ []string) error {
	// The stored login, not auth.Info: Info reads only local timestamps, so it
	// calls a login the server has revoked "logged in" and a locally expired one
	// "not logged in" — the second of which is still the mode every run uses.
	creds, err := auth.StoredLogin()
	if err != nil {
		return fmt.Errorf("load auth: %w", err)
	}
	if creds == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Not logged in. Prompts go straight from this machine to the providers in settings.yaml.")
		return nil
	}
	// Asking the deployment is the only way to know whether it still accepts
	// the login; a status that cannot say so is the one that matters most.
	if _, err := resolveModelSource(cmd.Context()); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Signed in as %s on %s, but the login cannot be used right now.\n", creds.Email, creds.ServerURL)
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Logged in as %s on %s\n", creds.Email, creds.ServerURL)
	fmt.Fprintf(cmd.OutOrStdout(), "Credentials: %s\n", auth.StorageDescription(creds.Storage))
	return nil
}

func readLine(r *bufio.Reader) string {
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

// readPassword prompts and reads without echoing.
//
// When stdin is not a terminal — a pipe, a script — it reads a line normally.
// There is nothing to hide from in that case, and refusing would make the
// command unusable from anything but an interactive shell.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stdout, prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", nil
		}
		return strings.TrimSpace(line), nil
	}
	raw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}
