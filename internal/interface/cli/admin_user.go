package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/auth"
)

// newAdminUserCommand groups the account-lifecycle verbs, reached over the Admin
// API as the signed-in administrator — the automation peer of the Portal
// Accounts area. Creating the first account for bootstrap and recovering a
// locked-out one stay in `buildmax-server user`, which reaches the database
// directly; these are the routine, authenticated equivalents.
func newAdminUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage deployment accounts",
	}
	cmd.AddCommand(newAdminUserListCommand())
	cmd.AddCommand(newAdminUserCreateCommand())
	cmd.AddCommand(newAdminUserLoginCodeCommand())
	cmd.AddCommand(newAdminUserDisableCommand())
	cmd.AddCommand(newAdminUserEnableCommand())
	return cmd
}

func newAdminUserListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List accounts",
		Args:  cobra.NoArgs,
		RunE:  runAdminUserList,
	}
	cmd.Flags().String("search", "", "keep only accounts whose email contains this")
	return cmd
}

func newAdminUserCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <email>",
		Short: "Create an account",
		Long: "Create an account by email. Creating it grants no way in; issue a login\n" +
			"code afterwards with `buildmax admin user login-code`.",
		Args: cobra.ExactArgs(1),
		RunE: runAdminUserCreate,
	}
}

func newAdminUserLoginCodeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login-code <email>",
		Short: "Issue a single-use login code for an account",
		Long: "Issue a single-use login code, named by the account's email. The code is\n" +
			"printed once and recoverable nowhere; deliver it over a channel you trust.",
		Args: cobra.ExactArgs(1),
		RunE: runAdminUserLoginCode,
	}
}

func newAdminUserDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <email>",
		Short: "Disable an account and revoke its sessions",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runAdminUserSetDisabled(cmd, args[0], true) },
	}
}

func newAdminUserEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <email>",
		Short: "Re-enable a disabled account",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runAdminUserSetDisabled(cmd, args[0], false) },
	}
}

func runAdminUserList(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	search, _ := cmd.Flags().GetString("search")
	accounts, total, err := auth.ServerClient(serverURL).ListAccounts(cmd.Context(), token, search)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(accounts) == 0 {
		fmt.Fprintln(out, "no accounts")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tID\tSTATE")
	for _, a := range accounts {
		state := "active"
		if a.Disabled() {
			state = "disabled"
		} else if !a.HasPassword {
			state = "no password yet"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Email, a.ID, state)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if total > len(accounts) {
		fmt.Fprintf(out, "\nshowing %d of %d; narrow with --search\n", len(accounts), total)
	}
	return nil
}

func runAdminUserCreate(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	account, err := auth.ServerClient(serverURL).CreateAccount(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "created %s (%s)\n", account.Email, account.ID)
	fmt.Fprintln(out, "it cannot sign in yet — issue a login code with `buildmax admin user login-code`")
	return nil
}

func runAdminUserLoginCode(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	c := auth.ServerClient(serverURL)
	account, err := c.FindAccountByEmail(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	code, expiresAt, err := c.IssueLoginCode(cmd.Context(), token, account.ID)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "login code for %s: %s\n", account.Email, code)
	fmt.Fprintf(out, "valid until %s. Shown once; deliver it over a channel you trust.\n",
		expiresAt.Local().Format("2006-01-02 15:04"))
	return nil
}

func runAdminUserSetDisabled(cmd *cobra.Command, email string, disabled bool) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	c := auth.ServerClient(serverURL)
	account, err := c.FindAccountByEmail(cmd.Context(), token, email)
	if err != nil {
		return err
	}
	change, err := c.SetAccountDisabled(cmd.Context(), token, account.ID, disabled)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if !disabled {
		fmt.Fprintf(out, "%s can sign in again\n", change.Email)
		return nil
	}
	fmt.Fprintf(out, "disabled %s; %d session token(s) revoked\n", change.Email, change.SessionsRevoked)
	// The account is disabled either way; a nonzero exit tells a script the
	// cleanup still needs the retry this message names.
	if len(change.CleanupFailed) > 0 {
		return fmt.Errorf("%s is disabled, but cleanup failed for: %s; run `buildmax admin user disable %s` again to retry (safe: it re-runs the cleanup)",
			change.Email, strings.Join(change.CleanupFailed, ", "), change.Email)
	}
	return nil
}
