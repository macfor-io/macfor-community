package macostree_test

import (
	"os"
	"path/filepath"
	"testing"

	"howett.net/plist"

	"github.com/macfor-io/macfor-community/testdata/macostree"
)

func TestBuild_CreatesSystemVersionPlist(t *testing.T) {
	opts := macostree.Defaults()
	opts.OSVersion = "15.0"
	opts.OSBuild = "24A335"

	root := macostree.MustBuild(t, opts)

	path := filepath.Join(root, "System", "Library", "CoreServices", "SystemVersion.plist")
	assertFileExists(t, path)

	var data struct {
		ProductVersion      string `plist:"ProductVersion"`
		ProductBuildVersion string `plist:"ProductBuildVersion"`
		ProductName         string `plist:"ProductName"`
	}
	readPlist(t, path, &data)

	assertEqual(t, "ProductVersion", "15.0", data.ProductVersion)
	assertEqual(t, "ProductBuildVersion", "24A335", data.ProductBuildVersion)
	assertEqual(t, "ProductName", "macOS", data.ProductName)
}

func TestBuild_CreatesPreferencesPlist(t *testing.T) {
	opts := macostree.Defaults()
	opts.ComputerName = "Test Mac"
	opts.Hostname = "test-mac.local"
	opts.LocalHostname = "Test-Mac"

	root := macostree.MustBuild(t, opts)

	path := filepath.Join(root, "Library", "Preferences", "SystemConfiguration", "preferences.plist")
	assertFileExists(t, path)

	var data struct {
		ComputerName  string `plist:"ComputerName"`
		HostName      string `plist:"HostName"`
		LocalHostName string `plist:"LocalHostName"`
	}
	readPlist(t, path, &data)

	assertEqual(t, "ComputerName", "Test Mac", data.ComputerName)
	assertEqual(t, "HostName", "test-mac.local", data.HostName)
	assertEqual(t, "LocalHostName", "Test-Mac", data.LocalHostName)
}

func TestBuild_CreatesUserHomeDir(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "bob", UID: 502, GID: 20, Name: "Bob Jones", Shell: "/bin/bash"},
	}

	root := macostree.MustBuild(t, opts)

	assertDirExists(t, filepath.Join(root, "Users", "bob"))
}

func TestBuild_CreatesDSLocalPlist(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "carol", UID: 503, GID: 80, Name: "Carol White", Shell: "/bin/zsh"},
	}

	root := macostree.MustBuild(t, opts)

	path := filepath.Join(root, "private", "var", "db", "dslocal", "nodes", "Default", "users", "carol.plist")
	assertFileExists(t, path)

	var data struct {
		Name     []string `plist:"name"`
		RealName []string `plist:"realname"`
		UID      []string `plist:"uid"`
		GID      []string `plist:"gid"`
		Home     []string `plist:"home"`
		Shell    []string `plist:"shell"`
	}
	readPlist(t, path, &data)

	assertSliceEqual(t, "name", []string{"carol"}, data.Name)
	assertSliceEqual(t, "realname", []string{"Carol White"}, data.RealName)
	assertSliceEqual(t, "uid", []string{"503"}, data.UID)
	assertSliceEqual(t, "gid", []string{"80"}, data.GID)
	assertSliceEqual(t, "home", []string{"/Users/carol"}, data.Home)
	assertSliceEqual(t, "shell", []string{"/bin/zsh"}, data.Shell)
}

func TestBuild_CreatesSharedDir(t *testing.T) {
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	assertDirExists(t, filepath.Join(root, "Users", "Shared"))
}

func TestBuild_MultipleUsers(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "alice", UID: 501, GID: 20, Name: "Alice Smith", Shell: "/bin/zsh"},
		{Username: "bob", UID: 502, GID: 20, Name: "Bob Jones", Shell: "/bin/bash"},
		{Username: "carol", UID: 503, GID: 80, Name: "Carol White", Shell: "/bin/fish"},
	}

	root := macostree.MustBuild(t, opts)

	for _, user := range opts.Users {
		assertDirExists(t, filepath.Join(root, "Users", user.Username))

		dsPath := filepath.Join(root, "private", "var", "db", "dslocal", "nodes", "Default", "users", user.Username+".plist")
		assertFileExists(t, dsPath)
	}
}

func TestDefaults_ReturnsValidOptions(t *testing.T) {
	opts := macostree.Defaults()

	if opts.OSVersion == "" {
		t.Error("OSVersion should not be empty")
	}
	if opts.OSBuild == "" {
		t.Error("OSBuild should not be empty")
	}
	if opts.ComputerName == "" {
		t.Error("ComputerName should not be empty")
	}
	if opts.Hostname == "" {
		t.Error("Hostname should not be empty")
	}
	if opts.LocalHostname == "" {
		t.Error("LocalHostname should not be empty")
	}
	if len(opts.Users) == 0 {
		t.Fatal("Users should not be empty")
	}

	user := opts.Users[0]
	if user.Username == "" {
		t.Error("Username should not be empty")
	}
	if user.UID == 0 {
		t.Error("UID should not be zero")
	}
	if user.Name == "" {
		t.Error("Name should not be empty")
	}
	if user.Shell == "" {
		t.Error("Shell should not be empty")
	}
}

func TestMustBuild_ReturnsPath(t *testing.T) {
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	if root == "" {
		t.Fatal("MustBuild returned empty path")
	}

	info, err := os.Stat(root)
	if err != nil {
		t.Fatalf("MustBuild returned non-existent path: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("MustBuild returned path that is not a directory")
	}
}

// Test helpers.

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file to exist at %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected file at %s, got directory", path)
	}
}

func assertDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected directory to exist at %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected directory at %s, got file", path)
	}
}

func readPlist(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // Test helper reads fixture files.
	if err != nil {
		t.Fatalf("reading plist %s: %v", path, err)
	}
	if _, err := plist.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshaling plist %s: %v", path, err)
	}
}

func assertEqual(t *testing.T, field, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %q, got %q", field, expected, actual)
	}
}

func assertSliceEqual(t *testing.T, field string, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Errorf("%s: expected %d elements, got %d", field, len(expected), len(actual))
		return
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("%s[%d]: expected %q, got %q", field, i, expected[i], actual[i])
		}
	}
}
