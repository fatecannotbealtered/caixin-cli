package cmd

import "github.com/spf13/cobra"

// videoSectionCommand lists one 视频 channel directory.
//
// Page 1 is the screen the server rendered. Later pages come from the
// continuation endpoint the page itself declares, and are fetched only when a
// caller asks for them by number -- this client never walks the list on its own.
func (a *application) videoSectionCommand() *cobra.Command {
	var page int
	command := &cobra.Command{
		Use:   "video-section <url>",
		Short: "List one Caixin 视频 channel directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.VideoSection(cmd.Context(), args[0], page)
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
	command.Flags().IntVar(&page, "page", 1, "Page to read; page 1 is the server-rendered screen")
	return command
}
