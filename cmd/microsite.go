package cmd

import "github.com/spf13/cobra"

// micrositeCommand reads one standalone Caixin microsite.
//
// A microsite is a hand-built campaign page, so it is reported as a link
// surface -- what it links, what it offers for download, and how much of it is
// script this client did not run -- rather than as prose.
func (a *application) micrositeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "microsite <url>",
		Short: "Read one standalone Caixin microsite as a link surface",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.Microsite(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
