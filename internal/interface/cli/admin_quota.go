package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/interface/auth"
)

// newAdminQuotaTierCommand groups the quota-tier verbs: list the seeded tiers
// and move a Space onto one. Tier definitions are not editable from here or
// anywhere else; assignment is the operator's lever.
func newAdminQuotaTierCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "quota-tier",
		Short: "List quota tiers and assign a space to one",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the quota tiers a space can be assigned to",
		Args:  cobra.NoArgs,
		RunE:  runAdminQuotaTierList,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <space_id> <tier>",
		Short: "Assign a space to an existing quota tier",
		Long: "Assign a space — shared or personal — to an existing quota tier. The\n" +
			"new limits apply from the space's next quota check; work already\n" +
			"running is not stopped. The change is recorded in the audit trail with\n" +
			"the old and new tier.",
		Args: cobra.ExactArgs(2),
		RunE: runAdminQuotaTierSet,
	})
	return cmd
}

func runAdminQuotaTierList(cmd *cobra.Command, _ []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	tiers, err := auth.ServerClient(serverURL).ListQuotaTiers(cmd.Context(), token)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(tiers) == 0 {
		fmt.Fprintln(out, "no quota tiers are defined")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIER\tRUNS\tTOKENS\tPERIOD\tSTORAGE")
	for _, t := range tiers {
		storage := "unlimited"
		if t.MaxStorageBytes > 0 {
			storage = fmt.Sprintf("%d bytes", t.MaxStorageBytes)
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d days\t%s\n", t.TierName, t.MaxRunsPerPeriod, t.MaxTokensPerPeriod, t.PeriodDays, storage)
	}
	return w.Flush()
}

func runAdminQuotaTierSet(cmd *cobra.Command, args []string) error {
	serverURL, token, err := adminSessionFor()
	if err != nil {
		return err
	}
	if err := auth.ServerClient(serverURL).SetSpaceQuotaTier(cmd.Context(), token, args[0], args[1]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "space %s is now on the %s tier\n", args[0], args[1])
	return nil
}
