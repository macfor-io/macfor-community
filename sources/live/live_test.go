package live

import (
	"errors"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestNew(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if src == nil {
		t.Fatal("New() returned nil")
	}
	if src.currentUser == nil {
		t.Error("currentUser should not be nil")
	}
}

func TestLiveSource_Type(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := src.Type(); got != macfor.SourceTypeLive {
		t.Errorf("Type() = %v, want %v", got, macfor.SourceTypeLive)
	}
}

func TestLiveSource_Open(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello forensics")
	if err := os.WriteFile(tmpFile, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	t.Run("existing file", func(t *testing.T) {
		r, err := src.Open(tmpFile)
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
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := src.Open(filepath.Join(tmpDir, "nonexistent.txt"))
		if err == nil {
			t.Fatal("Open() expected error for non-existent file")
		}
		if !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("error should wrap ErrNotFound, got %v", err)
		}
	})

	t.Run("permission denied", func(t *testing.T) {
		// Create a file with no read permission
		noReadFile := filepath.Join(tmpDir, "noread.txt")
		if err := os.WriteFile(noReadFile, []byte("secret"), 0000); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
		defer func() { _ = os.Chmod(noReadFile, 0600) }() // Restore for cleanup

		_, err := src.Open(noReadFile)
		if err == nil {
			t.Fatal("Open() expected error for permission denied")
		}
		if !errors.Is(err, macfor.ErrPermissionDenied) {
			t.Errorf("error should wrap ErrPermissionDenied, got %v", err)
		}
	})
}

func TestLiveSource_Stat(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Create a temp file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("hello forensics")
	if err := os.WriteFile(tmpFile, content, 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	t.Run("existing file", func(t *testing.T) {
		info, err := src.Stat(tmpFile)
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

		// Check forensic timestamps are populated (on macOS)
		if info.AccessTime().IsZero() {
			t.Error("AccessTime() should not be zero")
		}
		if info.ChangeTime().IsZero() {
			t.Error("ChangeTime() should not be zero")
		}
		if info.BirthTime().IsZero() {
			t.Error("BirthTime() should not be zero")
		}

		// Birth time should be <= mod time
		if info.BirthTime().After(info.ModTime().Add(time.Second)) {
			t.Errorf("BirthTime() %v should be <= ModTime() %v", info.BirthTime(), info.ModTime())
		}
	})

	t.Run("directory", func(t *testing.T) {
		info, err := src.Stat(tmpDir)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := src.Stat(filepath.Join(tmpDir, "nonexistent.txt"))
		if err == nil {
			t.Fatal("Stat() expected error for non-existent file")
		}
		if !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("error should wrap ErrNotFound, got %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		linkPath := filepath.Join(tmpDir, "link.txt")
		if err := os.Symlink(tmpFile, linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		info, err := src.Stat(linkPath)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}

		// Lstat should report the symlink, not the target
		if info.Mode()&os.ModeSymlink == 0 {
			t.Error("expected symlink mode")
		}
	})
}

func TestLiveSource_ReadDir(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create some files
	files := []string{"a.txt", "b.txt", "c.txt"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("content"), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}

	// Create a subdirectory
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	t.Run("list directory", func(t *testing.T) {
		entries, err := src.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("ReadDir() error = %v", err)
		}

		if len(entries) != 4 { // 3 files + 1 subdir
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
		_, err := src.ReadDir(filepath.Join(tmpDir, "nonexistent"))
		if err == nil {
			t.Fatal("ReadDir() expected error for non-existent directory")
		}
		if !errors.Is(err, macfor.ErrNotFound) {
			t.Errorf("error should wrap ErrNotFound, got %v", err)
		}
	})
}

func TestLiveSource_Glob(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create some files
	txtFiles := []string{"a.txt", "b.txt"}
	for _, f := range txtFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, f), []byte("content"), 0600); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "c.log"), []byte("content"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	t.Run("match txt files", func(t *testing.T) {
		matches, err := src.Glob(filepath.Join(tmpDir, "*.txt"))
		if err != nil {
			t.Fatalf("Glob() error = %v", err)
		}

		if len(matches) != 2 {
			t.Errorf("len(matches) = %d, want 2", len(matches))
		}
	})

	t.Run("match all files", func(t *testing.T) {
		matches, err := src.Glob(filepath.Join(tmpDir, "*"))
		if err != nil {
			t.Fatalf("Glob() error = %v", err)
		}

		if len(matches) != 3 {
			t.Errorf("len(matches) = %d, want 3", len(matches))
		}
	})

	t.Run("no matches", func(t *testing.T) {
		matches, err := src.Glob(filepath.Join(tmpDir, "*.xyz"))
		if err != nil {
			t.Fatalf("Glob() error = %v", err)
		}

		if len(matches) != 0 {
			t.Errorf("len(matches) = %d, want 0", len(matches))
		}
	})
}

func TestLiveSource_Walk(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create a directory structure
	// tmpDir/
	//   a.txt
	//   subdir/
	//     b.txt

	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("b"), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	t.Run("walk all", func(t *testing.T) {
		var visited []string
		err := src.Walk(tmpDir, func(path string, _ macfor.FileInfo, err error) error {
			if err != nil {
				return err
			}
			visited = append(visited, path)
			return nil
		})
		if err != nil {
			t.Fatalf("Walk() error = %v", err)
		}

		if len(visited) != 4 { // tmpDir, a.txt, subdir, b.txt
			t.Errorf("len(visited) = %d, want 4", len(visited))
		}
	})

	t.Run("walk with FileInfo", func(t *testing.T) {
		err := src.Walk(tmpDir, func(path string, info macfor.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Verify forensic timestamps are available
			if !info.IsDir() {
				if info.AccessTime().IsZero() {
					t.Errorf("AccessTime() for %s should not be zero", path)
				}
				if info.BirthTime().IsZero() {
					t.Errorf("BirthTime() for %s should not be zero", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Walk() error = %v", err)
		}
	})
}

func TestLiveSource_ResolvePath(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	currentUser, err := user.Current()
	if err != nil {
		t.Fatalf("user.Current() error = %v", err)
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
			name: "tilde alone",
			path: "~",
			want: currentUser.HomeDir,
		},
		{
			name: "tilde with subpath",
			path: "~/Documents",
			want: filepath.Join(currentUser.HomeDir, "Documents"),
		},
		{
			name: "tilde with deep path",
			path: "~/Documents/work/project",
			want: filepath.Join(currentUser.HomeDir, "Documents/work/project"),
		},
		{
			name: "tilde with current username",
			path: "~" + currentUser.Username,
			want: currentUser.HomeDir,
		},
		{
			name: "tilde with current username and path",
			path: "~" + currentUser.Username + "/Documents",
			want: filepath.Join(currentUser.HomeDir, "Documents"),
		},
		{
			name: "tilde with nonexistent user",
			path: "~nonexistentuser12345/Documents",
			want: "~nonexistentuser12345/Documents", // Returns unchanged
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

func TestWrapOSError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantError error
	}{
		{
			name:      "not exist",
			err:       os.ErrNotExist,
			wantError: macfor.ErrNotFound,
		},
		{
			name:      "permission denied",
			err:       os.ErrPermission,
			wantError: macfor.ErrPermissionDenied,
		},
		{
			name:      "other error",
			err:       errors.New("some error"),
			wantError: macfor.ErrIOError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := wrapOSError("test", "/path", tt.err)
			if !errors.Is(wrapped, tt.wantError) {
				t.Errorf("wrapOSError() = %v, want to wrap %v", wrapped, tt.wantError)
			}
		})
	}
}
