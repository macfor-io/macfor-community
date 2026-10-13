package container

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	goparquet "github.com/parquet-go/parquet-go"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

// Test constants for common values.
const (
	testZshHistoryPath = "/Users/admin/.zsh_history"
	testUsernameAdmin  = "admin"
	testLogPath        = "logs/shell.history/collection.log"
)

func TestNewCollector(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	coll := NewCollector(c, "shell.history")
	if coll == nil {
		t.Fatal("NewCollector() returned nil")
	}
	if coll.pluginID != "shell.history" {
		t.Errorf("pluginID = %q, want %q", coll.pluginID, "shell.history")
	}
	if coll.container != c {
		t.Error("container reference not set")
	}
}

func TestCollector_AddFile(t *testing.T) {
	t.Run("adds file with correct path", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		c.SetCollectionMetadata(CollectionMeta{
			ToolVersion: "1.0.0",
			SourceType:  "live",
		})

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath
		artifact.Username = testUsernameAdmin

		content := "echo hello\nls -la\n"
		err = coll.AddFile(context.Background(), *artifact, strings.NewReader(content))
		if err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify file was added at the correct path
		expectedPath := artifact.ContainerPath()
		fileContent := readFromZip(t, path, expectedPath)
		if fileContent != content {
			t.Errorf("file content = %q, want %q", fileContent, content)
		}
	})

	t.Run("preserves source modified time", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0", SourceType: "live"})
		coll := NewCollector(c, "shell.history")

		mtime := time.Date(2025, 3, 14, 9, 26, 53, 589793238, time.UTC)
		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath
		artifact.Username = testUsernameAdmin
		artifact.WithOriginalFile(mtime, 11)

		if err := coll.AddFile(context.Background(), *artifact, strings.NewReader("echo hello\n")); err != nil {
			t.Fatalf("AddFile() error = %v", err)
		}
		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		if err := c.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatalf("OpenReader() error = %v", err)
		}
		defer func() { _ = zr.Close() }()

		var found bool
		for _, f := range zr.File {
			if f.Name == artifact.ContainerPath() {
				found = true
				// ZIP headers hold whole seconds.
				if want := mtime.Truncate(time.Second); !f.Modified.Equal(want) {
					t.Errorf("entry Modified = %v, want %v", f.Modified, want)
				}
			} else if f.Modified.Year() < 2000 {
				t.Errorf("generated entry %q is undated: %v", f.Name, f.Modified)
			}
		}
		if !found {
			t.Fatalf("entry %q not found", artifact.ContainerPath())
		}

		var custody ChainOfCustodyLog
		if err := json.Unmarshal([]byte(readFromZip(t, path, "chain-of-custody.json")), &custody); err != nil {
			t.Fatalf("unmarshal custody: %v", err)
		}
		var got any
		for _, e := range custody.Entries {
			if e.Action == CustodyActionFileAdded {
				got = e.Details["source_modified"]
			}
		}
		if want := "2025-03-14T09:26:53.589793238Z"; got != want {
			t.Errorf("custody source_modified = %v, want %s", got, want)
		}
	})

	t.Run("increments files collected count", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath
		artifact.Username = testUsernameAdmin

		_ = coll.AddFile(context.Background(), *artifact, strings.NewReader("content"))

		if coll.FilesCollected() != 1 {
			t.Errorf("FilesCollected() = %d, want 1", coll.FilesCollected())
		}
	})

	t.Run("logs custody entry for file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{
			ToolVersion: "1.0.0",
		})

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath
		artifact.Username = testUsernameAdmin

		_ = coll.AddFile(context.Background(), *artifact, strings.NewReader("content"))

		// Check custody log has file_added entry
		foundFileAdded := false
		for _, entry := range c.custodyLog.Entries {
			if entry.Action == CustodyActionFileAdded {
				foundFileAdded = true
				if entry.Target != artifact.ContainerPath() {
					t.Errorf("custody entry Target = %q, want %q", entry.Target, artifact.ContainerPath())
				}
				if entry.Hash == "" {
					t.Error("custody entry should have hash")
				}
				break
			}
		}
		if !foundFileAdded {
			t.Error("no file_added custody entry found")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err = coll.AddFile(ctx, *artifact, strings.NewReader("content"))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("AddFile() error = %v, want context.Canceled", err)
		}
	})
}

func TestCollector_AddRecord(t *testing.T) {
	t.Run("serializes record as JSONL", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		record := map[string]any{
			"command":   "ls -la",
			"timestamp": "2024-01-01T10:00:00Z",
			"user":      "admin",
		}

		err = coll.AddRecord(context.Background(), *artifact, record)
		if err != nil {
			t.Fatalf("AddRecord() error = %v", err)
		}

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify JSONL content
		recordPath := artifact.RecordPath()
		content := readFromZip(t, path, recordPath)

		var decoded map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &decoded); err != nil {
			t.Fatalf("unmarshaling record: %v", err)
		}

		if decoded["command"] != "ls -la" {
			t.Errorf("command = %v, want %q", decoded["command"], "ls -la")
		}
	})

	t.Run("appends multiple records to same file", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		records := []map[string]string{
			{"command": "echo hello"},
			{"command": "ls -la"},
			{"command": "pwd"},
		}

		for _, rec := range records {
			if err := coll.AddRecord(context.Background(), *artifact, rec); err != nil {
				t.Fatalf("AddRecord() error = %v", err)
			}
		}

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify we have 3 lines of JSONL
		recordPath := artifact.RecordPath()
		content := readFromZip(t, path, recordPath)

		scanner := bufio.NewScanner(strings.NewReader(content))
		lineCount := 0
		for scanner.Scan() {
			lineCount++
			var decoded map[string]string
			if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
				t.Errorf("line %d: failed to unmarshal: %v", lineCount, err)
			}
		}

		if lineCount != 3 {
			t.Errorf("line count = %d, want 3", lineCount)
		}
	})

	t.Run("increments records created count", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		for i := 0; i < 5; i++ {
			_ = coll.AddRecord(context.Background(), *artifact, map[string]int{"i": i})
		}

		if coll.RecordsCreated() != 5 {
			t.Errorf("RecordsCreated() = %d, want 5", coll.RecordsCreated())
		}
	})

	t.Run("handles unmarshalable types", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		// Channels cannot be marshaled
		err = coll.AddRecord(context.Background(), *artifact, make(chan int))
		if err == nil {
			t.Error("expected error for unmarshalable type")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err = coll.AddRecord(ctx, *artifact, map[string]string{"test": "value"})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("AddRecord() error = %v, want context.Canceled", err)
		}
	})
}

func TestCollector_RecordCollectError(t *testing.T) {
	newColl := func(t *testing.T) *Collector {
		t.Helper()
		c, err := NewContainer(Config{OutputPath: filepath.Join(t.TempDir(), "test.zip")})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return NewCollector(c, "browser.safari")
	}

	t.Run("counts each joined error", func(t *testing.T) {
		coll := newColl(t)
		history := fmt.Errorf("history for alice: %w", errors.New("permission denied"))
		cookies := fmt.Errorf("cookies for alice: %w", errors.New("permission denied"))
		nested := errors.Join(errors.New("bookmarks for bob: denied"), errors.New("session for bob: denied"))

		coll.RecordCollectError(errors.Join(history, cookies, nested))

		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		if summary.ErrorsEncountered != 4 {
			t.Errorf("ErrorsEncountered = %d, want 4", summary.ErrorsEncountered)
		}
		if summary.Errors[0].Message != history.Error() {
			t.Errorf("Errors[0].Message = %q, want %q", summary.Errors[0].Message, history.Error())
		}
	})

	t.Run("skips errors already added", func(t *testing.T) {
		coll := newColl(t)
		artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
		base := errors.New("disk I/O error")
		coll.AddError(*artifact, base)

		coll.RecordCollectError(errors.Join(
			fmt.Errorf("history for alice: %w", base),
			errors.New("cookies for alice: permission denied"),
		))

		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		if summary.ErrorsEncountered != 2 {
			t.Errorf("ErrorsEncountered = %d, want 2", summary.ErrorsEncountered)
		}
	})

	t.Run("records a plain error", func(t *testing.T) {
		coll := newColl(t)
		coll.RecordCollectError(errors.New("no users"))
		coll.RecordCollectError(nil)

		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		if summary.ErrorsEncountered != 1 {
			t.Errorf("ErrorsEncountered = %d, want 1", summary.ErrorsEncountered)
		}
	})
}

func TestCollector_AddError(t *testing.T) {
	t.Run("records error in collector", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0"})

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath

		testErr := macfor.ErrPermissionDenied
		coll.AddError(*artifact, testErr)

		if coll.ErrorCount() != 1 {
			t.Errorf("ErrorCount() = %d, want 1", coll.ErrorCount())
		}
	})

	t.Run("adds custody entry for error", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		c.SetCollectionMetadata(CollectionMeta{ToolVersion: "1.0.0"})

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath

		coll.AddError(*artifact, macfor.ErrPermissionDenied)

		foundErrorEntry := false
		for _, entry := range c.custodyLog.Entries {
			if entry.Action == CustodyActionError {
				foundErrorEntry = true
				if entry.Target != testZshHistoryPath {
					t.Errorf("entry Target = %q, want %q", entry.Target, testZshHistoryPath)
				}
				break
			}
		}
		if !foundErrorEntry {
			t.Error("no error custody entry found")
		}
	})

	t.Run("includes error in plugin summary", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.SourcePath = testZshHistoryPath
		artifact.Type = "history"

		coll.AddError(*artifact, macfor.ErrPermissionDenied)

		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if summary.ErrorsEncountered != 1 {
			t.Errorf("ErrorsEncountered = %d, want 1", summary.ErrorsEncountered)
		}
		if len(summary.Errors) != 1 {
			t.Fatalf("Errors length = %d, want 1", len(summary.Errors))
		}
		if summary.Errors[0].Artifact != testZshHistoryPath {
			t.Errorf("Error Artifact = %q, want %q", summary.Errors[0].Artifact, testZshHistoryPath)
		}
	})
}

func TestCollector_Log(t *testing.T) {
	t.Run("writes log entries to collection.log", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		coll := NewCollector(c, "shell.history")

		coll.Log(macfor.LogInfo, "Starting collection for user %s", "admin")
		coll.Log(macfor.LogDebug, "Found file at %s", testZshHistoryPath)
		coll.Log(macfor.LogWarn, "Skipping empty file")

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify log content
		logPath := testLogPath
		logContent := readFromZip(t, path, logPath)

		if !strings.Contains(logContent, "[INFO]") {
			t.Error("log should contain INFO entry")
		}
		if !strings.Contains(logContent, "[DEBUG]") {
			t.Error("log should contain DEBUG entry")
		}
		if !strings.Contains(logContent, "[WARN]") {
			t.Error("log should contain WARN entry")
		}
		if !strings.Contains(logContent, "shell.history") {
			t.Error("log should contain plugin ID")
		}
		if !strings.Contains(logContent, "admin") {
			t.Error("log should contain formatted message content")
		}
	})

	t.Run("log format includes timestamp", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		coll := NewCollector(c, "shell.history")

		coll.Log(macfor.LogInfo, "test message")

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		logPath := testLogPath
		logContent := readFromZip(t, path, logPath)

		// Check for RFC3339 timestamp pattern (e.g., 2024-01-01T10:00:00Z)
		if !strings.Contains(logContent, "T") || !strings.Contains(logContent, ":") {
			t.Error("log should contain RFC3339 timestamp")
		}
	})
}

func TestCollector_Finalize(t *testing.T) {
	t.Run("returns correct plugin summary", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		coll := NewCollector(c, "shell.history")

		// Add some files
		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
		artifact.Username = testUsernameAdmin
		artifact.SourcePath = testZshHistoryPath

		_ = coll.AddFile(context.Background(), *artifact, strings.NewReader("content1"))

		artifact.SourcePath = "/Users/admin/.bash_history"
		_ = coll.AddFile(context.Background(), *artifact, strings.NewReader("content2"))

		// Add some records
		for i := 0; i < 10; i++ {
			_ = coll.AddRecord(context.Background(), *artifact, map[string]int{"line": i})
		}

		// Add an error
		coll.AddError(*artifact, macfor.ErrPermissionDenied)

		summary, err := coll.Finalize()
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if summary.ID != "shell.history" {
			t.Errorf("ID = %q, want %q", summary.ID, "shell.history")
		}
		if summary.FilesCollected != 2 {
			t.Errorf("FilesCollected = %d, want 2", summary.FilesCollected)
		}
		if summary.RecordsCreated != 10 {
			t.Errorf("RecordsCreated = %d, want 10", summary.RecordsCreated)
		}
		if summary.ErrorsEncountered != 1 {
			t.Errorf("ErrorsEncountered = %d, want 1", summary.ErrorsEncountered)
		}
		if summary.StartedAt.IsZero() {
			t.Error("StartedAt should be set")
		}
		if summary.CompletedAt.IsZero() {
			t.Error("CompletedAt should be set")
		}
		if summary.CompletedAt.Before(summary.StartedAt) {
			t.Error("CompletedAt should be after StartedAt")
		}
	})

	t.Run("writes all buffered records", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := filepath.Join(tmpDir, "test.zip")

		c, err := NewContainer(Config{OutputPath: path})
		if err != nil {
			t.Fatalf("NewContainer() error = %v", err)
		}

		coll := NewCollector(c, "shell.history")

		artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")

		for i := 0; i < 100; i++ {
			_ = coll.AddRecord(context.Background(), *artifact, map[string]int{"line": i})
		}

		if _, err := coll.Finalize(); err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}

		if err := c.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		// Verify all records were written
		recordPath := artifact.RecordPath()
		content := readFromZip(t, path, recordPath)

		scanner := bufio.NewScanner(strings.NewReader(content))
		lineCount := 0
		for scanner.Scan() {
			lineCount++
		}

		if lineCount != 100 {
			t.Errorf("line count = %d, want 100", lineCount)
		}
	})
}

func TestCollector_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "shell.history")

	artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
	artifact.SourcePath = "/test/file"

	var wg sync.WaitGroup

	// Concurrent file adds
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			art := *artifact
			art.SourcePath = filepath.Join("/test", string(rune('a'+i)))
			art.Username = "user"
			_ = coll.AddFile(context.Background(), art, strings.NewReader("content"))
		}(i)
	}

	// Concurrent record adds
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = coll.AddRecord(context.Background(), *artifact, map[string]int{"i": i})
		}(i)
	}

	// Concurrent log writes
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			coll.Log(macfor.LogInfo, "concurrent log %d", i)
		}(i)
	}

	// Concurrent error adds
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			coll.AddError(*artifact, macfor.ErrPermissionDenied)
		}()
	}

	wg.Wait()

	// Verify counts
	if coll.FilesCollected() != 10 {
		t.Errorf("FilesCollected() = %d, want 10", coll.FilesCollected())
	}
	if coll.RecordsCreated() != 50 {
		t.Errorf("RecordsCreated() = %d, want 50", coll.RecordsCreated())
	}
	if coll.ErrorCount() != 5 {
		t.Errorf("ErrorCount() = %d, want 5", coll.ErrorCount())
	}

	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if summary.FilesCollected != 10 {
		t.Errorf("summary.FilesCollected = %d, want 10", summary.FilesCollected)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestCollector_ImplementsInterface(_ *testing.T) {
	// Compile-time check that Collector implements macfor.Collector
	var _ macfor.Collector = (*Collector)(nil)
}

func TestCollector_MultipleRecordPaths(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "browser.safari")

	historyArtifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	cookiesArtifact, _ := macfor.NewArtifact("browser.safari", "browser", "cookies")

	// Add records to different artifact types
	for i := 0; i < 5; i++ {
		_ = coll.AddRecord(context.Background(), *historyArtifact, map[string]string{"url": "http://example.com"})
		_ = coll.AddRecord(context.Background(), *cookiesArtifact, map[string]string{"cookie": "session=abc"})
	}

	if _, err := coll.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify both record files were created
	historyPath := historyArtifact.RecordPath()
	cookiesPath := cookiesArtifact.RecordPath()

	// Open the zip and check for both files
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	foundHistory := false
	foundCookies := false
	for _, f := range zr.File {
		if f.Name == historyPath {
			foundHistory = true
		}
		if f.Name == cookiesPath {
			foundCookies = true
		}
	}

	if !foundHistory {
		t.Errorf("history records file not found at %s", historyPath)
	}
	if !foundCookies {
		t.Errorf("cookies records file not found at %s", cookiesPath)
	}
}

func TestCollector_NoRecordsNoLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "shell.history")

	// Don't add any records or logs
	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if summary.FilesCollected != 0 {
		t.Errorf("FilesCollected = %d, want 0", summary.FilesCollected)
	}
	if summary.RecordsCreated != 0 {
		t.Errorf("RecordsCreated = %d, want 0", summary.RecordsCreated)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify no log file was created
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	logPath := testLogPath
	for _, f := range zr.File {
		if f.Name == logPath {
			// Read the file to see if it's empty
			rc, _ := f.Open()
			limited := io.LimitReader(rc, 1024)
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, limited)
			_ = rc.Close()
			// If there's content, that's an error
			if buf.Len() > 0 {
				t.Error("log file should not be created when no logs are written")
			}
			return
		}
	}
	// No log file found is expected
}

func TestCollector_Finalize_TimestampAccuracy(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	before := time.Now()
	coll := NewCollector(c, "shell.history")
	time.Sleep(10 * time.Millisecond) // Small delay to ensure time difference

	summary, _ := coll.Finalize()

	if summary.StartedAt.Before(before) {
		t.Error("StartedAt should be after test started")
	}
	if summary.CompletedAt.Before(summary.StartedAt) {
		t.Error("CompletedAt should be after StartedAt")
	}
}

// --- Parquet Integration Tests ---

type parquetIntTestRecord struct {
	URL   string `parquet:"url" json:"url"`
	Title string `parquet:"title" json:"title"`
	Count int64  `parquet:"count" json:"count"`
}

func TestCollector_ParquetFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{
		OutputPath:         path,
		RecordFormat:       macforparquet.FormatParquet,
		ParquetCompression: "snappy",
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	artifact.Username = "testuser"

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		rec := parquetIntTestRecord{URL: "http://example.com", Title: "Test", Count: int64(i)}
		if err := coll.AddRecord(ctx, *artifact, rec); err != nil {
			t.Fatalf("AddRecord() error = %v", err)
		}
	}

	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if summary.RecordsCreated != 5 {
		t.Errorf("RecordsCreated = %d, want 5", summary.RecordsCreated)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Verify Parquet file exists.
	content := readFromZip(t, path, "records/browser.safari/history.parquet")
	if len(content) == 0 {
		t.Fatal("Parquet file is empty")
	}

	// Verify it's valid Parquet and has correct row count.
	file, err := goparquet.OpenFile(bytes.NewReader([]byte(content)), int64(len(content)))
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	rowCount := int64(0)
	for _, rg := range file.RowGroups() {
		rowCount += rg.NumRows()
	}
	if rowCount != 5 {
		t.Errorf("Parquet row count = %d, want 5", rowCount)
	}

	// Verify no JSONL file exists.
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer func() { _ = zr.Close() }()
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".jsonl") && strings.Contains(f.Name, "browser.safari") {
			t.Errorf("JSONL file should not exist in Parquet-only mode: %s", f.Name)
		}
	}
}

func TestCollector_ParquetHashes(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{
		OutputPath:   path,
		RecordFormat: macforparquet.FormatParquet,
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")

	ctx := context.Background()
	_ = coll.AddRecord(ctx, *artifact, parquetIntTestRecord{URL: "http://example.com"})
	_, _ = coll.Finalize()

	// Get hashes before closing.
	hashes := c.Hashes()
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	parquetPath := "records/browser.safari/history.parquet"
	if _, ok := hashes[parquetPath]; !ok {
		t.Errorf("Parquet file %q not found in hashes", parquetPath)
	}
}

func TestCollector_ParquetManifest(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{
		OutputPath:   path,
		RecordFormat: macforparquet.FormatParquet,
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	c.SetCollectionMetadata(CollectionMeta{
		ToolVersion: "1.0.0-test",
		SourceType:  "live",
		Hostname:    "test-host",
	})

	if err := c.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read manifest.
	manifestJSON := readFromZip(t, path, "manifest.json")
	var manifest map[string]any
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		t.Fatalf("Unmarshal manifest error = %v", err)
	}

	format, ok := manifest["record_format"].(string)
	if !ok || format != "parquet" {
		t.Errorf("manifest record_format = %v, want %q", manifest["record_format"], "parquet")
	}
}

func TestCollector_BothFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{
		OutputPath:         path,
		RecordFormat:       macforparquet.FormatBoth,
		ParquetCompression: "snappy",
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := NewCollector(c, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		rec := parquetIntTestRecord{URL: "http://example.com", Title: "Test", Count: int64(i)}
		_ = coll.AddRecord(ctx, *artifact, rec)
	}

	_, _ = coll.Finalize()
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Both JSONL and Parquet files should exist.
	jsonlContent := readFromZip(t, path, "records/browser.safari/parsed.jsonl")
	lines := strings.Split(strings.TrimSpace(jsonlContent), "\n")
	if len(lines) != 3 {
		t.Errorf("JSONL lines = %d, want 3", len(lines))
	}

	parquetContent := readFromZip(t, path, "records/browser.safari/history.parquet")
	if len(parquetContent) == 0 {
		t.Fatal("Parquet file is empty")
	}
}
