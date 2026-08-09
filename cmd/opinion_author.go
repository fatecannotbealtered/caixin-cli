package cmd

import "github.com/spf13/cobra"

// opinionAuthorCommand reads one 观点 columnist page.
//
// The columnist must have been listed somewhere first, and the caller says
// where: the three listings describe the same person differently, and the
// payload echoes back whichever description it used.
func (a *application) opinionAuthorCommand() *cobra.Command {
	var page, limit int
	var discoverySource string
	var directoryPage int
	command := &cobra.Command{
		Use:   "opinion-author <url>",
		Short: "Read one Caixin 观点 columnist page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateClientSideLimit(cmd, limit); err != nil {
				return err
			}
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.OpinionAuthor(
				cmd.Context(), args[0], page, discoverySource, directoryPage)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("limit") {
				if err := limitListResult(result, limit); err != nil {
					return err
				}
			}
			return a.success(result)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "Page to read; page 1 is the server-rendered screen")
	command.Flags().IntVar(&limit, "limit", 0, "Limit returned items")
	command.Flags().StringVar(&discoverySource, "discovery-source", "homepage",
		"Where the columnist was found: homepage, author-directory, or columns")
	command.Flags().IntVar(&directoryPage, "directory-page", 0,
		"Roster page the columnist was found on; required with --discovery-source author-directory")
	return command
}
