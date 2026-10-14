// Package main is the entry point for the macfor CLI.
package main

import (
	"os"

	"github.com/macfor-io/macfor-community/pkg/cli"

	// Import plugins to register them via init().
	_ "github.com/macfor-io/macfor-community/plugins/safari"
	_ "github.com/macfor-io/macfor-community/plugins/shell"
	_ "github.com/macfor-io/macfor-community/plugins/users"

	// Import schemas to register Parquet schema entries via init().
	_ "github.com/macfor-io/macfor-community/internal/schemas"
)

// Version information, set by ldflags during build.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Set build information in the CLI package.
	cli.Version = version
	cli.Commit = commit
	cli.BuildTime = buildTime

	os.Exit(cli.ExitCode(cli.Execute()))
}
