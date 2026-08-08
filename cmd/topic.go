package cmd

import "github.com/spf13/cobra"

// topicCommand reads one Caixin topic page.
//
// Three different products publish under the word "topic": deepview builds its
// page from a layout config, key.caixin.com serves tabs from an entity API. The
// url decides which, so a caller does not have to know.
func (a *application) topicCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "topic <url>",
		Short: "List one Caixin topic page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.Topic(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return a.success(result)
		},
	}
}
