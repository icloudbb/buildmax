package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to a BuildMax server",
		RunE:  runLogin,
	}
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

func runLogin(_ *cobra.Command, _ []string) error {
	return interactiveLogin()
}

// interactiveLogin prompts for server URL, email, and OTP, then saves
// credentials on success. Used by both the login subcommand and the TUI
// startup gate.
func interactiveLogin() error {
	reader := bufio.NewReader(os.Stdin)
	s, _ := config.LoadSettings()
	serverDefault := s.ServerURL
	if serverDefault == "" {
		serverDefault = client.DefaultServerURL
	}
	fmt.Fprintf(os.Stdout, "Server URL [%s]: ", serverDefault)
	serverURL := readLine(reader)
	if serverURL == "" {
		serverURL = serverDefault
	}

	fmt.Fprint(os.Stdout, "Email: ")
	email := readLine(reader)
	if email == "" {
		return fmt.Errorf("email is required")
	}

	ctx := context.Background()
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
		lr, err = c.LoginWithPassword(ctx, email, password, "cli")
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
		lr, err = c.Login(ctx, email, otp, "cli")
		if err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}

	creds := &auth.Credentials{
		ServerURL:    serverURL,
		Token:        lr.Access(),
		RefreshToken: lr.RefreshToken,
		UserID:       lr.User.ID,
		Email:        lr.User.Email,
		Name:         lr.User.Name,
	}
	if err := auth.SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Logged in as %s on %s\n", lr.User.Email, serverURL)
	// Said at login rather than only on request: this is the moment someone
	// decides whether signing in on this machine is acceptable.
	fmt.Fprintf(os.Stdout, "Credentials: %s\n", auth.StorageDescription(creds.Storage))
	return nil
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
