package cmd

import "github.com/spf13/cobra"

// cultureSectionCommand lists one 文化 channel section.
//
// The page server-renders one screen and loads the rest behind a button this
// client never presses; `pagination.load_more_not_called` says so.
func (a *application) cultureSectionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "culture-section <url>",
		Short: "List one Caixin 文化 section as the server rendered it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.CultureSection(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}

// cultureAuthorCommand reads one 文化 columnist page.
//
// The page mixes the author's own pieces with the channel ranking and the
// roster of other columnists, so the counts are reported separately: only
// `article_items_count` is about writing, and `author_links_count` is other
// people.
func (a *application) cultureAuthorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "culture-author <url>",
		Short: "Read one Caixin 文化 columnist page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.CultureAuthor(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
