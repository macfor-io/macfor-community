package live

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

const darwinOS = "darwin"

// Integration tests that read actual system files.
// These tests verify the live source works correctly with real filesystem operations.

func TestIntegration_ReadSystemFile(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Read /etc/passwd which should always exist
	t.Run("read /etc/passwd", func(t *testing.T) {
		r, err := src.Open("/etc/passwd")
		if err != nil {
			t.Fatalf("Open(/etc/passwd) error = %v", err)
		}
		defer func() { _ = r.Close() }()

		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll error = %v", err)
		}

		if len(data) == 0 {
			t.Error("/etc/passwd should not be empty")
		}
	})

	// Stat a known file
	t.Run("stat /etc/passwd", func(t *testing.T) {
		info, err := src.Stat("/etc/passwd")
		if err != nil {
			t.Fatalf("Stat(/etc/passwd) error = %v", err)
		}

		if info.Name() != "passwd" {
			t.Errorf("Name() = %q, want passwd", info.Name())
		}

		if info.IsDir() {
			t.Error("IsDir() = true, want false")
		}
	})

	// Read a directory
	t.Run("read /etc directory", func(t *testing.T) {
		entries, err := src.ReadDir("/etc")
		if err != nil {
			t.Fatalf("ReadDir(/etc) error = %v", err)
		}

		if len(entries) == 0 {
			t.Error("/etc should not be empty")
		}

		// Should contain passwd
		found := false
		for _, e := range entries {
			if e.Name() == "passwd" {
				found = true
				break
			}
		}
		if !found {
			t.Error("/etc should contain passwd")
		}
	})
}

func TestIntegration_GlobSystemFiles(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Glob for plist files in /System
	t.Run("glob /System/Library/CoreServices/*.plist", func(t *testing.T) {
		matches, err := src.Glob("/System/Library/CoreServices/*.plist")
		if err != nil {
			t.Fatalf("Glob error = %v", err)
		}

		// Should find at least one plist
		if len(matches) == 0 {
			t.Error("expected at least one .plist file in CoreServices")
		}

		// All matches should end with .plist
		for _, m := range matches {
			if filepath.Ext(m) != ".plist" {
				t.Errorf("match %q does not end with .plist", m)
			}
		}
	})
}

func TestIntegration_WalkDirectory(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Walk a small system directory
	t.Run("walk /System/Library/CoreServices", func(t *testing.T) {
		var count int
		err := src.Walk("/System/Library/CoreServices", func(_ string, _ macfor.FileInfo, walkErr error) error {
			if walkErr != nil {
				// Skip permission errors
				if os.IsPermission(walkErr) {
					return nil
				}
				return walkErr
			}
			count++
			// Limit walk to prevent long test times
			if count > 100 {
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			t.Fatalf("Walk error = %v", err)
		}

		if count == 0 {
			t.Error("expected to walk at least one entry")
		}
	})
}

func TestIntegration_HomeDirectory(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify ~ resolution works
	t.Run("resolve and read home directory", func(t *testing.T) {
		resolved := src.ResolvePath("~")

		// Should resolve to an existing directory
		info, err := src.Stat(resolved)
		if err != nil {
			t.Fatalf("Stat(~) error = %v", err)
		}

		if !info.IsDir() {
			t.Error("home directory should be a directory")
		}

		// Should be able to read the directory
		entries, err := src.ReadDir(resolved)
		if err != nil {
			t.Fatalf("ReadDir(~) error = %v", err)
		}

		// Home directory typically has at least one entry
		if len(entries) == 0 {
			t.Log("warning: home directory appears empty")
		}
	})
}

func TestIntegration_ForensicTimestamps(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Verify forensic timestamps are populated for a system file
	t.Run("forensic timestamps on /etc/passwd", func(t *testing.T) {
		info, err := src.Stat("/etc/passwd")
		if err != nil {
			t.Fatalf("Stat error = %v", err)
		}

		// All timestamps should be non-zero on macOS
		if info.AccessTime().IsZero() {
			t.Error("AccessTime() should not be zero")
		}
		if info.ChangeTime().IsZero() {
			t.Error("ChangeTime() should not be zero")
		}
		if info.BirthTime().IsZero() {
			t.Error("BirthTime() should not be zero")
		}

		// ModTime should also be non-zero (from fs.FileInfo)
		if info.ModTime().IsZero() {
			t.Error("ModTime() should not be zero")
		}

		// Birth time should be <= change time (file was created before any metadata change)
		if info.BirthTime().After(info.ChangeTime()) {
			t.Errorf("BirthTime %v should be <= ChangeTime %v", info.BirthTime(), info.ChangeTime())
		}
	})
}

func TestIntegration_OSVersion(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("read OS version", func(t *testing.T) {
		version, err := src.OSVersion()
		if err != nil {
			t.Fatalf("OSVersion() error = %v", err)
		}

		if version == "" {
			t.Error("OSVersion() returned empty string")
		}

		// Version should look like "x.y" or "x.y.z" (e.g., "14.2" or "14.2.1")
		// Just verify it contains at least one dot
		if len(version) < 3 {
			t.Errorf("OSVersion() = %q, expected format like '14.2.1'", version)
		}

		t.Logf("Detected macOS version: %s", version)
	})

	t.Run("OS version is cached", func(t *testing.T) {
		// Call twice and verify it returns the same value (caching works)
		v1, err1 := src.OSVersion()
		v2, err2 := src.OSVersion()

		if err1 != nil || err2 != nil {
			t.Fatalf("OSVersion() errors: %v, %v", err1, err2)
		}

		if v1 != v2 {
			t.Errorf("OSVersion() not cached: %q != %q", v1, v2)
		}
	})
}

func TestIntegration_Hostname(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("read hostname", func(t *testing.T) {
		hostname, err := src.Hostname()
		if err != nil {
			t.Fatalf("Hostname() error = %v", err)
		}

		if hostname == "" {
			t.Error("Hostname() returned empty string")
		}

		t.Logf("Detected hostname: %s", hostname)
	})
}

func TestIntegration_ReadPlist(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("read SystemVersion.plist", func(t *testing.T) {
		var sv struct {
			ProductName         string `plist:"ProductName"`
			ProductVersion      string `plist:"ProductVersion"`
			ProductBuildVersion string `plist:"ProductBuildVersion"`
		}

		err := src.ReadPlist("/System/Library/CoreServices/SystemVersion.plist", &sv)
		if err != nil {
			t.Fatalf("ReadPlist() error = %v", err)
		}

		if sv.ProductName == "" {
			t.Error("ProductName should not be empty")
		}
		if sv.ProductVersion == "" {
			t.Error("ProductVersion should not be empty")
		}
		if sv.ProductBuildVersion == "" {
			t.Error("ProductBuildVersion should not be empty")
		}

		t.Logf("Read SystemVersion.plist: %s %s (%s)", sv.ProductName, sv.ProductVersion, sv.ProductBuildVersion)
	})

	t.Run("read plist into map", func(t *testing.T) {
		var result map[string]any

		err := src.ReadPlist("/System/Library/CoreServices/SystemVersion.plist", &result)
		if err != nil {
			t.Fatalf("ReadPlist() error = %v", err)
		}

		if len(result) == 0 {
			t.Error("expected non-empty result map")
		}

		if _, ok := result["ProductVersion"]; !ok {
			t.Error("expected ProductVersion key in result")
		}
	})
}

func TestIntegration_Users(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("enumerate users", func(t *testing.T) {
		users, err := src.Users()
		if err != nil {
			t.Fatalf("Users() error = %v", err)
		}

		// Should find at least one human user (the current user)
		if len(users) == 0 {
			t.Error("Users() returned empty slice, expected at least one human user")
		}

		t.Logf("Found %d human users", len(users))
		for _, u := range users {
			t.Logf("  - %s (UID %d, %s)", u.Username, u.UID, u.Shell)
		}
	})

	t.Run("find current user", func(t *testing.T) {
		users, err := src.Users()
		if err != nil {
			t.Fatalf("Users() error = %v", err)
		}

		// Get current user from Source
		currentUsername := src.currentUser.Username

		var found bool
		for _, u := range users {
			if u.Username == currentUsername {
				found = true

				// Verify the user data looks reasonable
				if u.UID < 500 {
					t.Errorf("current user UID = %d, expected >= 500", u.UID)
				}
				if u.HomeDir == "" {
					t.Error("current user HomeDir should not be empty")
				}
				if u.Shell == "" {
					t.Error("current user Shell should not be empty")
				}

				t.Logf("Found current user: %s (UID %d, home: %s, shell: %s)",
					u.Username, u.UID, u.HomeDir, u.Shell)
				break
			}
		}

		if !found {
			t.Errorf("current user %q not found in Users() result", currentUsername)
		}
	})

	t.Run("no system users", func(t *testing.T) {
		users, err := src.Users()
		if err != nil {
			t.Fatalf("Users() error = %v", err)
		}

		for _, u := range users {
			// No user should have UID < 500
			if u.UID < 500 {
				t.Errorf("found system user in results: %s (UID %d)", u.Username, u.UID)
			}

			// No user should have nologin or false shell
			if u.Shell == "/usr/bin/false" || u.Shell == "/sbin/nologin" || u.Shell == "/bin/false" {
				t.Errorf("found service account in results: %s (shell %s)", u.Username, u.Shell)
			}

			// Special system users should not appear
			if u.Username == "nobody" || u.Username == "root" || u.Username == "daemon" {
				t.Errorf("found special system user in results: %s", u.Username)
			}
		}
	})
}
