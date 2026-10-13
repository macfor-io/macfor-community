// Package macostree creates synthetic macOS filesystem trees for use in tests.
// The generated trees mirror the structure of a real macOS system root,
// with realistic plist files and user home directories.
package macostree

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"howett.net/plist"
)

// Options controls what the generated tree contains.
type Options struct {
	// OSVersion is the macOS version string (e.g., "14.2.1").
	OSVersion string
	// OSBuild is the macOS build string (e.g., "23C71").
	OSBuild string
	// ComputerName is the display name of the computer.
	ComputerName string
	// Hostname is the network hostname.
	Hostname string
	// LocalHostname is the Bonjour/local network hostname.
	LocalHostname string
	// Users is the list of users to create home directories for.
	Users []UserOptions
}

// UserOptions controls a single synthetic user.
type UserOptions struct {
	// Username is the login name (e.g., "alice").
	Username string
	// UID is the numeric user ID.
	UID int
	// GID is the primary group ID.
	GID int
	// Name is the full display name.
	Name string
	// Shell is the login shell path.
	Shell string
	// ZshHistory, if set, is written to the user's ~/.zsh_history.
	ZshHistory string
}

// Defaults returns sensible default options for a Sonoma system with one user.
func Defaults() Options {
	return Options{
		OSVersion:     "14.2.1",
		OSBuild:       "23C71",
		ComputerName:  "Alice's MacBook Pro",
		Hostname:      "alices-mbp.local",
		LocalHostname: "Alices-MacBook-Pro",
		Users: []UserOptions{
			{
				Username: "alice",
				UID:      501,
				GID:      20,
				Name:     "Alice Smith",
				Shell:    "/bin/zsh",
			},
		},
	}
}

// Build creates a synthetic macOS filesystem tree rooted at root.
// The caller is responsible for cleaning up the directory when done.
func Build(root string, opts Options) error {
	if err := writeSystemVersionPlist(root, opts); err != nil {
		return err
	}
	if err := writePreferencesPlist(root, opts); err != nil {
		return err
	}
	if err := writeSharedDir(root); err != nil {
		return err
	}
	for _, user := range opts.Users {
		if err := writeUserHomeDir(root, user); err != nil {
			return err
		}
		if err := writeDSLocalPlist(root, user); err != nil {
			return err
		}
	}
	return nil
}

// MustBuild creates a synthetic macOS filesystem tree in t.TempDir() and returns the path.
// It fails the test immediately if the tree cannot be created.
func MustBuild(t testing.TB, opts Options) string {
	t.Helper()
	root := t.TempDir()
	if err := Build(root, opts); err != nil {
		t.Fatalf("macostree.Build: %v", err)
	}
	return root
}

// systemVersionData represents the SystemVersion.plist structure.
type systemVersionData struct {
	ProductBuildVersion string `plist:"ProductBuildVersion"`
	ProductName         string `plist:"ProductName"`
	ProductVersion      string `plist:"ProductVersion"`
}

// preferencesData represents the SystemConfiguration/preferences.plist structure.
type preferencesData struct {
	ComputerName  string `plist:"ComputerName"`
	HostName      string `plist:"HostName"`
	LocalHostName string `plist:"LocalHostName"`
}

// dsLocalUserData represents the dslocal user plist structure.
// All values are string arrays to match the real dslocal format.
type dsLocalUserData struct {
	GID      []string `plist:"gid"`
	Home     []string `plist:"home"`
	Name     []string `plist:"name"`
	RealName []string `plist:"realname"`
	Shell    []string `plist:"shell"`
	UID      []string `plist:"uid"`
}

func writeSystemVersionPlist(root string, opts Options) error {
	dir := filepath.Join(root, "System", "Library", "CoreServices")
	if err := mkdirAll(dir); err != nil {
		return err
	}
	data := systemVersionData{
		ProductBuildVersion: opts.OSBuild,
		ProductName:         "macOS",
		ProductVersion:      opts.OSVersion,
	}
	return writePlist(filepath.Join(dir, "SystemVersion.plist"), data)
}

func writePreferencesPlist(root string, opts Options) error {
	dir := filepath.Join(root, "Library", "Preferences", "SystemConfiguration")
	if err := mkdirAll(dir); err != nil {
		return err
	}
	data := preferencesData{
		ComputerName:  opts.ComputerName,
		HostName:      opts.Hostname,
		LocalHostName: opts.LocalHostname,
	}
	return writePlist(filepath.Join(dir, "preferences.plist"), data)
}

func writeSharedDir(root string) error {
	return mkdirAll(filepath.Join(root, "Users", "Shared"))
}

func writeUserHomeDir(root string, user UserOptions) error {
	home := filepath.Join(root, "Users", user.Username)
	if err := mkdirAll(home); err != nil {
		return err
	}
	if user.ZshHistory == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(user.ZshHistory), 0o600)
}

func writeDSLocalPlist(root string, user UserOptions) error {
	dir := filepath.Join(root, "private", "var", "db", "dslocal", "nodes", "Default", "users")
	if err := mkdirAll(dir); err != nil {
		return err
	}
	data := dsLocalUserData{
		GID:      []string{strconv.Itoa(user.GID)},
		Home:     []string{"/Users/" + user.Username},
		Name:     []string{user.Username},
		RealName: []string{user.Name},
		Shell:    []string{user.Shell},
		UID:      []string{strconv.Itoa(user.UID)},
	}
	return writePlist(filepath.Join(dir, user.Username+".plist"), data)
}

func writePlist(path string, v any) error {
	b, err := plist.MarshalIndent(v, plist.XMLFormat, "\t")
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, b, 0644) //nolint:gosec
}

func mkdirAll(path string) error {
	return os.MkdirAll(path, 0755) //nolint:gosec
}
