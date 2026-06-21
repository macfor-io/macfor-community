package container

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/manifestpack"
)

func TestNewContainer(t *testing.T) {
	t.Run("creates container file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		// File should exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("container file was not created")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "nested", "dir", "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		// File should exist
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("container file was not created")
		}
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		// Try to create in a path that can't be written
		_, err := NewContainer(Config{OutputPath: "/nonexistent/readonly/path/test.zip"})
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})
}

func TestContainer_AddFile(t *testing.T) {
	t.Run("adds file with correct content", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		content := "test content for file"
		if err := c.AddFile("test/file.txt", strings.NewReader(content)); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify content in zip
		verifyZipContent(t, path, "test/file.txt", content)
	})

	t.Run("computes correct SHA-256 hash", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		content := "hash me please"
		expectedHash := computeSHA256(content)

		if err := c.AddFile("hashtest.txt", strings.NewReader(content)); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		hashes := c.Hashes()
		gotHash, ok := hashes["hashtest.txt"]
		if !ok {
			t.Fatal("hash not recorded for file")
		}

		if gotHash != expectedHash {
			t.Errorf("hash mismatch: got %s, want %s", gotHash, expectedHash)
		}

		_ = c.Close()
	})

	t.Run("uses Store method (no compression)", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		// Use compressible content
		content := strings.Repeat("compressible data ", 100)
		if err := c.AddFile("compress-test.txt", strings.NewReader(content)); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Open the zip and verify the method
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("opening zip: %v", err)
		}
		defer func() { _ = zr.Close() }()

		for _, f := range zr.File {
			if f.Name == "compress-test.txt" {
				if f.Method != zip.Store {
					t.Errorf("expected Store method (0), got %d", f.Method)
				}
				// With Store method, compressed size should equal uncompressed size
				if f.CompressedSize64 != f.UncompressedSize64 {
					t.Errorf("compressed size (%d) != uncompressed size (%d)", f.CompressedSize64, f.UncompressedSize64)
				}
				return
			}
		}
		t.Error("file not found in zip")
	})

	t.Run("returns error after close", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		_ = c.Close()

		err = c.AddFile("test.txt", strings.NewReader("content"))
		if err == nil {
			t.Error("expected error when adding file to closed container")
		}
	})
}

func TestContainer_AddJSON(t *testing.T) {
	t.Run("adds valid JSON content", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		data := map[string]any{
			"key":   "value",
			"count": 42,
			"items": []string{"a", "b", "c"},
		}

		if err := c.AddJSON("data.json", data); err != nil {
			t.Fatalf("AddJSON() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify JSON is valid and correctly formatted
		content := readFromZip(t, path, "data.json")
		var decoded map[string]any
		if err := json.Unmarshal([]byte(content), &decoded); err != nil {
			t.Fatalf("unmarshaling JSON: %v", err)
		}

		if decoded["key"] != "value" {
			t.Errorf("expected key=value, got %v", decoded["key"])
		}
		if decoded["count"] != float64(42) { // JSON numbers are float64
			t.Errorf("expected count=42, got %v", decoded["count"])
		}
	})

	t.Run("computes hash for JSON content", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		data := map[string]string{"test": "value"}
		if err := c.AddJSON("test.json", data); err != nil {
			t.Fatalf("AddJSON() error = %v", err)
		}

		hashes := c.Hashes()
		if _, ok := hashes["test.json"]; !ok {
			t.Error("hash not recorded for JSON file")
		}

		_ = c.Close()
	})

	t.Run("returns error for unmarshalable types", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		// Channels cannot be marshaled to JSON
		err = c.AddJSON("invalid.json", make(chan int))
		if err == nil {
			t.Error("expected error for unmarshalable type")
		}
	})
}

func TestContainer_Hashes(t *testing.T) {
	t.Run("returns copy of hashes", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		if err := c.AddFile("file1.txt", strings.NewReader("content1")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}
		if err := c.AddFile("file2.txt", strings.NewReader("content2")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		hashes1 := c.Hashes()
		hashes2 := c.Hashes()

		// Modifying the returned map should not affect internal state
		hashes1["modified"] = "value"

		if _, ok := hashes2["modified"]; ok {
			t.Error("Hashes() did not return a copy")
		}
	})

	t.Run("contains all added files", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		files := []string{"a.txt", "b/c.txt", "d/e/f.txt"}
		for _, f := range files {
			if err := c.AddFile(f, strings.NewReader("content")); err != nil {
				t.Fatalf("AddFile(%s) error = %v", f, err)
			}
		}

		hashes := c.Hashes()
		for _, f := range files {
			if _, ok := hashes[f]; !ok {
				t.Errorf("hash missing for %s", f)
			}
		}
	})
}

func TestContainer_Close(t *testing.T) {
	t.Run("is idempotent", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		if err := c.AddFile("test.txt", strings.NewReader("content")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		// First close
		if err := c.Close(); err != nil {
			t.Fatalf("first Close() error = %v", err)
		}

		// Second close should not error
		if err := c.Close(); err != nil {
			t.Errorf("second Close() error = %v", err)
		}
	})

	t.Run("creates valid zip file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		if err := c.AddFile("file1.txt", strings.NewReader("content1")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}
		if err := c.AddFile("dir/file2.txt", strings.NewReader("content2")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify we can open it as a valid zip
		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("opening zip: %v", err)
		}
		defer func() { _ = zr.Close() }()

		if len(zr.File) != 2 {
			t.Errorf("expected 2 files, got %d", len(zr.File))
		}
	})
}

func TestContainer_MarkError(t *testing.T) {
	t.Run("removes container on close after error", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		if err := c.AddFile("test.txt", strings.NewReader("content")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}
		c.MarkError()

		_ = c.Close()

		// File should be removed
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("container file should have been removed after MarkError")
		}
	})
}

func TestContainer_Path(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	if c.Path() != path {
		t.Errorf("Path() = %s, want %s", c.Path(), path)
	}
}

func TestContainer_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	// Add files concurrently
	var wg sync.WaitGroup
	numFiles := 100

	for i := 0; i < numFiles; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			filename := filepath.Join("concurrent", string(rune('a'+i%26))+".txt")
			_ = c.AddFile(filename, strings.NewReader("content"))
		}(i)
	}

	wg.Wait()

	// Verify hashes were recorded (thread-safe access)
	hashes := c.Hashes()
	if len(hashes) == 0 {
		t.Error("no hashes recorded")
	}
}

// Helper functions

func computeSHA256(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// testCollectionID is a constant used across tests.
const testCollectionID = "test-collection-123"

// testOSVersion is a constant used across tests.
const testOSVersion = "14.2.1"

func verifyZipContent(t *testing.T, zipPath, filePath, expectedContent string) {
	t.Helper()

	content := readFromZip(t, zipPath, filePath)
	if content != expectedContent {
		t.Errorf("content mismatch:\ngot:  %q\nwant: %q", content, expectedContent)
	}
}

func readFromZip(t *testing.T, zipPath, filePath string) string {
	t.Helper()

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if f.Name == filePath {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("opening file in zip: %v", err)
			}
			defer func() { _ = rc.Close() }()

			// Limit read size to prevent decompression bomb (G110)
			limited := io.LimitReader(rc, 10*1024*1024) // 10MB max for tests
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, limited); err != nil {
				t.Fatalf("reading file from zip: %v", err)
			}
			return buf.String()
		}
	}

	t.Fatalf("file %q not found in zip", filePath)
	return ""
}

func TestSourceInfoFromSystemInfo(t *testing.T) {
	collTime := time.Date(2026, 2, 14, 10, 0, 0, 0, time.UTC)
	bootTime := time.Date(2026, 2, 13, 8, 0, 0, 0, time.UTC)

	si := &macfor.SystemInfo{
		OSName:              "macOS",
		OSVersion:           testOSVersion,
		OSBuild:             "23C71",
		OSCodename:          "Sonoma",
		KernelVersion:       "23.2.0",
		HardwareModel:       "Mac14,2",
		HardwareModelName:   "MacBook Air (M2, 2022)",
		SerialNumber:        "ABC123",
		Architecture:        "arm64",
		CPUType:             "Apple M2",
		CPUCores:            8,
		MemoryGB:            16,
		BootVolumeName:      "Macintosh HD",
		BootVolumeUUID:      "uuid-1234",
		BootVolumeFS:        "apfs",
		BootVolumeEncrypted: true,
		SIPEnabled:          true,
		FileVaultEnabled:    true,
		SecureBootLevel:     "full",
		GatekeeperEnabled:   true,
		Hostname:            "test-mac.local",
		LocalHostname:       "test-mac",
		ComputerName:        "Test Mac",
		Timezone:            "America/Los_Angeles",
		TimezoneOffset:      "-08:00",
		BootTime:            bootTime,
		UptimeSeconds:       93600,
		CollectionTime:      collTime,
		CollectionUser:      "forensic",
		CollectionUID:       501,
		IsRoot:              false,
		SourceType:          macfor.SourceTypeLive,
	}

	info := SourceInfoFromSystemInfo(si)

	// Verify legacy flat fields.
	if info.Type != "live" {
		t.Errorf("Type = %q, want %q", info.Type, "live")
	}
	if info.Hostname != "test-mac.local" {
		t.Errorf("Hostname = %q, want %q", info.Hostname, "test-mac.local")
	}
	if info.OSVersion != testOSVersion {
		t.Errorf("OSVersion = %q, want %q", info.OSVersion, testOSVersion)
	}
	if info.SerialNumber != "ABC123" {
		t.Errorf("SerialNumber = %q, want %q", info.SerialNumber, "ABC123")
	}

	// Verify nested OS info.
	if info.OS == nil {
		t.Fatal("OS is nil")
	}
	if info.OS.Version != testOSVersion {
		t.Errorf("OS.Version = %q, want %q", info.OS.Version, testOSVersion)
	}
	if info.OS.Build != "23C71" {
		t.Errorf("OS.Build = %q, want %q", info.OS.Build, "23C71")
	}
	if info.OS.Codename != "Sonoma" {
		t.Errorf("OS.Codename = %q, want %q", info.OS.Codename, "Sonoma")
	}

	// Verify nested Hardware info.
	if info.Hardware == nil {
		t.Fatal("Hardware is nil")
	}
	if info.Hardware.Model != "Mac14,2" {
		t.Errorf("Hardware.Model = %q, want %q", info.Hardware.Model, "Mac14,2")
	}
	if info.Hardware.Architecture != "arm64" {
		t.Errorf("Hardware.Architecture = %q, want %q", info.Hardware.Architecture, "arm64")
	}
	if info.Hardware.CPUCores != 8 {
		t.Errorf("Hardware.CPUCores = %d, want %d", info.Hardware.CPUCores, 8)
	}

	// Verify nested Security info.
	if info.Security == nil {
		t.Fatal("Security is nil")
	}
	if !info.Security.SIPEnabled {
		t.Error("Security.SIPEnabled = false, want true")
	}
	if !info.Security.FileVaultEnabled {
		t.Error("Security.FileVaultEnabled = false, want true")
	}

	// Verify nested Storage info.
	if info.Storage == nil {
		t.Fatal("Storage is nil")
	}
	if info.Storage.BootVolumeName != "Macintosh HD" {
		t.Errorf("Storage.BootVolumeName = %q, want %q", info.Storage.BootVolumeName, "Macintosh HD")
	}
	if !info.Storage.BootVolumeEncrypted {
		t.Error("Storage.BootVolumeEncrypted = false, want true")
	}

	// Verify nested Network info.
	if info.Network == nil {
		t.Fatal("Network is nil")
	}
	if info.Network.Hostname != "test-mac.local" {
		t.Errorf("Network.Hostname = %q, want %q", info.Network.Hostname, "test-mac.local")
	}
	if info.Network.ComputerName != "Test Mac" {
		t.Errorf("Network.ComputerName = %q, want %q", info.Network.ComputerName, "Test Mac")
	}

	// Verify nested Temporal info.
	if info.Temporal == nil {
		t.Fatal("Temporal is nil")
	}
	if info.Temporal.Timezone != "America/Los_Angeles" {
		t.Errorf("Temporal.Timezone = %q, want %q", info.Temporal.Timezone, "America/Los_Angeles")
	}
	if info.Temporal.BootTime != bootTime {
		t.Errorf("Temporal.BootTime = %v, want %v", info.Temporal.BootTime, bootTime)
	}
	if info.Temporal.UptimeSeconds != 93600 {
		t.Errorf("Temporal.UptimeSeconds = %d, want %d", info.Temporal.UptimeSeconds, 93600)
	}

	// Verify nested CollectionEnv info.
	if info.CollectionEnv == nil {
		t.Fatal("CollectionEnv is nil")
	}
	if info.CollectionEnv.User != "forensic" {
		t.Errorf("CollectionEnv.User = %q, want %q", info.CollectionEnv.User, "forensic")
	}
	if info.CollectionEnv.UID != 501 {
		t.Errorf("CollectionEnv.UID = %d, want %d", info.CollectionEnv.UID, 501)
	}
}

func TestSourceInfoFromSystemInfo_JSONStructure(t *testing.T) {
	si := &macfor.SystemInfo{
		OSName:         "macOS",
		OSVersion:      testOSVersion,
		OSBuild:        "23C71",
		Architecture:   "arm64",
		Hostname:       "test-mac",
		SIPEnabled:     true,
		SourceType:     macfor.SourceTypeLive,
		CollectionTime: time.Date(2026, 2, 14, 10, 0, 0, 0, time.UTC),
	}

	info := SourceInfoFromSystemInfo(si)

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("marshaling SourceInfo: %v", err)
	}

	// Verify the JSON contains both legacy and nested fields.
	jsonStr := string(data)

	// Legacy flat fields.
	if !strings.Contains(jsonStr, `"type": "live"`) {
		t.Error("JSON missing legacy type field")
	}
	if !strings.Contains(jsonStr, `"hostname": "test-mac"`) {
		t.Error("JSON missing legacy hostname field")
	}
	if !strings.Contains(jsonStr, `"os_version": "14.2.1"`) {
		t.Error("JSON missing legacy os_version field")
	}

	// Nested fields.
	if !strings.Contains(jsonStr, `"os"`) {
		t.Error("JSON missing nested os object")
	}
	if !strings.Contains(jsonStr, `"hardware"`) {
		t.Error("JSON missing nested hardware object")
	}
	if !strings.Contains(jsonStr, `"security"`) {
		t.Error("JSON missing nested security object")
	}

	// Verify it can be round-tripped.
	var decoded SourceInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshaling SourceInfo: %v", err)
	}
	if decoded.OS.Version != testOSVersion {
		t.Errorf("round-trip: OS.Version = %q, want %q", decoded.OS.Version, testOSVersion)
	}
	if !decoded.Security.SIPEnabled {
		t.Error("round-trip: Security.SIPEnabled = false, want true")
	}
}

func TestContainer_SetCollectionMetadataWithSystemInfo(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	si := &macfor.SystemInfo{
		OSName:         "macOS",
		OSVersion:      testOSVersion,
		OSBuild:        "23C71",
		OSCodename:     "Sonoma",
		Architecture:   "arm64",
		Hostname:       "test-mac",
		SIPEnabled:     true,
		SourceType:     macfor.SourceTypeLive,
		CollectionTime: time.Now().UTC(),
	}

	meta := CollectionMeta{
		CollectionID: testCollectionID,
		ToolVersion:  "1.0.0",
		SourceType:   "live",
		Hostname:     "test-mac",
		OSVersion:    testOSVersion,
		SystemInfo:   si,
	}
	c.SetCollectionMetadata(meta)

	if c.manifest == nil {
		t.Fatal("manifest should be set")
	}

	// Verify legacy fields are populated.
	if c.manifest.Source.Hostname != "test-mac" {
		t.Errorf("Source.Hostname = %q, want %q", c.manifest.Source.Hostname, "test-mac")
	}
	if c.manifest.Source.OSVersion != testOSVersion {
		t.Errorf("Source.OSVersion = %q, want %q", c.manifest.Source.OSVersion, testOSVersion)
	}

	// Verify nested fields are populated.
	if c.manifest.Source.OS == nil {
		t.Fatal("Source.OS should be set")
	}
	if c.manifest.Source.OS.Version != testOSVersion {
		t.Errorf("Source.OS.Version = %q, want %q", c.manifest.Source.OS.Version, testOSVersion)
	}
	if c.manifest.Source.OS.Codename != "Sonoma" {
		t.Errorf("Source.OS.Codename = %q, want %q", c.manifest.Source.OS.Codename, "Sonoma")
	}
	if c.manifest.Source.Security == nil {
		t.Fatal("Source.Security should be set")
	}
	if !c.manifest.Source.Security.SIPEnabled {
		t.Error("Source.Security.SIPEnabled = false, want true")
	}

	// Verify manifest version is 1.2.
	if c.manifest.Version != "1.2" {
		t.Errorf("Version = %q, want %q", c.manifest.Version, "1.2")
	}
}

func TestContainer_SetCollectionMetadata(t *testing.T) {
	t.Run("sets metadata with provided collection ID", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		meta := CollectionMeta{
			CollectionID: testCollectionID,
			ToolVersion:  "1.0.0",
			SourceType:   "live",
			Hostname:     "test-host",
			OSVersion:    "14.2.1",
			OSName:       "macOS Sonoma",
		}
		c.SetCollectionMetadata(meta)

		if c.manifest == nil {
			t.Fatal("manifest should be set")
		}
		if c.manifest.CollectionID != testCollectionID {
			t.Errorf("CollectionID = %q, want %q", c.manifest.CollectionID, testCollectionID)
		}
		if c.manifest.ToolVersion != "1.0.0" {
			t.Errorf("ToolVersion = %q, want %q", c.manifest.ToolVersion, "1.0.0")
		}
		if c.manifest.Source.Hostname != "test-host" {
			t.Errorf("Hostname = %q, want %q", c.manifest.Source.Hostname, "test-host")
		}
	})

	t.Run("generates UUID when collection ID is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		meta := CollectionMeta{
			ToolVersion: "1.0.0",
			SourceType:  "live",
		}
		c.SetCollectionMetadata(meta)

		if c.manifest.CollectionID == "" {
			t.Error("CollectionID should be auto-generated")
		}
		// UUID format: 8-4-4-4-12
		if len(c.manifest.CollectionID) != 36 {
			t.Errorf("CollectionID length = %d, want 36 (UUID format)", len(c.manifest.CollectionID))
		}
	})

	t.Run("initializes custody log with collection started entry", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		meta := CollectionMeta{
			CollectionID: "test-123",
			ToolVersion:  "1.0.0",
			SourceType:   "live",
			Hostname:     "test-host",
		}
		c.SetCollectionMetadata(meta)

		if c.custodyLog == nil {
			t.Fatal("custodyLog should be set")
		}
		if len(c.custodyLog.Entries) != 1 {
			t.Fatalf("expected 1 custody entry, got %d", len(c.custodyLog.Entries))
		}
		entry := c.custodyLog.Entries[0]
		if entry.Action != CustodyActionCollectionStarted {
			t.Errorf("Action = %q, want %q", entry.Action, CustodyActionCollectionStarted)
		}
		if entry.Actor != "macfor" {
			t.Errorf("Actor = %q, want %q", entry.Actor, "macfor")
		}
	})
}

func TestContainer_AddCustodyEntry(t *testing.T) {
	t.Run("adds entry to custody log", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{
			CollectionID: "test-123",
			ToolVersion:  "1.0.0",
		})

		c.AddCustodyEntry(ChainOfCustodyEntry{
			Action: CustodyActionFileAdded,
			Actor:  "macfor",
			Target: "artifacts/shell/history.txt",
			Hash:   "abc123",
		})

		if len(c.custodyLog.Entries) != 2 { // initial + our entry
			t.Errorf("expected 2 entries, got %d", len(c.custodyLog.Entries))
		}

		entry := c.custodyLog.Entries[1]
		if entry.Action != CustodyActionFileAdded {
			t.Errorf("Action = %q, want %q", entry.Action, CustodyActionFileAdded)
		}
		if entry.Target != "artifacts/shell/history.txt" {
			t.Errorf("Target = %q, want %q", entry.Target, "artifacts/shell/history.txt")
		}
	})

	t.Run("sets timestamp if not provided", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.AddCustodyEntry(ChainOfCustodyEntry{
			Action: CustodyActionFileAdded,
			Actor:  "macfor",
		})

		if c.custodyLog.Entries[0].Timestamp.IsZero() {
			t.Error("timestamp should be auto-set")
		}
	})

	t.Run("works without SetCollectionMetadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		// Should not panic
		c.AddCustodyEntry(ChainOfCustodyEntry{
			Action: CustodyActionFileAdded,
			Actor:  "macfor",
		})

		if c.custodyLog == nil {
			t.Error("custodyLog should be auto-initialized")
		}
	})
}

func TestContainer_AddPluginSummary(t *testing.T) {
	t.Run("adds plugin summary and updates statistics", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{
			CollectionID: "test-123",
			ToolVersion:  "1.0.0",
		})

		c.AddPluginSummary(PluginSummary{
			ID:             "shell.history",
			Name:           "Shell History",
			Version:        "1.0.0",
			FilesCollected: 5,
			RecordsCreated: 100,
		})

		if len(c.manifest.Plugins) != 1 {
			t.Fatalf("expected 1 plugin, got %d", len(c.manifest.Plugins))
		}
		if c.manifest.Statistics.PluginsExecuted != 1 {
			t.Errorf("PluginsExecuted = %d, want 1", c.manifest.Statistics.PluginsExecuted)
		}
		if c.manifest.Statistics.TotalFiles != 5 {
			t.Errorf("TotalFiles = %d, want 5", c.manifest.Statistics.TotalFiles)
		}
		if c.manifest.Statistics.TotalRecords != 100 {
			t.Errorf("TotalRecords = %d, want 100", c.manifest.Statistics.TotalRecords)
		}
	})

	t.Run("accumulates statistics from multiple plugins", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0"})

		c.AddPluginSummary(PluginSummary{
			ID:                "shell.history",
			FilesCollected:    5,
			RecordsCreated:    100,
			ErrorsEncountered: 2,
		})
		c.AddPluginSummary(PluginSummary{
			ID:                "browser.safari",
			FilesCollected:    10,
			RecordsCreated:    200,
			ErrorsEncountered: 1,
		})

		stats := c.manifest.Statistics
		if stats.PluginsExecuted != 2 {
			t.Errorf("PluginsExecuted = %d, want 2", stats.PluginsExecuted)
		}
		if stats.TotalFiles != 15 {
			t.Errorf("TotalFiles = %d, want 15", stats.TotalFiles)
		}
		if stats.TotalRecords != 300 {
			t.Errorf("TotalRecords = %d, want 300", stats.TotalRecords)
		}
		if stats.TotalErrors != 3 {
			t.Errorf("TotalErrors = %d, want 3", stats.TotalErrors)
		}
	})
}

func TestContainer_Finalize_WritesAllMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	c.SetCollectionMetadata(CollectionMeta{
		CollectionID: testCollectionID,
		ToolVersion:  "1.0.0",
		SourceType:   "live",
		Hostname:     "test-host",
		OSVersion:    "14.2.1",
	})

	// Add some content
	if err := c.AddFile("test.txt", strings.NewReader("content")); err != nil {
		t.Fatalf("AddFile() error = %v", err)
	}

	c.AddPluginSummary(PluginSummary{
		ID:             "shell.history",
		Name:           "Shell History",
		FilesCollected: 1,
	})

	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	verifyManifestJSON(t, path)
	verifyCustodyLogJSON(t, path)
	verifyHashManifestJSON(t, path)
}

func TestContainer_SetManifestProvenance(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	c.SetCollectionMetadata(CollectionMeta{
		CollectionID: testCollectionID,
		ToolVersion:  "1.0.0",
		SourceType:   "live",
	})
	c.SetManifestProvenance(&manifestpack.Provenance{
		PackVersion: "1.0.0",
		Digest:      "abc123",
		Entries: []manifestpack.ProvenanceEntry{
			{PluginID: "browser.safari", SHA256: "deadbeef"},
		},
	})
	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// manifest.json carries the provenance block.
	var manifest Manifest
	if err := json.Unmarshal([]byte(readFromZip(t, path, "manifest.json")), &manifest); err != nil {
		t.Fatalf("unmarshaling manifest: %v", err)
	}
	if manifest.ManifestProvenance == nil {
		t.Fatal("manifest.ManifestProvenance is nil, want recorded provenance")
	}
	if manifest.ManifestProvenance.Digest != "abc123" || len(manifest.ManifestProvenance.Entries) != 1 {
		t.Errorf("unexpected provenance: %+v", manifest.ManifestProvenance)
	}

	// chain-of-custody.json carries the provenance event.
	var custody ChainOfCustodyLog
	if err := json.Unmarshal([]byte(readFromZip(t, path, "chain-of-custody.json")), &custody); err != nil {
		t.Fatalf("unmarshaling custody: %v", err)
	}
	found := false
	for _, e := range custody.Entries {
		if e.Action == CustodyActionManifestProvenance {
			found = true
		}
	}
	if !found {
		t.Errorf("no %q custody entry found", CustodyActionManifestProvenance)
	}
}

func verifyManifestJSON(t *testing.T, path string) {
	t.Helper()
	manifestContent := readFromZip(t, path, "manifest.json")
	var manifest Manifest
	if err := json.Unmarshal([]byte(manifestContent), &manifest); err != nil {
		t.Fatalf("unmarshaling manifest: %v", err)
	}
	if manifest.CollectionID != testCollectionID {
		t.Errorf("manifest.CollectionID = %q, want %q", manifest.CollectionID, testCollectionID)
	}
	if manifest.Source.Hostname != "test-host" {
		t.Errorf("manifest.Source.Hostname = %q, want %q", manifest.Source.Hostname, "test-host")
	}
	if len(manifest.Plugins) != 1 {
		t.Errorf("manifest.Plugins count = %d, want 1", len(manifest.Plugins))
	}
}

func verifyCustodyLogJSON(t *testing.T, path string) {
	t.Helper()
	custodyContent := readFromZip(t, path, "chain-of-custody.json")
	var custody ChainOfCustodyLog
	if err := json.Unmarshal([]byte(custodyContent), &custody); err != nil {
		t.Fatalf("unmarshaling custody log: %v", err)
	}
	if custody.CollectionID != testCollectionID {
		t.Errorf("custody.CollectionID = %q, want %q", custody.CollectionID, testCollectionID)
	}
	// Should have at least collection_started and collection_completed
	if len(custody.Entries) < 2 {
		t.Errorf("custody.Entries count = %d, want >= 2", len(custody.Entries))
	}
}

func verifyHashManifestJSON(t *testing.T, path string) {
	t.Helper()
	hashContent := readFromZip(t, path, "hashes.json")
	var hashManifest HashManifest
	if err := json.Unmarshal([]byte(hashContent), &hashManifest); err != nil {
		t.Fatalf("unmarshaling hash manifest: %v", err)
	}
	if hashManifest.Algorithm != "sha256" {
		t.Errorf("hashManifest.Algorithm = %q, want %q", hashManifest.Algorithm, "sha256")
	}
	if _, ok := hashManifest.Files["test.txt"]; !ok {
		t.Error("hash manifest should contain test.txt")
	}
}

func TestContainer_Finalize_IsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0"})

	// First finalize
	if err := c.Finalize(); err != nil {
		t.Fatalf("first Finalize() error = %v", err)
	}

	// Second finalize should not error
	if err := c.Finalize(); err != nil {
		t.Errorf("second Finalize() error = %v", err)
	}
}

func TestContainer_Finalize_ErrorAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	_ = c.Close()

	err = c.Finalize()
	if err == nil {
		t.Error("expected error when finalizing closed container")
	}
}

func TestContainer_Finalize(t *testing.T) {
	t.Run("works without SetCollectionMetadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		if err := c.AddFile("test.txt", strings.NewReader("content")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		// Should not panic, should use defaults
		if err := c.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify manifest was still written
		manifestContent := readFromZip(t, path, "manifest.json")
		var manifest Manifest
		if err := json.Unmarshal([]byte(manifestContent), &manifest); err != nil {
			t.Fatalf("unmarshaling manifest: %v", err)
		}
		if manifest.CollectionID == "" {
			t.Error("CollectionID should be auto-generated")
		}
	})

	t.Run("sets CompletedAt timestamp", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0"})

		if err := c.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		manifestContent := readFromZip(t, path, "manifest.json")
		var manifest Manifest
		if err := json.Unmarshal([]byte(manifestContent), &manifest); err != nil {
			t.Fatalf("unmarshaling manifest: %v", err)
		}
		if manifest.CompletedAt.IsZero() {
			t.Error("CompletedAt should be set")
		}
		if manifest.CompletedAt.Before(manifest.CollectedAt) {
			t.Error("CompletedAt should be after CollectedAt")
		}
	})
}
