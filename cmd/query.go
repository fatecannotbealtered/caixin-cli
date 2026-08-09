package cmd

import (
	"context"
	"strconv"
	"time"

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

// listLimit keeps the legacy --size spelling working while exposing the
// contract-wide --limit spelling. Supplying both is ambiguous and rejected.
func listLimit(cmd *cobra.Command, size, limit int) (int, error) {
	if cmd.Flags().Changed("size") && cmd.Flags().Changed("limit") {
		return 0, output.NewError("E_USAGE", "--size and --limit cannot be used together", nil)
	}
	if cmd.Flags().Changed("limit") {
		return limit, nil
	}
	return size, nil
}

// limitListResult caps list shapes whose upstream page size is fixed. It walks
// the two shapes this CLI exposes: a top-level list and directory modules.
func limitListResult(result map[string]any, limit int) error {
	if limit < 1 {
		return output.NewError("E_VALIDATION", "--limit must be positive", nil)
	}

	remaining, original := limit, 0
	truncate := func(items []any) []any {
		original += len(items)
		kept := len(items)
		if kept > remaining {
			kept = remaining
		}
		remaining -= kept
		return items[:kept]
	}
	for _, key := range []string{"articles", "items"} {
		if items, ok := result[key].([]any); ok {
			result[key] = truncate(items)
		}
	}
	if modules, ok := result["modules"].([]any); ok {
		for _, raw := range modules {
			module, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if items, ok := module["items"].([]any); ok {
				module["items"] = truncate(items)
			}
		}
	}

	count := original
	if count > limit {
		count = limit
		result["truncated"] = true
	}
	result["count"] = count
	for _, key := range []string{"returned", "items_count", "unique_urls_count"} {
		if _, present := result[key]; present {
			result[key] = count
		}
	}

	hasMore, _ := result["has_more"].(bool)
	hasMore = hasMore || original > count
	pagination, _ := result["pagination"].(map[string]any)
	if pagination != nil && pagination["next_page"] != nil {
		hasMore = true
	}
	if last, ok := result["reported_last_page"].(bool); ok && !last {
		hasMore = true
	}
	if more, ok := result["load_more_available"].(bool); ok && more {
		hasMore = true
	}
	result["has_more"] = hasMore
	if result["next_page"] == nil && pagination != nil {
		result["next_page"] = pagination["next_page"]
	}
	if result["next_page"] == nil && hasMore {
		page, ok := result["page"].(int)
		if !ok && pagination != nil {
			page, ok = pagination["page"].(int)
		}
		if !ok {
			page = 1
		}
		result["next_page"] = page + 1
	}
	if !hasMore {
		result["next_page"] = nil
	}
	return nil
}

func validateClientSideLimit(cmd *cobra.Command, limit int) error {
	if cmd.Flags().Changed("limit") && limit < 1 {
		return output.NewError("E_VALIDATION", "--limit must be positive", nil)
	}
	return nil
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
	var dryRun bool
	var confirm string
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Discard the stored Caixin session",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if dryRun && confirm != "" {
				return output.NewError("E_USAGE", "--dry-run and --confirm cannot be used together", nil)
			}
			client, err := a.client()
			if err != nil {
				return err
			}
			configured := client.Authenticated()
			payload := logoutPayload(client.StateDirectory(), configured, client.CredentialFingerprint())
			if dryRun {
				token, expiresAt, err := newLogoutConfirmToken(payload)
				if err != nil {
					return output.WrapError("E_IO", "could not create a confirmation token", err, nil)
				}
				return a.success(map[string]any{
					"preview": map[string]any{"changes": []map[string]any{{
						"action": "delete", "resource": "local_credentials", "id": "current",
						"before": map[string]any{"configured": configured}, "after": nil,
					}}},
					"confirm_token": token,
					"expires_at":    expiresAt.Format(time.RFC3339),
				})
			}
			if err := validateLogoutConfirmToken(confirm, payload); err != nil {
				return err
			}
			if err := consumeLogoutConfirmToken(confirm, payload.StateDir); err != nil {
				return err
			}
			if err := client.Logout(); err != nil {
				return output.WrapError("E_IO", "could not remove the stored session", err, nil)
			}
			return a.success(map[string]any{"logged_out": true, "previously_configured": configured})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview credential deletion and issue a confirmation token")
	cmd.Flags().StringVar(&confirm, "confirm", "", "Execute credential deletion with a token returned by --dry-run")
	return cmd
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

	var latestPage, latestSize, latestLimit, latestChannel int
	var latestDate string
	latest := &cobra.Command{
		Use:   "latest",
		Short: "Read the legacy ?v=old scroll feed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			effectiveSize, err := listLimit(cmd, latestSize, latestLimit)
			if err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Latest(ctx, latestPage, effectiveSize, latestDate, latestChannel)
			})
		},
	}
	latest.Flags().IntVar(&latestPage, "page", 1, "Page number")
	latest.Flags().IntVar(&latestSize, "size", 20, "Items per page")
	latest.Flags().IntVar(&latestLimit, "limit", 0, "Limit returned items")
	latest.Flags().IntVar(&latestChannel, "channel", 0, "Channel id")
	latest.Flags().StringVar(&latestDate, "date", "", "Restrict to one YYYY-MM-DD date")
	commands = append(commands, latest)

	var scrollPage, scrollLimit int
	var scrollDate, scrollCategory string
	newscroll := &cobra.Command{
		Use:   "newscroll",
		Short: "Read the site's default rolling news list",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateClientSideLimit(cmd, scrollLimit); err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				result, err := c.Newscroll(ctx, scrollPage, scrollDate, scrollCategory)
				if err == nil && cmd.Flags().Changed("limit") {
					err = limitListResult(result, scrollLimit)
				}
				return result, err
			})
		},
	}
	newscroll.Flags().IntVar(&scrollPage, "page", 1, "Page number")
	newscroll.Flags().IntVar(&scrollLimit, "limit", 0, "Limit returned items")
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
	var searchLimit int
	search := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Search Caixin, filtered by the live menu",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Keyword = args[0]
			effectiveSize, err := listLimit(cmd, options.Size, searchLimit)
			if err != nil {
				return err
			}
			options.Size = effectiveSize
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Search(ctx, options)
			})
		},
	}
	search.Flags().IntVar(&options.Page, "page", 1, "Page number")
	search.Flags().IntVar(&options.Size, "size", 10, "Results per page (max 20)")
	search.Flags().IntVar(&searchLimit, "limit", 0, "Limit returned items")
	search.Flags().StringVar(&options.CategoryCode, "category", "20", "Category code from search-menu")
	search.Flags().IntVar(&options.Sort, "sort", 0, "Sort order supported by the category")
	search.Flags().IntVar(&options.TimeRange, "time-range", 0, "0 any, 1 day, 2 week, 3 month, 4 year, 5 custom")
	search.Flags().StringVar(&options.StartTime, "start-date", "", "Custom range start (requires --time-range 5)")
	search.Flags().StringVar(&options.EndTime, "end-date", "", "Custom range end (requires --time-range 5)")
	search.Flags().StringVar(&options.FilterCode, "filter", "all", "Scope: all, text, author, or title")
	commands = append(commands, search)

	var feedPage, feedSize, feedLimit int
	cxdata := &cobra.Command{
		Use:   "cxdata-feed <category>",
		Short: "Read one of the nine public Caixin Data feeds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveSize, err := listLimit(cmd, feedSize, feedLimit)
			if err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.CXDataFeedItems(ctx, args[0], feedPage, effectiveSize)
			})
		},
	}
	cxdata.Flags().IntVar(&feedPage, "page", 1, "Page number")
	cxdata.Flags().IntVar(&feedSize, "size", 25, "Items per page (max 25)")
	cxdata.Flags().IntVar(&feedLimit, "limit", 0, "Limit returned items")
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

	var topicsPage, topicsSize, topicsLimit int
	topics := &cobra.Command{
		Use:   "topics <entry-url>",
		Short: "List one of the six topic directory entry points",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveSize, err := listLimit(cmd, topicsSize, topicsLimit)
			if err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Topics(ctx, args[0], topicsPage, effectiveSize)
			})
		},
	}
	topics.Flags().IntVar(&topicsPage, "page", 1, "Page number")
	topics.Flags().IntVar(&topicsSize, "size", 25, "Items per page (max 25)")
	topics.Flags().IntVar(&topicsLimit, "limit", 0, "Limit returned items")
	commands = append(commands, topics)

	var frontlinePage, frontlineSize, frontlineLimit int
	frontline := &cobra.Command{
		Use:   "frontline",
		Short: "List 财新一线 flash news",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			effectiveSize, err := listLimit(cmd, frontlineSize, frontlineLimit)
			if err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				return c.Frontline(ctx, frontlinePage, effectiveSize)
			})
		},
	}
	frontline.Flags().IntVar(&frontlinePage, "page", 1, "Page number")
	frontline.Flags().IntVar(&frontlineSize, "size", 20, "Items per page (max 20)")
	frontline.Flags().IntVar(&frontlineLimit, "limit", 0, "Limit returned items")
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

	var bloggersPage, bloggersLimit int
	var bloggersSort string
	bloggers := &cobra.Command{
		Use:   "bloggers-directory",
		Short: "List one explicit page of the public blogger directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateClientSideLimit(cmd, bloggersLimit); err != nil {
				return err
			}
			return a.run(cmd, func(ctx context.Context, c *caixin.Client) (map[string]any, error) {
				result, err := c.BloggersDirectory(ctx, bloggersPage, bloggersSort)
				if err == nil && cmd.Flags().Changed("limit") {
					err = limitListResult(result, bloggersLimit)
				}
				return result, err
			})
		},
	}
	bloggers.Flags().IntVar(&bloggersPage, "page", 1, "Page number")
	bloggers.Flags().IntVar(&bloggersLimit, "limit", 0, "Limit returned items")
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
