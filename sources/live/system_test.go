package live

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestLiveSource_Hostname(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	hostname, err := src.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}

	if hostname == "" {
		t.Error("Hostname() returned empty string")
	}

	// Cross-check with os.Hostname()
	expected, _ := os.Hostname()
	if hostname != expected {
		t.Errorf("Hostname() = %q, want %q", hostname, expected)
	}
}

func TestLiveSource_ReadPlist_XMLFormat(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create a test XML plist file
	xmlPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>ProductName</key>
	<string>macOS</string>
	<key>ProductVersion</key>
	<string>14.2.1</string>
	<key>ProductBuildVersion</key>
	<string>23C71</string>
</dict>
</plist>`

	plistPath := filepath.Join(tmpDir, "test.plist")
	if err := os.WriteFile(plistPath, []byte(xmlPlist), 0600); err != nil {
		t.Fatalf("failed to create test plist: %v", err)
	}

	var result struct {
		ProductName         string `plist:"ProductName"`
		ProductVersion      string `plist:"ProductVersion"`
		ProductBuildVersion string `plist:"ProductBuildVersion"`
	}

	if err := src.ReadPlist(plistPath, &result); err != nil {
		t.Fatalf("ReadPlist() error = %v", err)
	}

	if result.ProductName != "macOS" {
		t.Errorf("ProductName = %q, want %q", result.ProductName, "macOS")
	}
	if result.ProductVersion != "14.2.1" {
		t.Errorf("ProductVersion = %q, want %q", result.ProductVersion, "14.2.1")
	}
	if result.ProductBuildVersion != "23C71" {
		t.Errorf("ProductBuildVersion = %q, want %q", result.ProductBuildVersion, "23C71")
	}
}

func TestLiveSource_ReadPlist_MapOutput(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create a test plist with various types
	xmlPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>StringValue</key>
	<string>hello</string>
	<key>IntValue</key>
	<integer>42</integer>
	<key>BoolValue</key>
	<true/>
	<key>ArrayValue</key>
	<array>
		<string>a</string>
		<string>b</string>
	</array>
</dict>
</plist>`

	plistPath := filepath.Join(tmpDir, "test.plist")
	if err := os.WriteFile(plistPath, []byte(xmlPlist), 0600); err != nil {
		t.Fatalf("failed to create test plist: %v", err)
	}

	var result map[string]any
	if err := src.ReadPlist(plistPath, &result); err != nil {
		t.Fatalf("ReadPlist() error = %v", err)
	}

	if result["StringValue"] != "hello" {
		t.Errorf("StringValue = %v, want %q", result["StringValue"], "hello")
	}
	if result["IntValue"] != uint64(42) {
		t.Errorf("IntValue = %v (type %T), want uint64(42)", result["IntValue"], result["IntValue"])
	}
	if result["BoolValue"] != true {
		t.Errorf("BoolValue = %v, want true", result["BoolValue"])
	}
}

func TestLiveSource_ReadPlist_NonExistent(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.plist")

	var result map[string]any
	err = src.ReadPlist(nonExistentPath, &result)
	if err == nil {
		t.Fatal("ReadPlist() expected error for non-existent file")
	}
	if !errors.Is(err, macfor.ErrNotFound) {
		t.Errorf("error should wrap ErrNotFound, got %v", err)
	}
}

func TestLiveSource_ReadPlist_InvalidPlist(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create an invalid plist file
	invalidPlist := `this is not a valid plist file`
	plistPath := filepath.Join(tmpDir, "invalid.plist")
	if err := os.WriteFile(plistPath, []byte(invalidPlist), 0600); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	var result map[string]any
	err = src.ReadPlist(plistPath, &result)
	if err == nil {
		t.Fatal("ReadPlist() expected error for invalid plist")
	}
	if !errors.Is(err, macfor.ErrParseError) {
		t.Errorf("error should wrap ErrParseError, got %v", err)
	}
}

func TestLiveSource_ReadPlist_PermissionDenied(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tmpDir := t.TempDir()

	// Create a plist file with no read permission
	xmlPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Test</key>
	<string>value</string>
</dict>
</plist>`

	plistPath := filepath.Join(tmpDir, "noread.plist")
	if err := os.WriteFile(plistPath, []byte(xmlPlist), 0000); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer func() { _ = os.Chmod(plistPath, 0600) }() // Restore for cleanup

	var result map[string]any
	err = src.ReadPlist(plistPath, &result)
	if err == nil {
		t.Fatal("ReadPlist() expected error for permission denied")
	}
	if !errors.Is(err, macfor.ErrPermissionDenied) {
		t.Errorf("error should wrap ErrPermissionDenied, got %v", err)
	}
}

func TestLiveSource_ReadPlist_TildeExpansion(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Create a test plist in a temp subdirectory of home
	homeDir := src.currentUser.HomeDir
	tmpSubDir := filepath.Join(homeDir, ".macfor_test_"+t.Name())
	if err := os.MkdirAll(tmpSubDir, 0700); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpSubDir) }()

	xmlPlist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>TildeTest</key>
	<string>passed</string>
</dict>
</plist>`

	plistPath := filepath.Join(tmpSubDir, "test.plist")
	if err := os.WriteFile(plistPath, []byte(xmlPlist), 0600); err != nil {
		t.Fatalf("failed to create test plist: %v", err)
	}

	// Read using tilde path
	tildePath := "~/.macfor_test_" + t.Name() + "/test.plist"
	var result struct {
		TildeTest string `plist:"TildeTest"`
	}

	if err := src.ReadPlist(tildePath, &result); err != nil {
		t.Fatalf("ReadPlist() with tilde path error = %v", err)
	}

	if result.TildeTest != "passed" {
		t.Errorf("TildeTest = %q, want %q", result.TildeTest, "passed")
	}
}
