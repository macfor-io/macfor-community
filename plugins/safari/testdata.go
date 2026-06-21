package safari

import (
	"os"
	"path/filepath"
	"runtime"
)

// testdataDir returns the absolute path to the testdata/safari directory.
// It uses runtime.Caller to find the package directory regardless of
// where tests are run from.
func testdataDir() string {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("could not get caller info")
	}
	// filename is the path to this file (testdata.go)
	// Go up to the project root, then into testdata/safari
	pkgDir := filepath.Dir(filename)
	return filepath.Join(pkgDir, "..", "..", "testdata", "safari")
}

// loadFixture reads a test fixture file from testdata/safari and returns its contents.
// The name parameter is the filename without path (e.g., "History.db").
// Returns an error if the file cannot be read.
func loadFixture(name string) ([]byte, error) {
	path := filepath.Join(testdataDir(), name)
	return os.ReadFile(path) //nolint:gosec // Only used in tests with known fixture names
}

// mustLoadFixture reads a test fixture file and panics if it cannot be read.
// Use this in tests where a missing fixture indicates a test setup problem.
func mustLoadFixture(name string) []byte {
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

// fixturePath returns the absolute path to a fixture file.
// This is useful when a test needs to pass a file path to a function
// rather than loading the file contents.
func fixturePath(name string) string {
	return filepath.Join(testdataDir(), name)
}

// listFixtures returns a list of all fixture files in testdata/safari
// (excluding directories).
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

// listLocalStorageFixtures returns a list of all fixture files in
// testdata/safari/LocalStorage.
func listLocalStorageFixtures() ([]string, error) {
	dir := filepath.Join(testdataDir(), "LocalStorage")
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

// Fixture names for History.db files.
const (
	// FixtureHistoryDB is the main History.db with sample browsing data.
	// Contains 8 history items and 14 visits including various domains
	// (github.com, google.com, stackoverflow.com, etc.), different visit
	// counts, and one failed load.
	FixtureHistoryDB = "History.db"

	// FixtureHistoryDBEmpty is an empty History.db with schema only.
	FixtureHistoryDBEmpty = "History_empty.db"

	// FixtureHistoryDBUnicode contains URLs and titles with Unicode characters.
	// Includes Japanese, Chinese, Russian, emoji, Arabic, and Hebrew.
	FixtureHistoryDBUnicode = "History_unicode.db"

	// FixtureHistoryDBLarge contains 1000 history items for performance testing.
	FixtureHistoryDBLarge = "History_large.db"
)

// Fixture names for Downloads.plist files.
const (
	// FixtureDownloadsPlist contains sample downloads with various states:
	// completed download (claude-code zip file), completed download with
	// RemoveWhenDone flag (Go installer), and incomplete download (large
	// ISO file, partial progress).
	FixtureDownloadsPlist = "Downloads.plist"

	// FixtureDownloadsPlistEmpty is an empty downloads plist (empty array).
	FixtureDownloadsPlistEmpty = "Downloads_empty.plist"
)

// Fixture names for Bookmarks.plist files.
const (
	// FixtureBookmarksPlist contains a nested bookmark structure with
	// BookmarksBar folder (Work subfolder with Research nested folder),
	// BookmarksMenu folder, and Reading List with 2 items.
	FixtureBookmarksPlist = "Bookmarks.plist"

	// FixtureBookmarksPlistEmpty is a bookmarks plist with empty Children array.
	FixtureBookmarksPlistEmpty = "Bookmarks_empty.plist"
)

// Fixture names for Cookies.binarycookies files.
const (
	// FixtureCookiesBinary contains sample cookies from various domains:
	// .github.com with Secure+HttpOnly flags, .google.com with Secure
	// flag only, and stackoverflow.com with no flags.
	FixtureCookiesBinary = "Cookies.binarycookies"

	// FixtureCookiesBinaryEmpty is an empty cookies file (0 pages).
	FixtureCookiesBinaryEmpty = "Cookies_empty.binarycookies"
)

// Fixture names for Extensions.plist files.
const (
	// FixtureExtensionsPlist contains sample extension data: 1Password
	// extension (enabled), AdBlock extension (enabled), and Example
	// extension (disabled).
	FixtureExtensionsPlist = "Extensions.plist"
)

// Fixture names for LastSession.plist files.
const (
	// FixtureLastSessionPlist contains session state with Window 1 (3 tabs:
	// GitHub, Hacker News, Stack Overflow) and Window 2 (1 tab: Go Packages).
	FixtureLastSessionPlist = "LastSession.plist"
)

// Fixture names for TopSites.plist files.
const (
	// FixtureTopSitesPlist contains top sites: GitHub, Google (built-in),
	// Stack Overflow, and Hacker News.
	FixtureTopSitesPlist = "TopSites.plist"
)

// Fixture names for LocalStorage files.
const (
	// FixtureLocalStorageDir is the LocalStorage subdirectory.
	FixtureLocalStorageDir = "LocalStorage"

	// FixtureLocalStorageGitHub contains local storage for github.com.
	FixtureLocalStorageGitHub = "LocalStorage/https_github.com_0.localstorage"

	// FixtureLocalStorageExample contains local storage for example.com.
	FixtureLocalStorageExample = "LocalStorage/https_example.com_0.localstorage"
)
