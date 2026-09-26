package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/auth"
)

// newAdminCommand groups the deployment-administration verbs.
//
// These are authenticated Admin API calls, the automation-friendly peer of the
// Portal administration area. They are not the break-glass path: creating the
// first administrator and recovering a deployment with none stay in
// `buildmax-server admin`, which reaches the database directly. This command
// speaks to a running server as the signed-in administrator, and holds no
// authority a login cannot reach.
func newAdminCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administer a running deployment",
		Long: "Administer a running deployment as the signed-in administrator, over\n" +
			"the same API the Portal administration area uses.\n\n" +
			"Bootstrapping the first administrator and recovering a deployment that\n" +
			"has lost every administrator are done with `buildmax-server admin` on\n" +
			"the machine that holds the database, not here.",
	}
	cmd.AddCommand(newAdminListCommand())
	cmd.AddCommand(newAdminGrantCommand())
	cmd.AddCommand(newAdminRevokeCommand())
	cmd.AddCommand(newAdminUserCommand())
	cmd.AddCommand(newAdminModelCommand())
	return cmd
}

func newAdminListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List deployment administrators",
		Args:  cobra.NoArgs,
		RunE:  runAdminList,
	}
	cmd.Flags().Bool("all", false, "include revoked grants")
	return cmd
}

func newAdminGrantCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "grant <email>",
		Short: "Grant administrator authority to an account",
		Long: "Grant deployment-administrator authority to an existing account,\n" +
			"named by its email. Granting does not create the account.",
		Args: cobra.ExactArgs(1),
		RunE: runAdminGrant,
	}
}

func newAdminRevokeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <email>",
		Short: "Revoke an account's administrator authority",
		Long: "Revoke deployment-administrator authority from an account, named by\n" +
			"its email. Revoking the deployment's last administrator is refused\n" +
			"here; `buildmax-server admin revoke` on the database machine is the\n" +
			"way to do that deliberately.",
		Args: cobra.ExactArgs(1),
		RunE: runAdminRevoke,
	}
}

// adminSessionFor resolves the signed-in server and a token for it, the same way
// every authenticated command does.
func adminSessionFor() (serverURL, token string, err error) {
	info, err := auth.Info()
	if err != nil {
		return "", "", fmt.Errorf("read credentials: %w", err)
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return "", "", fmt.Errorf("not signed in: run `buildmax login` first")
	}
	token, err = auth.TokenForServer(info.ServerURL)
	if err != nil {
		return "", "", fmt.Errorf("authenticate to %s: %w", info.ServerURL, err)
	}
	return info.ServerURL, token, nil
}

func runAdminList(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	includeRevoked, _ := cmd.Flags().GetBool("all")
	grants, err := auth.ServerClient(serverURL).ListSystemGrants(cmd.Context(), token, includeRevoked)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(grants) == 0 {
		fmt.Fprintln(out, "no administrators")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "EMAIL\tROLE\tSTATE\tGRANTED BY\tGRANTED AT")
	for _, g := range grants {
		email := g.Email
		if email == "" {
			email = g.UserID
		}
		state := "active"
		if !g.Active() {
			state = "revoked"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			email, g.Role, state, g.GrantedBy, g.GrantedAt.Local().Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

func runAdminGrant(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	c := auth.ServerClient(serverURL)
	account, err := c.FindAccountByEmail(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	grant, err := c.GrantSystemRole(cmd.Context(), token, account.ID, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "granted %s to %s\n", grant.Role, account.Email)
	return nil
}

func runAdminRevoke(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	c := auth.ServerClient(serverURL)
	account, err := c.FindAccountByEmail(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	if err := c.RevokeSystemRole(cmd.Context(), token, account.ID, ""); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "revoked administrator authority from %s\n", account.Email)
	return nil
}
