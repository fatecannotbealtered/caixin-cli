package cmd

import (
	"errors"

	"github.com/fatecannotbealtered/caixin-cli/internal/caixin"
	"github.com/fatecannotbealtered/caixin-cli/internal/output"
	"github.com/spf13/cobra"
)

// Scan login needs a human holding a phone, so it is two commands, and neither
// blocks (CLI-SPEC §16.3). `login` mints the code and stops with
// E_HUMAN_REQUIRED (exit 9) naming the action and the resume command;
// `login-resume` checks once and returns whatever it found.
//
// The alternative -- one command that polls until the scan lands -- would hold
// an agent for minutes on a human's schedule and make the timeout the tool's
// choice instead of the caller's.
func (a *application) loginCommand() *cobra.Command {
	var imagePath string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Start a QR login; a human scans the image, then run login-resume",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, err := client.LoginStart(cmd.Context(), imagePath)
			if err != nil {
				return err
			}
			return output.NewError("E_HUMAN_REQUIRED",
				"a human must scan the QR code with the Caixin app", result)
		},
	}
	cmd.Flags().StringVar(&imagePath, "qr-image", "",
		"Where to write the QR image (default <state-dir>/login-qr.png)")
	return cmd
}

func (a *application) loginResumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login-resume",
		Short: "Check the outstanding QR login exactly once",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := a.client()
			if err != nil {
				return err
			}
			result, status, err := client.LoginResume(cmd.Context())
			switch {
			case errors.Is(err, caixin.ErrNoPendingLogin):
				return output.WrapError("E_VALIDATION", err.Error(), err, nil)
			case errors.Is(err, caixin.ErrLoginPending):
				// Still the human's turn. Reported as the same human-action code
				// so an agent's decision tree does not need a special case.
				return output.NewError("E_HUMAN_REQUIRED",
					"the QR code has not been confirmed yet", map[string]any{
						"status":       status,
						"resume":       "caixin-cli login-resume",
						"human_action": true,
					})
			case err != nil:
				return err
			}
			return a.success(result)
		},
	}
}
