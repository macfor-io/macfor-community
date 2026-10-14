package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	goparquet "github.com/parquet-go/parquet-go"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

type e2eRecord struct {
	URL       string    `parquet:"url" json:"url"`
	Title     string    `parquet:"title" json:"title"`
	Timestamp time.Time `parquet:"timestamp" json:"timestamp"`
	Count     int64     `parquet:"count" json:"count"`
}

func TestE2E_ParquetCollection(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "parquet-test.zip")

	cont, err := container.NewContainer(container.Config{
		OutputPath:         outputPath,
		RecordFormat:       macforparquet.FormatParquet,
		ParquetCompression: "snappy",
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	cont.SetCollectionMetadata(container.CollectionMeta{
		ToolVersion: "1.0.0-e2e-test",
		SourceType:  "live",
		Hostname:    "test-host",
	})

	// Simulate a plugin writing records.
	coll := container.NewCollector(cont, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	artifact.Username = "testuser"

	ctx := context.Background()
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		rec := e2eRecord{
			URL:       "https://example.com/page",
			Title:     "Example",
			Timestamp: ts.Add(time.Duration(i) * time.Minute),
			Count:     int64(i + 1),
		}
		if err := coll.AddRecord(ctx, *artifact, rec); err != nil {
			t.Fatalf("AddRecord() error = %v", err)
		}
	}

	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	cont.AddPluginSummary(summary)

	if err := cont.Finalize(); err != nil {
		t.Fatalf("Container.Finalize() error = %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Open the ZIP and verify structure.
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer func() { _ = zr.Close() }()

	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	// Parquet file should exist.
	parquetPath := "records/browser.safari/history.parquet"
	if !fileNames[parquetPath] {
		t.Errorf("missing %s in container", parquetPath)
	}

	// JSONL file should NOT exist.
	for name := range fileNames {
		if strings.HasSuffix(name, ".jsonl") && strings.Contains(name, "browser.safari") {
			t.Errorf("JSONL file should not exist in parquet-only mode: %s", name)
		}
	}

	// manifest.json should exist.
	if !fileNames["manifest.json"] {
		t.Fatal("missing manifest.json")
	}

	// hashes.json should exist.
	if !fileNames["hashes.json"] {
		t.Fatal("missing hashes.json")
	}

	// Verify manifest contents.
	manifestData := readZipEntry(t, zr, "manifest.json")
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("Unmarshal manifest error = %v", err)
	}
	if manifest["record_format"] != "parquet" {
		t.Errorf("manifest record_format = %v, want parquet", manifest["record_format"])
	}
	if manifest["version"] != "1.4" {
		t.Errorf("manifest version = %v, want 1.2", manifest["version"])
	}

	// Verify hashes.json contains the Parquet file.
	hashesData := readZipEntry(t, zr, "hashes.json")
	var hashes map[string]any
	if err := json.Unmarshal(hashesData, &hashes); err != nil {
		t.Fatalf("Unmarshal hashes error = %v", err)
	}
	files, ok := hashes["files"].(map[string]any)
	if !ok {
		t.Fatal("hashes.json missing files map")
	}
	if _, ok := files[parquetPath]; !ok {
		t.Errorf("Parquet file %q not in hashes.json", parquetPath)
	}

	// Read back Parquet data and verify.
	parquetData := readZipEntry(t, zr, parquetPath)
	file, err := goparquet.OpenFile(bytes.NewReader(parquetData), int64(len(parquetData)))
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	rowCount := int64(0)
	for _, rg := range file.RowGroups() {
		rowCount += rg.NumRows()
	}
	if rowCount != 10 {
		t.Errorf("Parquet row count = %d, want 10", rowCount)
	}
}

func TestE2E_BothCollection(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "both-test.zip")

	cont, err := container.NewContainer(container.Config{
		OutputPath:         outputPath,
		RecordFormat:       macforparquet.FormatBoth,
		ParquetCompression: "snappy",
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := container.NewCollector(cont, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		rec := e2eRecord{URL: "http://example.com", Title: "Test", Count: int64(i)}
		_ = coll.AddRecord(ctx, *artifact, rec)
	}

	_, _ = coll.Finalize()
	_ = cont.Close()

	// Both formats should be present.
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer func() { _ = zr.Close() }()

	hasJSONL := false
	hasParquet := false
	for _, f := range zr.File {
		if f.Name == "records/browser.safari/parsed.jsonl" {
			hasJSONL = true
		}
		if f.Name == "records/browser.safari/history.parquet" {
			hasParquet = true
		}
	}

	if !hasJSONL {
		t.Error("missing JSONL file in both mode")
	}
	if !hasParquet {
		t.Error("missing Parquet file in both mode")
	}
}

func TestE2E_DefaultJSONL(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "jsonl-test.zip")

	// No RecordFormat specified — should default to JSONL.
	cont, err := container.NewContainer(container.Config{
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	coll := container.NewCollector(cont, "browser.safari")
	artifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		rec := e2eRecord{URL: "http://example.com", Title: "Test", Count: int64(i)}
		_ = coll.AddRecord(ctx, *artifact, rec)
	}

	_, _ = coll.Finalize()
	_ = cont.Close()

	// Only JSONL should be present.
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	defer func() { _ = zr.Close() }()

	hasJSONL := false
	for _, f := range zr.File {
		if f.Name == "records/browser.safari/parsed.jsonl" {
			hasJSONL = true
		}
		if strings.HasSuffix(f.Name, ".parquet") {
			t.Errorf("Parquet file should not exist in default JSONL mode: %s", f.Name)
		}
	}

	if !hasJSONL {
		t.Error("missing JSONL file in default mode")
	}
}

// readZipEntry reads a file from an opened zip reader.
func readZipEntry(t *testing.T, zr *zip.ReadCloser, name string) []byte {
	t.Helper()
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("opening %s: %v", name, err)
			}
			defer func() { _ = rc.Close() }()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatalf("reading %s: %v", name, err)
			}
			return buf.Bytes()
		}
	}
	t.Fatalf("file %s not found in ZIP", name)
	return nil
}
