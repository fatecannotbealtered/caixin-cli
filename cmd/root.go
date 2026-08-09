package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	caixincli "github.com/fatecannotbealtered/caixin-cli"
	"github.com/fatecannotbealtered/caixin-cli/internal/caixin"
	"github.com/fatecannotbealtered/caixin-cli/internal/output"
	"github.com/spf13/cobra"
)

var version = caixincli.Version

// SkillMinVersion is the tool version the bundled Skill was written against.
// `doctor` compares it so a Skill that expects newer commands fails loudly
// rather than calling something that does not exist (CLI-SPEC §14).
var SkillMinVersion = caixincli.Version

type application struct {
	in  io.Reader
	out io.Writer
	err io.Writer

	startedAt time.Time
	format    string
	jsonAlias bool
	compact   bool
	fields    []string
	quiet     bool
	// untrusted is the running command's declared untrusted field list.
	untrusted []string

	stateDir string
	timeout  time.Duration
}

// Execute is the process entry point.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	exit := ExecuteArgs(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(exit)
}

// ExecuteArgs runs one isolated invocation and returns its process exit code.
// Tests drive the CLI through this seam so they exercise the real command
// boundary rather than internal helpers.
func ExecuteArgs(ctx context.Context, args []string, in io.Reader, stdout, stderr io.Writer) int {
	app := &application{
		in:        in,
		out:       stdout,
		err:       stderr,
		startedAt: time.Now(),
		format:    output.FormatJSON,
	}
	root := app.rootCommand()
	root.SetArgs(args)
	root.SetIn(in)
	root.SetOut(stdout)
	root.SetErr(stderr)

	if err := root.ExecuteContext(ctx); err != nil {
		cliErr := asCLIError(err)
		if printErr := app.printer().Failure(cliErr); printErr != nil {
			_, _ = fmt.Fprintf(stderr, "failed to write error output: %v\n", printErr)
			return 1
		}
		// In text mode Failure() already wrote the human-readable line to
		// stdout; repeating it on stderr would just duplicate it.
		if !app.quiet && app.format == output.FormatJSON {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", cliErr.Code, cliErr.Message)
		}
		return cliErr.ExitCode()
	}
	return 0
}

func (a *application) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "caixin-cli",
		Short:         "Read-only Caixin (财新) access for AI agents",
		Version:       version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Resolved here, from the leaf command about to run, so the marker
			// on `data` is exactly the list `reference` declares for it.
			a.untrusted = untrustedFieldsFor(cmd.Name())
			return a.validateOutput(cmd)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.PersistentFlags().StringVar(&a.format, "format", output.FormatJSON, "Output format: json, text, or raw")
	root.PersistentFlags().BoolVar(&a.jsonAlias, "json", false, "Compatibility alias for --format json")
	root.PersistentFlags().BoolVar(&a.compact, "compact", false, "Emit compact JSON")
	root.PersistentFlags().StringSliceVar(&a.fields, "fields", nil, "Return only these top-level data fields")
	root.PersistentFlags().BoolVar(&a.quiet, "quiet", false, "Suppress non-error stderr diagnostics")
	root.PersistentFlags().StringVar(&a.stateDir, "state-dir", "", "Session directory (default $CAIXIN_STATE_DIR or ~/.caixin-fetch)")
	root.PersistentFlags().DurationVar(&a.timeout, "timeout", 0, "Upstream request timeout")

	for _, add := range []func() *cobra.Command{
		a.referenceCommand,
		a.contextCommand,
		a.doctorCommand,
		a.changelogCommand,
		a.statusCommand,
		a.entitlementsCommand,
		a.loginCommand,
		a.loginResumeCommand,
		a.logoutCommand,
		a.articleCommand,
		a.snapshotCommand,
		a.publicDirectoryCommand,
		a.esg30SubdirectoryCommand,
		a.sectionDirectoryCommand,
		a.issueCommand,
		a.blogAuthorCommand,
		a.cultureSectionCommand,
		a.cultureAuthorCommand,
		a.videoSectionCommand,
		a.topicCommand,
		a.micrositeCommand,
		a.datanewsInteractiveCommand,
		a.esg30ResourceCommand,
		a.opinionColumnsCommand,
		a.opinionUpfrontCommand,
		a.opinionAuthorDirectoryCommand,
		a.opinionAuthorCommand,
		a.updateCommand,
	} {
		root.AddCommand(add())
	}
	root.AddCommand(a.queryCommands()...)
	return root
}

// validateOutput resolves the output mode before any command runs.
//
// Every rejection here resets the format to json first: the caller asked for a
// mode that cannot be honored, so reporting the failure in that same
// unresolvable mode would deny the agent the machine contract exactly when it
// needs to understand what went wrong.
func (a *application) validateOutput(cmd *cobra.Command) error {
	usage := func(message string) error {
		// Only the format is forced back: it is the setting that could not be
		// honored. --compact is orthogonal and still meaningful on an error
		// envelope, and silently ignoring it would surprise a caller who asked
		// for compact output. --fields is dropped because a projection only
		// applies to success data.
		a.format = output.FormatJSON
		a.fields = nil
		return output.NewError("E_USAGE", message, nil)
	}

	if a.jsonAlias {
		formatFlag := cmd.Flags().Lookup("format")
		if formatFlag != nil && formatFlag.Changed && !strings.EqualFold(strings.TrimSpace(a.format), output.FormatJSON) {
			return usage("--json conflicts with a non-json --format")
		}
		a.format = output.FormatJSON
	}
	a.format = strings.ToLower(strings.TrimSpace(a.format))
	if a.format == "" {
		a.format = output.FormatJSON
	}
	switch a.format {
	case output.FormatJSON, output.FormatText:
	case output.FormatRaw:
		if cmd.Annotations["raw_output"] != "true" {
			return usage(cmd.CommandPath() + " does not support --format raw")
		}
	default:
		return usage("--format must be one of: json, text, raw")
	}
	if a.compact && a.format != output.FormatJSON {
		return usage("--compact requires --format json")
	}
	if len(a.fields) > 0 && a.format != output.FormatJSON {
		return usage("--fields requires --format json")
	}
	return nil
}

func (a *application) printer() *output.Printer {
	return output.NewPrinter(a.out, output.Options{
		Format:    a.format,
		Compact:   a.compact,
		Fields:    a.fields,
		StartedAt: a.startedAt,
		Untrusted: a.untrusted,
		Notices:   cachedNotices(),
	})
}

func (a *application) success(data any) error {
	if err := a.printer().Success(data); err != nil {
		if len(a.fields) > 0 {
			return output.WrapError("E_VALIDATION", "invalid --fields selection", err, nil)
		}
		return output.WrapError("E_UNKNOWN", "failed to encode command output", err, nil)
	}
	return nil
}

// cachedNotices reads any stored update advisory.
//
// Read-only, from a local file, never the network: CLI-SPEC §14 is explicit
// that business commands surface a cached notice but must not phone home to
// advertise updates. The cost is one file read; an absent or stale cache
// yields nothing and `meta.notices` is omitted.
func cachedNotices() []map[string]any {
	notice := updateConfig.ReadCachedNotice()
	if notice == nil {
		return nil
	}
	encoded, err := json.Marshal(notice)
	if err != nil {
		return nil
	}
	var asMap map[string]any
	if err := json.Unmarshal(encoded, &asMap); err != nil {
		return nil
	}
	return []map[string]any{asMap}
}

// baseURLOverride points the client at a mock upstream. It is unexported and
// set only by tests in this package, so it adds no public surface: production
// builds can never be steered at another host.
var baseURLOverride string

func (a *application) client() (*caixin.Client, error) {
	client, err := caixin.New(caixin.Options{
		StateDir: a.stateDir,
		Timeout:  a.timeout,
		BaseHost: baseURLOverride,
	})
	if err != nil {
		return nil, output.WrapError("E_CONFIG", "could not initialize the Caixin client", err, nil)
	}
	return client, nil
}

// asCLIError classifies a failure by TYPE, never by sniffing message text: a
// response body containing the words "not found" must not become E_NOT_FOUND
// (CLI-SPEC §6).
func asCLIError(err error) *output.CLIError {
	var cliErr *output.CLIError
	if errors.As(err, &cliErr) {
		return cliErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return output.WrapError("E_TIMEOUT", "operation timed out", err, nil)
	}
	if errors.Is(err, context.Canceled) {
		return output.WrapError("E_INTERRUPTED", "operation was interrupted", err, nil)
	}

	if errors.Is(err, caixin.ErrNoBrowser) {
		return output.WrapError("E_CONFIG", err.Error(), err, map[string]any{
			"install": "install Google Chrome or Microsoft Edge, or set CAIXIN_BROWSER to its path",
		})
	}

	var validationErr *caixin.ValidationError
	if errors.As(err, &validationErr) {
		return output.WrapError("E_VALIDATION", validationErr.Error(), err, validationErr.Details)
	}

	var apiErr *caixin.APIError
	if errors.As(err, &apiErr) {
		details := map[string]any{}
		code := "E_SERVER"
		if apiErr.StatusCode > 0 {
			details["status_code"] = apiErr.StatusCode
			code = output.CodeForStatus(apiErr.StatusCode)
		} else if apiErr.Code != nil {
			details["business_code"] = apiErr.Code
			code = "E_SERVER"
		}
		return output.WrapError(code, apiErr.Error(), err, details)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return output.WrapError("E_TIMEOUT", "upstream request timed out", err, nil)
		}
		return output.WrapError("E_NETWORK", "upstream network request failed", err, nil)
	}
	return output.WrapError("E_USAGE", err.Error(), err, nil)
}
