package shell

import (
	"os"
	"path/filepath"
	"runtime"
)

// testdataDir returns the absolute path to the testdata/shell directory.
// It uses runtime.Caller to find the package directory regardless of
// where tests are run from.
func testdataDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("could not get caller info")
	}
	// filename is the path to this file (testdata.go)
	// Go up to the project root, then into testdata/shell
	pkgDir := filepath.Dir(filename)
	return filepath.Join(pkgDir, "..", "..", "testdata", "shell")
}

// loadFixture reads a test fixture file from testdata/shell and returns its contents.
// The name parameter is the filename without path (e.g., "bash_history_basic").
// Returns an error if the file cannot be read.
func loadFixture(name string) (string, error) {
	path := filepath.Join(testdataDir(), name)
	data, err := os.ReadFile(path) //nolint:gosec // Only used in tests with known fixture names
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// mustLoadFixture reads a test fixture file and panics if it cannot be read.
// Use this in tests where a missing fixture indicates a test setup problem.
func mustLoadFixture(name string) string {
	content, err := loadFixture(name)
	if err != nil {
		panic("failed to load fixture " + name + ": " + err.Error())
	}
	return content
}

// fixtureExists checks if a fixture file exists.
func fixtureExists(name string) bool {
	path := filepath.Join(testdataDir(), name)
	_, err := os.Stat(path)
	return err == nil
}

// listFixtures returns a list of all fixture files in testdata/shell.
func listFixtures() ([]string, error) {
	dir := testdataDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// Fixture names for easy reference in tests (bash history files).
const (
	FixtureBashBasic       = "bash_history_basic"
	FixtureBashTimestamped = "bash_history_timestamped"
	FixtureBashMixed       = "bash_history_mixed"
	FixtureBashUnicode     = "bash_history_unicode"
	FixtureBashSpecial     = "bash_history_special"
	FixtureBashEmpty       = "bash_history_empty"
	FixtureBashLongCmd     = "bash_history_longcmd"
)

// Fixture names for zsh history files.
const (
	FixtureZshExtended  = "zsh_history_extended"
	FixtureZshBasic     = "zsh_history_basic"
	FixtureZshMultiline = "zsh_history_multiline"
	FixtureZshUnicode   = "zsh_history_unicode"
	FixtureZshSpecial   = "zsh_history_special"
	FixtureZshEmpty     = "zsh_history_empty"
	FixtureZshMixed     = "zsh_history_mixed"
	FixtureZshLongCmd   = "zsh_history_longcmd"
)

// Fixture names for fish history files.
const (
	FixtureFish        = "fish_history"
	FixtureFishLongCmd = "fish_history_longcmd"
)

// Fixture names for sh history files.
const (
	FixtureShBasic   = "sh_history_basic"
	FixtureShUnicode = "sh_history_unicode"
	FixtureShSpecial = "sh_history_special"
)

// Fixture names for edge case files.
const (
	FixtureEmpty         = "empty_history"
	FixtureBinaryGarbage = "binary_garbage"
)
