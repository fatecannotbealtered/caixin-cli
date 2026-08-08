package cmd

import "github.com/spf13/cobra"

// publicDirectoryCommand lists one standing Caixin index page.
//
// It lists; it never fetches what it lists. The payload says so through
// `directory_only` and `article_details_not_fetched`, so a caller cannot mistake
// a title here for having read the piece.
func (a *application) publicDirectoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "public-directory <url>",
		Short: "List one public Caixin directory page as the server rendered it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.PublicDirectory(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
