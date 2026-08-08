package cmd

import "github.com/spf13/cobra"

// blogAuthorCommand reads one Caixin blogger's home page.
//
// The page server-renders only its newest posts; the rest sits behind a "load
// more" button this client does not press. The payload says which it gave you.
func (a *application) blogAuthorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "blog-author <url>",
		Short: "Read one Caixin blogger's profile and server-rendered posts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.BlogAuthor(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
