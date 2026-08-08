package cmd

import (
	"github.com/spf13/cobra"
)

// snapshotCommand reads one Caixin channel front page.
//
// It returns the server-rendered listing only: no scripts run, so what a
// browser would lazy-load is absent and the payload says so through
// `javascript_executed` and `complete_listing_verified` rather than presenting
// a partial page as the whole one.
func (a *application) snapshotCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "snapshot <url>",
		Short: "Read one Caixin channel front page as the server rendered it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.Snapshot(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
