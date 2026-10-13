// Package cli implements the command-line interface for macfor.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// Build information, set via ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Global flags.
var (
	verbose bool
	quiet   bool
)

// logger is the global logger instance configured by the root command.
var logger zerolog.Logger

// rootCmd is the base command for macfor.
var rootCmd *cobra.Command

// newRootCmd creates a new root command with all flags configured.
// This is used both for initialization and for testing.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "macfor",
		Short: "macOS Forensic Artifact Collection Tool",
		Long: `macfor collects forensic artifacts from macOS systems
using a plugin-based architecture for extensibility.

It supports collecting artifacts from live systems and disk images,
preserving evidence integrity through hashing and chain of custody
documentation.

Examples:
  # Collect all artifacts from the current system
  macfor collect --output ./evidence

  # Collect only shell history artifacts
  macfor collect --plugin shell.history --output ./evidence

  # List all available plugins
  macfor list plugins

  # Show collection plan without executing (dry run)
  macfor collect --dry-run --output ./evidence`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			configureLogger()
		},
		Run: func(cmd *cobra.Command, _ []string) {
			// Show help when no subcommand is provided.
			_ = cmd.Help()
		},
		// Silence Cobra's built-in error and usage printing since we handle it ourselves.
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// Global flags available to all commands.
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-error output")

	// Version flag handled by Cobra.
	cmd.Version = Version

	// Custom version template to include commit and build time.
	cmd.SetVersionTemplate(`macfor {{.Version}} (commit: ` + Commit + `, built: ` + BuildTime + `)
`)

	// Add subcommands.
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newCollectCmd())
	cmd.AddCommand(newManifestCmd())

	return cmd
}

// Execute runs the root command and returns an error if one occurred.
// Fatal errors are printed to stderr; a partial collection has already
// reported its errors, so it is not printed again.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil && !errors.Is(err, ErrPartialCollection) {
		_, _ = fmt.Fprintf(rootCmd.ErrOrStderr(), "Error: %v\n", err)
	}
	return err
}

// Logger returns the configured global logger.
func Logger() zerolog.Logger {
	return logger
}

// SetOutput sets the output writer for the root command (useful for testing).
func SetOutput(w io.Writer) {
	rootCmd.SetOut(w)
	rootCmd.SetErr(w)
}

// ResetFlags resets global flags to their default values (useful for testing).
func ResetFlags() {
	verbose = false
	quiet = false
}

//nolint:gochecknoinits // init is the standard Cobra pattern for CLI setup.
func init() {
	rootCmd = newRootCmd()
}

// ReinitRootCmd reinitializes the root command with fresh state.
// This is primarily useful for testing.
func ReinitRootCmd() {
	rootCmd = newRootCmd()
}

// configureLogger sets up the global zerolog logger based on verbosity flags.
func configureLogger() {
	var level zerolog.Level
	var output io.Writer

	switch {
	case quiet:
		level = zerolog.ErrorLevel
		output = os.Stderr
	case verbose:
		level = zerolog.DebugLevel
		output = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		}
	default:
		level = zerolog.InfoLevel
		output = zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
	}

	logger = zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Logger()
}

// Verbose returns true if verbose mode is enabled.
func Verbose() bool {
	return verbose
}

// Quiet returns true if quiet mode is enabled.
func Quiet() bool {
	return quiet
}
