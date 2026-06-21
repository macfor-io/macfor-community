package directory

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

func TestNew_ValidDir(t *testing.T) {
	dir := t.TempDir()

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New(%q) error = %v", dir, err)
	}
	if src == nil {
		t.Fatal("New() returned nil")
	}
	if src.root != dir {
		t.Errorf("root = %q, want %q", src.root, dir)
	}
}

func TestNew_NonExistentDir(t *testing.T) {
	_, err := New("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("New() expected error for non-existent directory")
	}
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got %v", err)
	}
}

func TestNew_File(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := New(filePath)
	if err == nil {
		t.Fatal("New() expected error for file path")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should mention 'not a directory', got %v", err)
	}
}

func TestType(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := src.Type(); got != macfor.SourceTypeDirectory {
		t.Errorf("Type() = %v, want %v", got, macfor.SourceTypeDirectory)
	}
}

func TestOpen_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello forensics")
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	r, err := src.Open("/test.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = r.Close() }()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content = %q, want %q", data, content)
	}
}

func TestOpen_MissingFile(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = src.Open("/nonexistent.txt")
	if err == nil {
		t.Fatal("Open() expected error for non-existent file")
	}
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got %v", err)
	}
}

func TestPathTraversal(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "dotdot prefix", path: "../../etc/passwd"},
		{name: "dotdot in middle", path: "/foo/../../etc/passwd"},
		{name: "absolute dotdot", path: "/../../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := src.Open(tt.path)
			if err == nil {
				t.Fatal("Open() expected error for path traversal")
			}
			if !errors.Is(err, macfor.ErrPermissionDenied) {
				t.Errorf("error should wrap ErrPermissionDenied, got %v", err)
			}
		})
	}
}

func TestGlob_AbsolutePattern(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	for _, name := range []string{"a.txt", "b.txt", "c.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	matches, err := src.Glob("/*.txt")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if len(matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2, got %v", len(matches), matches)
	}

	sort.Strings(matches)
	if matches[0] != "/a.txt" || matches[1] != "/b.txt" {
		t.Errorf("matches = %v, want [/a.txt /b.txt]", matches)
	}
}

func TestGlob_NoMatches(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	matches, err := src.Glob("/*.xyz")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0", len(matches))
	}
}

func TestGlob_NestedPattern(t *testing.T) {
	dir := t.TempDir()

	// Create nested structure.
	subDir := filepath.Join(dir, "Users", "admin", "Library")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "data.db"), []byte("db"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	matches, err := src.Glob("/Users/*/Library/*.db")
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("len(matches) = %d, want 1", len(matches))
	}
	if matches[0] != "/Users/admin/Library/data.db" {
		t.Errorf("matches[0] = %q, want /Users/admin/Library/data.db", matches[0])
	}
}

func TestWalk_PathsAreAbsolute(t *testing.T) {
	dir := t.TempDir()

	// Create a directory structure.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	subDir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("b"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var visited []string
	err = src.Walk("/", func(path string, _ macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited = append(visited, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}

	// Should visit: /, /a.txt, /subdir, /subdir/b.txt.
	if len(visited) != 4 {
		t.Errorf("len(visited) = %d, want 4, got %v", len(visited), visited)
	}

	for _, p := range visited {
		if !strings.HasPrefix(p, "/") {
			t.Errorf("path %q does not start with /", p)
		}
	}
}

func TestWalk_FileInfoPopulated(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = src.Walk("/", func(path string, info macfor.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info == nil {
			t.Errorf("info is nil for path %q", path)
			return nil
		}
		// On macOS, forensic timestamps should be populated for files.
		if !info.IsDir() && info.AccessTime().IsZero() {
			t.Errorf("AccessTime() for %s should not be zero", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk() error = %v", err)
	}
}

func TestReadDir(t *testing.T) {
	dir := t.TempDir()

	// Create test files and a subdirectory.
	files := []string{"a.txt", "b.txt", "c.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("content"), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("list directory", func(t *testing.T) {
		entries, err := src.ReadDir("/")
		if err != nil {
			t.Fatalf("ReadDir() error = %v", err)
		}

		if len(entries) != 4 { // 3 files + 1 subdir.
			t.Errorf("len(entries) = %d, want 4", len(entries))
		}

		names := make(map[string]bool)
		for _, e := range entries {
			names[e.Name()] = true
		}

		for _, f := range files {
			if !names[f] {
				t.Errorf("expected to find %s in entries", f)
			}
		}
		if !names["subdir"] {
			t.Error("expected to find subdir in entries")
		}
	})

	t.Run("non-existent directory", func(t *testing.T) {
		_, err := src.ReadDir("/nonexistent")
		if err == nil {
			t.Fatal("ReadDir() expected error for non-existent directory")
		}
		if !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("error should wrap ErrNotFound, got %v", err)
		}
	})
}

func TestStat(t *testing.T) {
	dir := t.TempDir()
	content := []byte("hello forensics")
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("existing file", func(t *testing.T) {
		info, err := src.Stat("/test.txt")
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}

		if info.Name() != "test.txt" {
			t.Errorf("Name() = %v, want test.txt", info.Name())
		}
		if info.Size() != int64(len(content)) {
			t.Errorf("Size() = %v, want %v", info.Size(), len(content))
		}
		if info.IsDir() {
			t.Error("IsDir() = true, want false")
		}
	})

	t.Run("directory", func(t *testing.T) {
		info, err := src.Stat("/")
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := src.Stat("/nonexistent.txt")
		if err == nil {
			t.Fatal("Stat() expected error for non-existent file")
		}
		if !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("error should wrap ErrNotFound, got %v", err)
		}
	})
}

func TestStat_ForensicTimestamps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	info, err := src.Stat("/test.txt")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	// AccessTime and ChangeTime should be populated on both macOS and Linux.
	if info.AccessTime().IsZero() {
		t.Error("AccessTime() should not be zero")
	}
	if info.ChangeTime().IsZero() {
		t.Error("ChangeTime() should not be zero")
	}
}

func TestResolvePath(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "absolute path unchanged",
			path: "/etc/passwd",
			want: "/etc/passwd",
		},
		{
			name: "relative path unchanged",
			path: "foo/bar",
			want: "foo/bar",
		},
		{
			name: "tilde without users returns unchanged",
			path: "~/Documents",
			want: "~/Documents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := src.ResolvePath(tt.path)
			if got != tt.want {
				t.Errorf("ResolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestSystemInfo_Stub(t *testing.T) {
	dir := t.TempDir()
	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}

	if info.OSName != "macOS" {
		t.Errorf("OSName = %q, want macOS", info.OSName)
	}
	if info.SourceType != macfor.SourceTypeDirectory {
		t.Errorf("SourceType = %q, want %q", info.SourceType, macfor.SourceTypeDirectory)
	}
	if info.ImagePath != dir {
		t.Errorf("ImagePath = %q, want %q", info.ImagePath, dir)
	}
	if info.CollectionTime.IsZero() {
		t.Error("CollectionTime should not be zero")
	}
}

func TestOpen_WithoutLeadingSlash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Should also work without leading slash.
	r, err := src.Open("test.txt")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	_ = r.Close()
}

func TestReadPlist(t *testing.T) {
	dir := t.TempDir()

	// Create a simple XML plist file.
	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ProductName</key>
	<string>macOS</string>
	<key>ProductVersion</key>
	<string>14.2.1</string>
</dict>
</plist>`

	if err := os.WriteFile(filepath.Join(dir, "SystemVersion.plist"), []byte(plistContent), 0600); err != nil {
		t.Fatalf("failed to create plist file: %v", err)
	}

	src, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var data struct {
		ProductName    string `plist:"ProductName"`
		ProductVersion string `plist:"ProductVersion"`
	}

	if err := src.ReadPlist("/SystemVersion.plist", &data); err != nil {
		t.Fatalf("ReadPlist() error = %v", err)
	}

	if data.ProductName != "macOS" {
		t.Errorf("ProductName = %q, want macOS", data.ProductName)
	}
	if data.ProductVersion != "14.2.1" {
		t.Errorf("ProductVersion = %q, want 14.2.1", data.ProductVersion)
	}
}

func TestReadPlist_MissingFile(t *testing.T) {
	src, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var data map[string]any
	err = src.ReadPlist("/nonexistent.plist", &data)
	if err == nil {
		t.Fatal("ReadPlist() expected error for non-existent file")
	}
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got %v", err)
	}
}

// testUserAlice is the username used across user enumeration tests.
const testUserAlice = "alice"

func TestUsers_WithDslocal(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "alice", UID: 501, GID: 20, Name: "Alice Smith", Shell: "/bin/zsh"},
		{Username: "bob", UID: 502, GID: 20, Name: "Bob Jones", Shell: "/bin/bash"},
	}
	root := macostree.MustBuild(t, opts)

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}

	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}

	// Sort by username for deterministic ordering.
	sort.Slice(users, func(i, j int) bool {
		return users[i].Username < users[j].Username
	})

	// Verify alice.
	if users[0].Username != testUserAlice {
		t.Errorf("users[0].Username = %q, want alice", users[0].Username)
	}
	if users[0].UID != 501 {
		t.Errorf("users[0].UID = %d, want 501", users[0].UID)
	}
	if users[0].GID != 20 {
		t.Errorf("users[0].GID = %d, want 20", users[0].GID)
	}
	if users[0].Name != "Alice Smith" {
		t.Errorf("users[0].Name = %q, want Alice Smith", users[0].Name)
	}
	if users[0].Shell != "/bin/zsh" {
		t.Errorf("users[0].Shell = %q, want /bin/zsh", users[0].Shell)
	}
	if users[0].HomeDir != "/Users/alice" {
		t.Errorf("users[0].HomeDir = %q, want /Users/alice", users[0].HomeDir)
	}

	// Verify bob.
	if users[1].Username != "bob" {
		t.Errorf("users[1].Username = %q, want bob", users[1].Username)
	}
	if users[1].UID != 502 {
		t.Errorf("users[1].UID = %d, want 502", users[1].UID)
	}
	if users[1].Name != "Bob Jones" {
		t.Errorf("users[1].Name = %q, want Bob Jones", users[1].Name)
	}
	if users[1].Shell != "/bin/bash" {
		t.Errorf("users[1].Shell = %q, want /bin/bash", users[1].Shell)
	}
}

func TestUsers_NoDslocal(t *testing.T) {
	// Create tree manually with Users/alice/ but no dslocal plists.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Users", testUserAlice), 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("failed to create user dir: %v", err)
	}

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}

	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}

	if users[0].Username != testUserAlice {
		t.Errorf("Username = %q, want alice", users[0].Username)
	}
	if users[0].HomeDir != "/Users/alice" {
		t.Errorf("HomeDir = %q, want /Users/alice", users[0].HomeDir)
	}
	// Without dslocal plist, UID should be zero-value.
	if users[0].UID != 0 {
		t.Errorf("UID = %d, want 0 (default)", users[0].UID)
	}
	if users[0].Name != "" {
		t.Errorf("Name = %q, want empty", users[0].Name)
	}
}

func TestUsers_NoUsersDir(t *testing.T) {
	root := t.TempDir()

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}

	if len(users) != 0 {
		t.Errorf("len(users) = %d, want 0", len(users))
	}
}

func TestUsers_SharedAndHiddenSkipped(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Users", "Shared"), 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("failed to create Shared dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Users", ".localized"), 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("failed to create .localized dir: %v", err)
	}

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}

	if len(users) != 0 {
		t.Errorf("len(users) = %d, want 0 (Shared and .localized should be skipped)", len(users))
	}
}

func TestUsers_FilesInUsersDirSkipped(t *testing.T) {
	// Files in the Users/ directory should not be treated as users.
	root := t.TempDir()
	usersDir := filepath.Join(root, "Users")
	if err := os.MkdirAll(usersDir, 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("failed to create Users dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, ".DS_Store"), []byte{}, 0600); err != nil {
		t.Fatalf("failed to create .DS_Store: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(usersDir, testUserAlice), 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("failed to create alice dir: %v", err)
	}

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}

	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].Username != testUserAlice {
		t.Errorf("Username = %q, want alice", users[0].Username)
	}
}

func TestResolvePath_WithUsers(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "alice", UID: 501, GID: 20, Name: "Alice Smith", Shell: "/bin/zsh"},
	}
	root := macostree.MustBuild(t, opts)

	src, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "tilde only resolves to first user home",
			path: "~",
			want: "/Users/alice",
		},
		{
			name: "tilde slash resolves to first user home subdir",
			path: "~/Documents",
			want: "/Users/alice/Documents",
		},
		{
			name: "tilde with username resolves to that user",
			path: "~alice/Library",
			want: "/Users/alice/Library",
		},
		{
			name: "absolute path unchanged",
			path: "/etc/passwd",
			want: "/etc/passwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := src.ResolvePath(tt.path)
			if got != tt.want {
				t.Errorf("ResolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
