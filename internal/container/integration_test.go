package container

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

const testHostname = "test-host"

// Integration tests that verify the container and collector work together
// with real file operations and ZIP archive generation.

func TestIntegration_ContainerLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "test-evidence.zip")

	t.Run("full container lifecycle", func(t *testing.T) {
		// Create container
		cont, err := NewContainer(Config{OutputPath: outputPath})
		if err != nil {
			t.Fatalf("failed to create container: %v", err)
		}

		// Set metadata
		cont.SetCollectionMetadata(CollectionMeta{
			ToolVersion:  "1.0.0",
			SourceType:   "live",
			Hostname:     testHostname,
			OSVersion:    "14.0",
			CollectionID: "test-collection-id",
		})

		// Add some files
		testContent := []byte("test file content")
		err = cont.AddFile("artifacts/test/file1.txt", bytes.NewReader(testContent))
		if err != nil {
			t.Fatalf("failed to add file: %v", err)
		}

		// Add JSON
		testData := map[string]any{
			"key":   "value",
			"count": 42,
		}
		err = cont.AddJSON("data/test.json", testData)
		if err != nil {
			t.Fatalf("failed to add JSON: %v", err)
		}

		// Add custody entry
		cont.AddCustodyEntry(ChainOfCustodyEntry{
			Timestamp: time.Now(),
			Action:    CustodyActionFileAdded,
			Actor:     "test",
			Target:    "artifacts/test/file1.txt",
		})

		// Add plugin summary
		cont.AddPluginSummary(PluginSummary{
			ID:                "test.plugin",
			Name:              "Test Plugin",
			Version:           "1.0.0",
			FilesCollected:    1,
			RecordsCreated:    0,
			ErrorsEncountered: 0,
		})

		// Finalize
		err = cont.Finalize()
		if err != nil {
			t.Fatalf("failed to finalize: %v", err)
		}

		// Close
		err = cont.Close()
		if err != nil {
			t.Fatalf("failed to close: %v", err)
		}

		// Verify the container exists
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Fatal("container file does not exist")
		}

		// Open and verify contents
		zr, err := zip.OpenReader(outputPath)
		if err != nil {
			t.Fatalf("failed to open container: %v", err)
		}
		defer func() { _ = zr.Close() }()

		// Build file map
		files := make(map[string]*zip.File)
		for _, f := range zr.File {
			files[f.Name] = f
		}

		// Verify expected files exist
		expectedFiles := []string{
			"artifacts/test/file1.txt",
			"data/test.json",
			"manifest.json",
			"chain-of-custody.json",
			"hashes.json",
		}

		for _, name := range expectedFiles {
			if _, ok := files[name]; !ok {
				t.Errorf("expected file %s not found in container", name)
			}
		}

		// Verify file content
		data, err := readZipFile(files["artifacts/test/file1.txt"])
		if err != nil {
			t.Fatalf("failed to read test file: %v", err)
		}
		if !bytes.Equal(data, testContent) {
			t.Error("test file content mismatch")
		}

		// Verify hashes
		hashData, err := readZipFile(files["hashes.json"])
		if err != nil {
			t.Fatalf("failed to read hashes.json: %v", err)
		}

		var hashManifest HashManifest
		if err := json.Unmarshal(hashData, &hashManifest); err != nil {
			t.Fatalf("failed to parse hashes.json: %v", err)
		}

		// Verify the hash for our test file
		expectedHash := sha256Hash(testContent)
		if hash, ok := hashManifest.Files["artifacts/test/file1.txt"]; !ok {
			t.Error("test file hash not found in manifest")
		} else if hash != expectedHash {
			t.Errorf("hash mismatch: expected %s, got %s", expectedHash, hash)
		}

		// Verify manifest
		manifestData, err := readZipFile(files["manifest.json"])
		if err != nil {
			t.Fatalf("failed to read manifest.json: %v", err)
		}

		var manifest Manifest
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			t.Fatalf("failed to parse manifest.json: %v", err)
		}

		if manifest.CollectionID != "test-collection-id" {
			t.Errorf("expected collection ID 'test-collection-id', got '%s'", manifest.CollectionID)
		}
		if manifest.ToolVersion != "1.0.0" {
			t.Errorf("expected tool version '1.0.0', got '%s'", manifest.ToolVersion)
		}
		if manifest.Source.Hostname != testHostname {
			t.Errorf("expected hostname '%s', got '%s'", testHostname, manifest.Source.Hostname)
		}
		if len(manifest.Plugins) != 1 {
			t.Errorf("expected 1 plugin, got %d", len(manifest.Plugins))
		}
	})
}

//nolint:gocyclo // Integration test with many assertions and subtests.
func TestIntegration_CollectorWithContainer(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "collector-test.zip")

	cont, err := NewContainer(Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(CollectionMeta{
		ToolVersion: "1.0.0",
		SourceType:  "live",
	})

	t.Run("collector file and record operations", func(t *testing.T) {
		coll := NewCollector(cont, "test.plugin")

		// Create test artifact
		artifact, err := macfor.NewArtifact("test.plugin", "test", "history")
		if err != nil {
			t.Fatalf("failed to create artifact: %v", err)
		}
		artifact.SourcePath = "/Users/testuser/.test_history"
		artifact.Username = "testuser"

		// Add a file
		fileContent := []byte("command1\ncommand2\ncommand3\n")
		err = coll.AddFile(context.Background(), *artifact, bytes.NewReader(fileContent))
		if err != nil {
			t.Fatalf("failed to add file: %v", err)
		}

		// Verify file count
		if coll.FilesCollected() != 1 {
			t.Errorf("expected 1 file, got %d", coll.FilesCollected())
		}

		// Add records
		records := []map[string]any{
			{"command": "ls -la", "line": 1},
			{"command": "pwd", "line": 2},
			{"command": "cd ..", "line": 3},
		}

		recordArtifact, _ := macfor.NewArtifact("test.plugin", "test", "parsed")
		recordArtifact.Username = "testuser"

		for _, r := range records {
			err = coll.AddRecord(context.Background(), *recordArtifact, r)
			if err != nil {
				t.Fatalf("failed to add record: %v", err)
			}
		}

		// Verify record count
		if coll.RecordsCreated() != 3 {
			t.Errorf("expected 3 records, got %d", coll.RecordsCreated())
		}

		// Log some messages
		coll.Log(macfor.LogInfo, "Test info message")
		coll.Log(macfor.LogWarn, "Test warning message")

		// Finalize collector
		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("failed to finalize collector: %v", err)
		}

		// Verify summary
		if summary.ID != "test.plugin" {
			t.Errorf("expected plugin ID 'test.plugin', got '%s'", summary.ID)
		}
		if summary.FilesCollected != 1 {
			t.Errorf("expected 1 file, got %d", summary.FilesCollected)
		}
		if summary.RecordsCreated != 3 {
			t.Errorf("expected 3 records, got %d", summary.RecordsCreated)
		}
		if summary.ErrorsEncountered != 0 {
			t.Errorf("expected 0 errors, got %d", summary.ErrorsEncountered)
		}

		// Add summary to container
		cont.AddPluginSummary(summary)
	})

	// Finalize and close container
	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize container: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Verify the contents
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	t.Run("verify artifacts in container", func(t *testing.T) {
		// Find artifact file
		var artifactFile *zip.File
		for name, f := range files {
			if strings.HasPrefix(name, "artifacts/") && strings.HasSuffix(name, "history") {
				artifactFile = f
				break
			}
		}

		if artifactFile == nil {
			t.Fatal("artifact file not found in container")
		}

		data, err := readZipFile(artifactFile)
		if err != nil {
			t.Fatalf("failed to read artifact: %v", err)
		}

		if !bytes.Contains(data, []byte("command1")) {
			t.Error("artifact does not contain expected content")
		}
	})

	t.Run("verify records in container", func(t *testing.T) {
		// Find record file
		var recordFile *zip.File
		for name, f := range files {
			if strings.HasPrefix(name, "records/") {
				recordFile = f
				t.Logf("Found record file: %s", name)
				break
			}
		}

		if recordFile == nil {
			t.Fatal("record file not found in container")
		}

		data, err := readZipFile(recordFile)
		if err != nil {
			t.Fatalf("failed to read records: %v", err)
		}

		// JSONL format - each line is a JSON object
		lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
		if len(lines) != 3 {
			t.Errorf("expected 3 record lines, got %d", len(lines))
		}

		// Verify first record
		var firstRecord map[string]any
		if err := json.Unmarshal(lines[0], &firstRecord); err != nil {
			t.Fatalf("failed to parse first record: %v", err)
		}

		if firstRecord["command"] != "ls -la" {
			t.Errorf("expected command 'ls -la', got '%v'", firstRecord["command"])
		}
	})

	t.Run("verify logs in container", func(t *testing.T) {
		logFile, ok := files["logs/test.plugin/collection.log"]
		if !ok {
			t.Fatal("collection log not found")
		}

		data, err := readZipFile(logFile)
		if err != nil {
			t.Fatalf("failed to read log: %v", err)
		}

		if !bytes.Contains(data, []byte("Test info message")) {
			t.Error("log does not contain info message")
		}
		if !bytes.Contains(data, []byte("Test warning message")) {
			t.Error("log does not contain warning message")
		}
	})
}

func TestIntegration_CollectorErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "error-test.zip")

	cont, err := NewContainer(Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(CollectionMeta{
		ToolVersion: "1.0.0",
	})

	coll := NewCollector(cont, "test.plugin")

	t.Run("add error to collector", func(t *testing.T) {
		artifact, _ := macfor.NewArtifact("test.plugin", "test", "file")
		artifact.SourcePath = "/nonexistent/path"

		// Add an error
		coll.AddError(*artifact, os.ErrNotExist)

		if coll.ErrorCount() != 1 {
			t.Errorf("expected 1 error, got %d", coll.ErrorCount())
		}
	})

	t.Run("errors included in summary", func(t *testing.T) {
		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("failed to finalize: %v", err)
		}

		if summary.ErrorsEncountered != 1 {
			t.Errorf("expected 1 error in summary, got %d", summary.ErrorsEncountered)
		}

		if len(summary.Errors) != 1 {
			t.Errorf("expected 1 error detail, got %d", len(summary.Errors))
		}

		cont.AddPluginSummary(summary)
	})

	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize container: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Verify errors are in custody log
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	custodyData, err := readZipFile(files["chain-of-custody.json"])
	if err != nil {
		t.Fatalf("failed to read custody log: %v", err)
	}

	var custody ChainOfCustodyLog
	if err := json.Unmarshal(custodyData, &custody); err != nil {
		t.Fatalf("failed to parse custody log: %v", err)
	}

	// Should have an error custody entry
	hasErrorEntry := false
	for _, entry := range custody.Entries {
		if entry.Action == CustodyActionError {
			hasErrorEntry = true
			break
		}
	}

	if !hasErrorEntry {
		t.Error("custody log should contain error entry")
	}
}

func TestIntegration_ContainerCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "cancel-test.zip")

	cont, err := NewContainer(Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(CollectionMeta{
		ToolVersion: "1.0.0",
	})

	coll := NewCollector(cont, "test.plugin")

	t.Run("cancellation during AddFile", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		artifact, _ := macfor.NewArtifact("test.plugin", "test", "file")
		err := coll.AddFile(ctx, *artifact, bytes.NewReader([]byte("test")))

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	t.Run("cancellation during AddRecord", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		artifact, _ := macfor.NewArtifact("test.plugin", "test", "record")
		err := coll.AddRecord(ctx, *artifact, map[string]any{"test": "data"})

		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})

	// Clean up
	cont.MarkError()
	_ = cont.Close()
}

func TestIntegration_HashVerification(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "hash-test.zip")

	cont, err := NewContainer(Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Add multiple files with known content
	testFiles := map[string][]byte{
		"file1.txt": []byte("content of file 1"),
		"file2.txt": []byte("content of file 2"),
		"file3.bin": bytes.Repeat([]byte{0x00, 0xFF}, 100),
	}

	for name, content := range testFiles {
		if err := cont.AddFile("test/"+name, bytes.NewReader(content)); err != nil {
			t.Fatalf("failed to add %s: %v", name, err)
		}
	}

	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Open and verify all hashes
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	hashData, err := readZipFile(files["hashes.json"])
	if err != nil {
		t.Fatalf("failed to read hashes.json: %v", err)
	}

	var hashManifest HashManifest
	if err := json.Unmarshal(hashData, &hashManifest); err != nil {
		t.Fatalf("failed to parse hashes.json: %v", err)
	}

	// Verify each file hash
	for name, content := range testFiles {
		path := "test/" + name
		expectedHash := sha256Hash(content)

		actualHash, ok := hashManifest.Files[path]
		if !ok {
			t.Errorf("hash for %s not found", path)
			continue
		}

		if actualHash != expectedHash {
			t.Errorf("hash mismatch for %s: expected %s, got %s", path, expectedHash, actualHash)
		}

		// Also verify by reading the actual file content
		data, err := readZipFile(files[path])
		if err != nil {
			t.Errorf("failed to read %s: %v", path, err)
			continue
		}

		recomputedHash := sha256Hash(data)
		if recomputedHash != expectedHash {
			t.Errorf("recomputed hash mismatch for %s: expected %s, got %s", path, expectedHash, recomputedHash)
		}
	}
}

func TestIntegration_NestedDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "nested-test.zip")

	cont, err := NewContainer(Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Add files in deeply nested directories
	nestedPaths := []string{
		"artifacts/shell/shell.history/files/user1/.zsh_history",
		"artifacts/shell/shell.history/files/user2/.bash_history",
		"records/shell.history/parsed.jsonl",
		"logs/shell.history/collection.log",
	}

	for _, path := range nestedPaths {
		content := []byte("content for " + path)
		if err := cont.AddFile(path, bytes.NewReader(content)); err != nil {
			t.Fatalf("failed to add %s: %v", path, err)
		}
	}

	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	// Verify all paths exist
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	for _, path := range nestedPaths {
		if _, ok := files[path]; !ok {
			t.Errorf("nested path %s not found in container", path)
		}
	}
}

// readZipFile reads the contents of a file in a ZIP archive.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// sha256Hash computes the SHA-256 hash of data and returns it as a hex string.
func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
