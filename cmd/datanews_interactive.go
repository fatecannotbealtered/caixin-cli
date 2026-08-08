package cmd

import "github.com/spf13/cobra"

// datanewsInteractiveCommand reads one 数字说 interactive project.
//
// The visualisation itself is drawn by scripts this client does not run, so the
// payload reports the page's framing and says plainly that the interactive
// content was not rendered.
func (a *application) datanewsInteractiveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "datanews-interactive <url>",
		Short: "Read one Caixin 数字说 interactive project's server-rendered framing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.DatanewsInteractive(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
