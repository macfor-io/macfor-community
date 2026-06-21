package cli

import (
	"bytes"
	"strings"
	"testing"
)

// resetTestState ensures clean state for each test by reinitializing the root command.
func resetTestState() {
	ResetFlags()
	ReinitRootCmd()
}

func TestRootCommand_Help(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Simulate running "macfor" (no args shows help).
	rootCmd.SetArgs([]string{})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify key elements are present in help output.
	// Note: Cobra shows Long description, not Short in the main help output.
	expectedStrings := []string{
		"macfor",
		"forensic artifacts",
		"plugin-based architecture",
		"-v, --verbose",
		"-q, --quiet",
		"-h, --help",
		"--version",
		"Examples:",
		"macfor collect --output",
		"macfor list plugins",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("help output missing expected string: %q\noutput: %s", expected, output)
		}
	}
}

func TestRootCommand_HelpFlag(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Simulate running "macfor --help".
	rootCmd.SetArgs([]string{"--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify help output.
	if !strings.Contains(output, "macfor") {
		t.Error("--help should show macfor in output")
	}
	if !strings.Contains(output, "--verbose") {
		t.Error("--help should show --verbose flag")
	}
}

func TestRootCommand_Version(t *testing.T) {
	resetTestState()

	// Save original values.
	origVersion := Version
	origCommit := Commit
	origBuildTime := BuildTime

	// Set test version info.
	Version = "1.2.3"
	Commit = "abc123"
	BuildTime = "2024-01-15T10:00:00Z"

	// Restore after test.
	defer func() {
		Version = origVersion
		Commit = origCommit
		BuildTime = origBuildTime
		// Restore version template.
		rootCmd.Version = origVersion
		rootCmd.SetVersionTemplate(`macfor {{.Version}} (commit: ` + origCommit + `, built: ` + origBuildTime + `)
`)
	}()

	// Re-initialize version template with test values.
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(`macfor {{.Version}} (commit: ` + Commit + `, built: ` + BuildTime + `)
`)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Simulate running "macfor --version".
	rootCmd.SetArgs([]string{"--version"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	expectedStrings := []string{
		"macfor 1.2.3",
		"commit: abc123",
		"built: 2024-01-15T10:00:00Z",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("version output missing expected string: %q\ngot: %s", expected, output)
		}
	}
}

func TestRootCommand_VerboseFlag(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Verify verbose is false by default.
	if Verbose() {
		t.Error("verbose should be false by default")
	}

	// Parse with verbose flag.
	rootCmd.SetArgs([]string{"--verbose"})
	_ = rootCmd.Execute()

	if !Verbose() {
		t.Error("verbose should be true after --verbose flag")
	}
}

func TestRootCommand_QuietFlag(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Verify quiet is false by default.
	if Quiet() {
		t.Error("quiet should be false by default")
	}

	// Parse with quiet flag.
	rootCmd.SetArgs([]string{"--quiet"})
	_ = rootCmd.Execute()

	if !Quiet() {
		t.Error("quiet should be true after --quiet flag")
	}
}

func TestRootCommand_ShortFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		checkFn  func() bool
		expected bool
	}{
		{
			name:     "short verbose flag -v",
			args:     []string{"-v"},
			checkFn:  Verbose,
			expected: true,
		},
		{
			name:     "short quiet flag -q",
			args:     []string{"-q"},
			checkFn:  Quiet,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestState()

			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetErr(&buf)

			rootCmd.SetArgs(tt.args)
			_ = rootCmd.Execute()

			if got := tt.checkFn(); got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestConfigureLogger(t *testing.T) {
	tests := []struct {
		name          string
		verbose       bool
		quiet         bool
		expectedLevel string
	}{
		{
			name:          "default level is info",
			verbose:       false,
			quiet:         false,
			expectedLevel: "info",
		},
		{
			name:          "verbose enables debug",
			verbose:       true,
			quiet:         false,
			expectedLevel: "debug",
		},
		{
			name:          "quiet sets error level",
			verbose:       false,
			quiet:         true,
			expectedLevel: "error",
		},
		{
			name:          "quiet takes precedence over verbose",
			verbose:       true,
			quiet:         true,
			expectedLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTestState()

			// Set flags directly for testing.
			verbose = tt.verbose
			quiet = tt.quiet

			configureLogger()

			// Verify logger was configured by checking it's not nil.
			log := Logger()
			if log.GetLevel().String() != tt.expectedLevel {
				t.Errorf("expected log level %s, got %s", tt.expectedLevel, log.GetLevel().String())
			}
		})
	}
}

func TestResetFlags(t *testing.T) {
	// Set flags to non-default values.
	verbose = true
	quiet = true

	// Reset.
	ResetFlags()

	if verbose {
		t.Error("verbose should be false after reset")
	}
	if quiet {
		t.Error("quiet should be false after reset")
	}
}

func TestExecute(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{})

	err := Execute()
	if err != nil {
		t.Errorf("Execute() should not return error, got: %v", err)
	}
}
