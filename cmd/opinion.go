package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

// The three 观点 directories share a shape: the server renders one screen and
// the rest comes from a gateway endpoint whose parameters the page prints into
// a script. Page 1 is that rendered screen; a later page is fetched only when a
// caller asks for it by number, and never walked automatically.

// opinionDirectoryCommand builds one of the three, which differ only in which
// client method reads them.
func (a *application) opinionDirectoryCommand(
	use, short string,
	read func(context.Context, string, int) (map[string]any, error),
) *cobra.Command {
	var page, limit int
	command := &cobra.Command{
		Use:   use + " <url>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateClientSideLimit(cmd, limit); err != nil {
				return err
			}
			result, err := read(cmd.Context(), args[0], page)
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

// opinionColumnsCommand lists the 名家专栏 directory.
func (a *application) opinionColumnsCommand() *cobra.Command {
	return a.opinionDirectoryCommand("opinion-columns",
		"List the Caixin 观点 名家专栏 directory",
		func(ctx context.Context, url string, page int) (map[string]any, error) {
			client, err := a.client()
			if err != nil {
				return nil, err
			}
			return client.OpinionColumns(ctx, url, page)
		})
}

// opinionUpfrontCommand lists the 火线评论 directory.
func (a *application) opinionUpfrontCommand() *cobra.Command {
	return a.opinionDirectoryCommand("opinion-upfront",
		"List the Caixin 观点 火线评论 directory",
		func(ctx context.Context, url string, page int) (map[string]any, error) {
			client, err := a.client()
			if err != nil {
				return nil, err
			}
			return client.OpinionUpfront(ctx, url, page)
		})
}

// opinionAuthorDirectoryCommand lists the 观点作者 roster.
func (a *application) opinionAuthorDirectoryCommand() *cobra.Command {
	return a.opinionDirectoryCommand("opinion-author-directory",
		"List the Caixin 观点作者 roster",
		func(ctx context.Context, url string, page int) (map[string]any, error) {
			client, err := a.client()
			if err != nil {
				return nil, err
			}
			return client.OpinionAuthorDirectory(ctx, url, page)
		})
}
