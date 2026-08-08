package cmd

import "github.com/spf13/cobra"

// issueCommand reads one magazine issue's table of contents.
//
// It lists the issue; it fetches no article body. `directory_only` says so.
func (a *application) issueCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "issue <url>",
		Short: "List one Caixin magazine issue's contents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.Issue(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
