package cmd

import (
	"context"
	"strconv"

	"github.com/fatecannotbealtered/caixin-cli/internal/caixin"
	"github.com/fatecannotbealtered/caixin-cli/internal/output"
	"github.com/spf13/cobra"
)

// run is the single seam every query command funnels through.
func (a *application) run(cmd *cobra.Command, call func(context.Context, *caixin.Client) (map[string]any, error)) error {
	client, err := a.client()
	if err != nil {
		return err
	}
	data, err := call(cmd.Context(), client)
	if err != nil {
		return err
	}
	return a.success(data)
}

func (a *application) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether a Caixin session is loaded",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			// Answering "not logged in" is a successful query, not a failure.
			// A loaded session also does not imply paid entitlement.
			return a.success(map[string]any{
				"authenticated":           client.Authenticated(),
				"state_dir":               client.StateDirectory(),
				"entitlement_not_implied": true,
			})
		},
	}
}

func (a *application) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Discard the stored Caixin session",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			if err := client.Logout(); err != nil {
				return output.WrapError("E_IO", "could not remove the stored session", err, nil)
			}
			return a.success(map[string]any{"logged_out": true})
		},
	}
}

func (a *application) queryCommands() []*cobra.Command {
	var commands []*cobra.Command

	commands = append(commands, &cobra.Command{
		Use:   "channels",
		Short: "List the scroll-news channel menu",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Channels(ctx)
			})
		},
	})

	var latestPage, latestSize, latestChannel int
	var latestDate string
	latest := &cobra.Command{
		Use:   "latest",
		Short: "Read the legacy ?v=old scroll feed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Latest(ctx, latestPage, latestSize, latestDate, latestChannel)
			})
		},
	}
	latest.Flags().IntVar(&latestPage, "page", 1, "Page number")
	latest.Flags().IntVar(&latestSize, "size", 20, "Items per page")
	latest.Flags().IntVar(&latestChannel, "channel", 0, "Channel id")
	latest.Flags().StringVar(&latestDate, "date", "", "Restrict to one YYYY-MM-DD date")
	commands = append(commands, latest)

	var scrollPage int
	var scrollDate, scrollCategory string
	newscroll := &cobra.Command{
		Use:   "newscroll",
		Short: "Read the site's default rolling news list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Newscroll(ctx, scrollPage, scrollDate, scrollCategory)
			})
		},
	}
	newscroll.Flags().IntVar(&scrollPage, "page", 1, "Page number")
	newscroll.Flags().StringVar(&scrollDate, "date", "", "Restrict to one YYYY-MM-DD date")
	newscroll.Flags().StringVar(&scrollCategory, "category", "", "Channel code from the live menu")
	commands = append(commands, newscroll)

	commands = append(commands, &cobra.Command{
		Use:   "search-menu",
		Short: "Report the live search scopes, sorts, and filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.SearchMenu(ctx)
			})
		},
	})

	options := caixin.SearchOptions{}
	search := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search Caixin, filtered by the live menu",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Keyword = args[0]
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Search(ctx, options)
			})
		},
	}
	search.Flags().IntVar(&options.Page, "page", 1, "Page number")
	search.Flags().IntVar(&options.Size, "size", 10, "Results per page (max 20)")
	search.Flags().StringVar(&options.CategoryCode, "category", "20", "Category code from search-menu")
	search.Flags().IntVar(&options.Sort, "sort", 0, "Sort order supported by the category")
	search.Flags().IntVar(&options.TimeRange, "time-range", 0, "0 any, 1 day, 2 week, 3 month, 4 year, 5 custom")
	search.Flags().StringVar(&options.StartTime, "start-date", "", "Custom range start (requires --time-range 5)")
	search.Flags().StringVar(&options.EndTime, "end-date", "", "Custom range end (requires --time-range 5)")
	search.Flags().StringVar(&options.FilterCode, "filter", "all", "Scope: all, text, author, or title")
	commands = append(commands, search)

	var feedPage, feedSize int
	cxdata := &cobra.Command{
		Use:   "cxdata-feed <category>",
		Short: "Read one of the nine public Caixin Data feeds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.CXDataFeedItems(ctx, args[0], feedPage, feedSize)
			})
		},
	}
	cxdata.Flags().IntVar(&feedPage, "page", 1, "Page number")
	cxdata.Flags().IntVar(&feedSize, "size", 25, "Items per page (max 25)")
	commands = append(commands, cxdata)

	commands = append(commands, &cobra.Command{
		Use:   "entities-preview <companies|persons>",
		Short: "Read the single anonymous preview record for an entity library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.EntitiesPreview(ctx, args[0])
			})
		},
	})

	var topicsPage, topicsSize int
	topics := &cobra.Command{
		Use:   "topics <entry-url>",
		Short: "List one of the six topic directory entry points",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Topics(ctx, args[0], topicsPage, topicsSize)
			})
		},
	}
	topics.Flags().IntVar(&topicsPage, "page", 1, "Page number")
	topics.Flags().IntVar(&topicsSize, "size", 25, "Items per page (max 25)")
	commands = append(commands, topics)

	var frontlinePage, frontlineSize int
	frontline := &cobra.Command{
		Use:   "frontline",
		Short: "List 财新一线 flash news",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Frontline(ctx, frontlinePage, frontlineSize)
			})
		},
	}
	frontline.Flags().IntVar(&frontlinePage, "page", 1, "Page number")
	frontline.Flags().IntVar(&frontlineSize, "size", 20, "Items per page (max 20)")
	commands = append(commands, frontline)

	commands = append(commands, &cobra.Command{
		Use:   "frontline-detail <code>",
		Short: "Read one flash-news item by its 32-hex code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.FrontlineDetail(ctx, args[0])
			})
		},
	})

	var bloggersPage int
	var bloggersSort string
	bloggers := &cobra.Command{
		Use:   "bloggers-directory",
		Short: "List one explicit page of the public blogger directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.BloggersDirectory(ctx, bloggersPage, bloggersSort)
			})
		},
	}
	bloggers.Flags().IntVar(&bloggersPage, "page", 1, "Page number")
	bloggers.Flags().StringVar(&bloggersSort, "sort", "latest", "Sort: latest or pinyin")
	commands = append(commands, bloggers)

	// route is deliberately offline: it classifies a link and hands back argv.
	// An agent runs that argv verbatim and never concatenates it into a shell.
	commands = append(commands, &cobra.Command{
		Use:   "route <url>",
		Short: "Decide locally which command consumes a clicked url",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.success(caixin.ClassifyURL(args[0]).AsMap())
		},
	})

	return commands
}

var _ = strconv.Itoa
