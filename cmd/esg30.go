package cmd

import "github.com/spf13/cobra"

// esg30SubdirectoryCommand reads one ESG30 sub-index.
//
// It reads the parent directory first and refuses a url the parent is not
// currently listing, which is what `discovery_required` on the route verdict
// promises.
func (a *application) esg30SubdirectoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "esg30-subdirectory <url>",
		Short: "List one ESG30 sub-index, after confirming its parent lists it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.ESG30Subdirectory(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
