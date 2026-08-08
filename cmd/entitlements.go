package cmd

import "github.com/spf13/cobra"

// entitlementsCommand reports what the signed-in account may read.
//
// This is the honest answer to "can I read paid articles" -- it asks the
// account service rather than inferring entitlement from whether some fetch
// happened to succeed.
func (a *application) entitlementsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "entitlements",
		Short: "Report the signed-in account's subscription and per-feature grants",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.Entitlements(cmd.Context())
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
