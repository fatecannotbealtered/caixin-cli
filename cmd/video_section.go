package cmd

import "github.com/spf13/cobra"

// videoSectionCommand lists one 视频 channel directory.
//
// Page 1 is the screen the server rendered. Later pages come from the
// continuation endpoint the page itself declares, and are fetched only when a
// caller asks for them by number -- this client never walks the list on its own.
func (a *application) videoSectionCommand() *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use:   "video-section <url>",
		Short: "List one Caixin 视频 channel directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateClientSideLimit(cmd, limit); err != nil {
				return err
			}
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.VideoSection(cmd.Context(), args[0], page)
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
	return command
}
