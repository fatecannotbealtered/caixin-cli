package cmd

import "github.com/spf13/cobra"

// sectionDirectoryCommand lists one Caixin channel section.
//
// The layout is asserted rather than tolerated: a section page that grew a
// second article list or lost a sidebar means the template moved, and half a
// directory returned confidently is worse than an error.
func (a *application) sectionDirectoryCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "section-directory <url>",
		Short: "List one Caixin channel section as the server rendered it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.SectionDirectory(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
