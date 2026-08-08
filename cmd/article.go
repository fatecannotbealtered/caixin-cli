package cmd

import (
	"context"

	"github.com/fatecannotbealtered/caixin-cli/internal/caixin"
	"github.com/spf13/cobra"
)

// articleCommand reads one Caixin article.
//
// What comes back depends on the session's entitlement, and the payload says
// which: `entitled` and `complete` are reported rather than inferred from the
// text being non-empty, so a teaser is never mistaken for a full article.
func (a *application) articleCommand() *cobra.Command {
	var full bool
	var browserWS string
	cmd := &cobra.Command{
		Use:   "article <url>",
		Short: "Read one Caixin article; --full fetches the complete body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			var signer *caixin.Signer
			if full {
				signer = caixin.NewSigner(client.StateDirectory(), a.timeout)
				if browserWS != "" {
					signer.RemoteWS = browserWS
				}
			}
			result, err := client.Article(cmd.Context(), args[0], signer)
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
	cmd.Flags().BoolVar(&full, "full", false,
		"Fetch the complete body over the signed endpoint (no browser needed once "+
			"the signing key is cached; see docs/FULL-TEXT.md)")
	cmd.Flags().StringVar(&browserWS, "browser-ws", "",
		"DevTools websocket of a running browser, used only to extract the signing "+
			"key on a host with no local browser and no CAIXIN_SIGNING_KEY")
	return cmd
}

var _ = context.Background
