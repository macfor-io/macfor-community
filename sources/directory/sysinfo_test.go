package directory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/testdata/macostree"
)

const (
	testOSVersion = "14.2.1"
	testOSName    = "macOS"
)

func TestOSVersion_WithPlist(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	version, err := src.OSVersion()
	if err != nil {
		t.Fatalf("OSVersion() error = %v", err)
	}
	if version != testOSVersion {
		t.Errorf("OSVersion() = %q, want %q", version, testOSVersion)
	}
}

func TestOSVersion_NoPlist(t *testing.T) {
	root := t.TempDir()
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	version, err := src.OSVersion()
	if err != nil {
		t.Fatalf("OSVersion() error = %v", err)
	}
	if version != unknownValue {
		t.Errorf("OSVersion() = %q, want %q", version, unknownValue)
	}
}

func TestOSVersion_EmptyVersion(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Options{
		OSVersion: "",
		OSBuild:   "23C71",
	})
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	version, err := src.OSVersion()
	if err != nil {
		t.Fatalf("OSVersion() error = %v", err)
	}
	if version != unknownValue {
		t.Errorf("OSVersion() = %q, want %q", version, unknownValue)
	}
}

func TestHostname_WithPlist(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	hostname, err := src.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}
	if hostname != "Alice's MacBook Pro" {
		t.Errorf("Hostname() = %q, want %q", hostname, "Alice's MacBook Pro")
	}
}

func TestHostname_FallbackToHostName(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Options{
		Hostname: "alices-mbp.local",
	})
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	hostname, err := src.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}
	// ComputerName is empty, so it should fall back to HostName.
	if hostname != "alices-mbp.local" {
		t.Errorf("Hostname() = %q, want %q", hostname, "alices-mbp.local")
	}
}

func TestHostname_NoPlist(t *testing.T) {
	root := t.TempDir()
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	hostname, err := src.Hostname()
	if err != nil {
		t.Fatalf("Hostname() error = %v", err)
	}
	if hostname != unknownValue {
		t.Errorf("Hostname() = %q, want %q", hostname, unknownValue)
	}
}

func TestSystemInfo_FullPlists(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("SystemInfo() returned nil")
	}

	// OS fields.
	if info.OSVersion != testOSVersion {
		t.Errorf("OSVersion = %q, want %q", info.OSVersion, testOSVersion)
	}
	if info.OSBuild != "23C71" {
		t.Errorf("OSBuild = %q, want %q", info.OSBuild, "23C71")
	}
	if info.OSCodename != "Sonoma" {
		t.Errorf("OSCodename = %q, want %q", info.OSCodename, "Sonoma")
	}
	if info.OSName != testOSName {
		t.Errorf("OSName = %q, want %q", info.OSName, testOSName)
	}

	// Network identity fields.
	if info.ComputerName != "Alice's MacBook Pro" {
		t.Errorf("ComputerName = %q, want %q", info.ComputerName, "Alice's MacBook Pro")
	}
	if info.Hostname != "alices-mbp.local" {
		t.Errorf("Hostname = %q, want %q", info.Hostname, "alices-mbp.local")
	}
	if info.LocalHostname != "Alices-MacBook-Pro" {
		t.Errorf("LocalHostname = %q, want %q", info.LocalHostname, "Alices-MacBook-Pro")
	}

	// Directory source fields.
	if info.Architecture != unknownValue {
		t.Errorf("Architecture = %q, want %q", info.Architecture, unknownValue)
	}
	if info.SourceType != macfor.SourceTypeDirectory {
		t.Errorf("SourceType = %v, want %v", info.SourceType, macfor.SourceTypeDirectory)
	}
	if info.ImagePath != root {
		t.Errorf("ImagePath = %q, want %q", info.ImagePath, root)
	}
	if info.CollectionTime.IsZero() {
		t.Error("CollectionTime is zero")
	}

	// Hardware model defaults to "unknown" since macostree doesn't generate platform plist.
	if info.HardwareModel != unknownValue {
		t.Errorf("HardwareModel = %q, want %q", info.HardwareModel, unknownValue)
	}
}

func TestSystemInfo_NoPlists(t *testing.T) {
	root := t.TempDir()
	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("SystemInfo() returned nil")
	}

	if info.OSVersion != unknownValue {
		t.Errorf("OSVersion = %q, want %q", info.OSVersion, unknownValue)
	}
	if info.Hostname != unknownValue {
		t.Errorf("Hostname = %q, want %q", info.Hostname, unknownValue)
	}
	if info.HardwareModel != unknownValue {
		t.Errorf("HardwareModel = %q, want %q", info.HardwareModel, unknownValue)
	}
	if info.OSName != testOSName {
		t.Errorf("OSName = %q, want %q", info.OSName, testOSName)
	}
	if info.CollectionTime.IsZero() {
		t.Error("CollectionTime is zero")
	}
}

func TestSystemInfo_PartialPlists(t *testing.T) {
	// Build tree, then remove preferences.plist to simulate partial data.
	root := macostree.MustBuild(t, macostree.Defaults())
	if err := os.Remove(filepath.Join(root, "Library", "Preferences", "SystemConfiguration", "preferences.plist")); err != nil {
		t.Fatalf("removing preferences.plist: %v", err)
	}

	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}
	if info == nil {
		t.Fatal("SystemInfo() returned nil")
	}

	// OS version should still be populated.
	if info.OSVersion != testOSVersion {
		t.Errorf("OSVersion = %q, want %q", info.OSVersion, testOSVersion)
	}
	// Hostname should be "unknown" since preferences.plist is missing.
	if info.Hostname != unknownValue {
		t.Errorf("Hostname = %q, want %q", info.Hostname, unknownValue)
	}
}

func TestSystemInfo_WithPlatformPlist(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())

	// Manually create a com.apple.platform.plist with a hardware model.
	writePlatformPlist(t, root, "Mac14,2")

	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}

	if info.HardwareModel != "Mac14,2" {
		t.Errorf("HardwareModel = %q, want %q", info.HardwareModel, "Mac14,2")
	}
	if info.HardwareModelName != "MacBook Air (M2, 2022)" {
		t.Errorf("HardwareModelName = %q, want %q", info.HardwareModelName, "MacBook Air (M2, 2022)")
	}
}

func TestSystemInfo_UnknownHardwareModel(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())

	// Create a platform plist with an unrecognized model.
	writePlatformPlist(t, root, "MacFuture99,1")

	src, err := New(root)
	if err != nil {
		t.Fatalf("New(%q) error = %v", root, err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo() error = %v", err)
	}

	if info.HardwareModel != "MacFuture99,1" {
		t.Errorf("HardwareModel = %q, want %q", info.HardwareModel, "MacFuture99,1")
	}
	// GetHardwareModelName returns the identifier itself for unknown models.
	if info.HardwareModelName != "MacFuture99,1" {
		t.Errorf("HardwareModelName = %q, want %q", info.HardwareModelName, "MacFuture99,1")
	}
}

// writePlatformPlist creates a com.apple.platform.plist with the given hardware model.
func writePlatformPlist(t *testing.T, root, model string) {
	t.Helper()

	dir := filepath.Join(root, "private", "var", "db", "SystemConfiguration")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("creating platform plist dir: %v", err)
	}

	// Write a minimal XML plist.
	content := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>hardware-model</key>
	<string>` + model + `</string>
</dict>
</plist>`

	path := filepath.Join(dir, "com.apple.platform.plist")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil { //nolint:gosec
		t.Fatalf("writing platform plist: %v", err)
	}
}
