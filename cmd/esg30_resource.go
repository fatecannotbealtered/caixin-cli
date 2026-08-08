package cmd

import "github.com/spf13/cobra"

// esg30ResourceCommand reads one sponsored campaign page.
//
// The page is read only after the directory that listed it, and the payload
// says which directory that was: the same page reached from the ESG30 index and
// from the promote directory are two different claims about what it is.
func (a *application) esg30ResourceCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "esg30-resource <url>",
		Short: "Read one sponsored Caixin campaign page as a link surface",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.ESG30Resource(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
