package container

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

func TestBothRecordWriter_WriteAndFinalize(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	w := newBothRecordWriter("shell.history", "snappy")
	artifact, _ := macfor.NewArtifact("shell.history", "shell", "history")
	artifact.Username = "testuser"

	ts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	records := []testParquetRecord{
		{Command: "ls -la", Timestamp: ts, User: "admin", LineNum: 1},
		{Command: "cd /tmp", Timestamp: ts.Add(time.Second), User: "admin", LineNum: 2},
		{Command: "pwd", Timestamp: ts.Add(2 * time.Second), User: "admin", LineNum: 3},
	}

	for _, rec := range records {
		if err := w.WriteRecord(*artifact, rec); err != nil {
			t.Fatalf("WriteRecord() error = %v", err)
		}
	}

	count, err := w.Finalize(c)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if count != 3 {
		t.Errorf("Finalize() count = %d, want 3", count)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Both JSONL and Parquet files should exist.
	jsonlPath := "records/shell.history/parsed.jsonl"
	jsonlContent := readFromZip(t, path, jsonlPath)
	if len(jsonlContent) == 0 {
		t.Error("JSONL file is empty")
	}

	// Verify JSONL has 3 lines.
	lines := strings.Split(strings.TrimSpace(jsonlContent), "\n")
	if len(lines) != 3 {
		t.Errorf("JSONL lines = %d, want 3", len(lines))
	}

	parquetPath := "records/shell.history/history.parquet"
	parquetContent := readFromZip(t, path, parquetPath)
	if len(parquetContent) == 0 {
		t.Error("Parquet file is empty")
	}
}

func TestBothRecordWriter_EmptyFinalize(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	w := newBothRecordWriter("test.plugin", "snappy")

	count, err := w.Finalize(c)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestBothRecordWriter_NonStructRejection(t *testing.T) {
	w := newBothRecordWriter("test.plugin", "snappy")
	artifact, _ := macfor.NewArtifact("test.plugin", "test", "data")

	// Non-struct should fail on the JSONL writer (which marshals JSON first).
	// The parquet writer would also reject it, but JSONL goes first.
	err := w.WriteRecord(*artifact, "not a struct")
	if err == nil {
		t.Error("expected error for non-struct record")
	}
}
