package container

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	goparquet "github.com/parquet-go/parquet-go"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

const (
	testPluginShellHistory  = "shell.history"
	testArtifactTypeHistory = "history"
)

type testParquetRecord struct {
	Command   string    `parquet:"command" json:"command"`
	Timestamp time.Time `parquet:"timestamp" json:"timestamp"`
	User      string    `parquet:"user" json:"user"`
	LineNum   int64     `parquet:"line_num" json:"line_num"`
}

func makeTestParquetArtifact() macfor.Artifact {
	a, _ := macfor.NewArtifact(testPluginShellHistory, "shell", testArtifactTypeHistory)
	return *a
}

func TestParquetRecordWriter_WriteAndFinalize(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	w := newParquetRecordWriter(testPluginShellHistory, "snappy")
	artifact := makeTestParquetArtifact()

	ts := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	records := []testParquetRecord{
		{Command: "ls -la", Timestamp: ts, User: "admin", LineNum: 1},
		{Command: "cd /tmp", Timestamp: ts.Add(time.Second), User: "admin", LineNum: 2},
		{Command: "pwd", Timestamp: ts.Add(2 * time.Second), User: "admin", LineNum: 3},
	}

	for _, rec := range records {
		if err := w.WriteRecord(artifact, rec); err != nil {
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

	// Close container to flush zip.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read back the Parquet file from the zip.
	parquetPath := "records/shell.history/history.parquet"
	content := readFromZip(t, path, parquetPath)
	if len(content) == 0 {
		t.Fatal("Parquet file is empty")
	}

	// Verify it's valid Parquet by reading with parquet-go.
	file, err := goparquet.OpenFile(bytes.NewReader([]byte(content)), int64(len(content)))
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	rowCount := int64(0)
	for _, rg := range file.RowGroups() {
		rowCount += rg.NumRows()
	}
	if rowCount != 3 {
		t.Errorf("Parquet row count = %d, want 3", rowCount)
	}
}

// mergedTestRecord includes common fields for reading back merged Parquet data.
type mergedTestRecord struct {
	ArtifactType string    `parquet:"_artifact_type"`
	PluginID     string    `parquet:"_plugin_id"`
	Username     string    `parquet:"_username"`
	CollectedAt  time.Time `parquet:"_collected_at,timestamp(microsecond)"`
	Command      string    `parquet:"command"`
	Timestamp    time.Time `parquet:"timestamp"`
	User         string    `parquet:"user"`
	LineNum      int64     `parquet:"line_num"`
}

func TestParquetRecordWriter_ReadBack(t *testing.T) {
	w := newParquetRecordWriter(testPluginShellHistory, "snappy")
	artifact := makeTestParquetArtifact()
	artifact.Username = "testuser"

	ts := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	rec := testParquetRecord{Command: "echo hello", Timestamp: ts, User: "root", LineNum: 42}

	if err := w.WriteRecord(artifact, rec); err != nil {
		t.Fatalf("WriteRecord() error = %v", err)
	}

	// Encode directly.
	buf := w.writers[testArtifactTypeHistory]
	data, err := w.encodeParquet(buf)
	if err != nil {
		t.Fatalf("encodeParquet() error = %v", err)
	}

	// Read back with merged type reader.
	reader := goparquet.NewGenericReader[mergedTestRecord](bytes.NewReader(data))
	defer func() { _ = reader.Close() }()

	decoded := make([]mergedTestRecord, 1)
	n, err := reader.Read(decoded)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("Read() n = %d, want 1", n)
	}

	if decoded[0].Command != "echo hello" {
		t.Errorf("Command = %q, want %q", decoded[0].Command, "echo hello")
	}
	if decoded[0].User != "root" {
		t.Errorf("User = %q, want %q", decoded[0].User, "root")
	}
	if decoded[0].LineNum != 42 {
		t.Errorf("LineNum = %d, want 42", decoded[0].LineNum)
	}
}

func TestParquetRecordWriter_CommonFields(t *testing.T) {
	w := newParquetRecordWriter(testPluginShellHistory, "snappy")
	artifact := makeTestParquetArtifact()
	artifact.Username = "forensic_user"

	rec := testParquetRecord{Command: "test", User: "admin", LineNum: 1}
	_ = w.WriteRecord(artifact, rec)

	buf := w.writers[testArtifactTypeHistory]
	data, err := w.encodeParquet(buf)
	if err != nil {
		t.Fatalf("encodeParquet() error = %v", err)
	}

	reader := goparquet.NewGenericReader[mergedTestRecord](bytes.NewReader(data))
	defer func() { _ = reader.Close() }()

	decoded := make([]mergedTestRecord, 1)
	n, err := reader.Read(decoded)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("Read() n = %d, want 1", n)
	}

	// Verify common fields.
	if decoded[0].ArtifactType != testArtifactTypeHistory {
		t.Errorf("_artifact_type = %q, want %q", decoded[0].ArtifactType, testArtifactTypeHistory)
	}
	if decoded[0].PluginID != testPluginShellHistory {
		t.Errorf("_plugin_id = %q, want %q", decoded[0].PluginID, testPluginShellHistory)
	}
	if decoded[0].Username != "forensic_user" {
		t.Errorf("_username = %q, want %q", decoded[0].Username, "forensic_user")
	}
	if decoded[0].CollectedAt.IsZero() {
		t.Error("_collected_at should not be zero")
	}

	// Verify plugin fields still present.
	if decoded[0].Command != "test" {
		t.Errorf("command = %q, want %q", decoded[0].Command, "test")
	}
}

// envelopeNamedRecord has fields whose Go names match the common fields the
// writer prepends to every record.
type envelopeNamedRecord struct {
	ArtifactType string    `json:"artifact_type"`
	PluginID     string    `json:"plugin_id"`
	Username     string    `json:"username"`
	CollectedAt  time.Time `json:"collected_at"`
}

// TestParquetRecordWriter_EnvelopeNamedFields guards against a reflect.StructOf
// duplicate-field panic when a plugin record has a field named like one of the
// common fields (e.g. Username), and checks both values are kept.
func TestParquetRecordWriter_EnvelopeNamedFields(t *testing.T) {
	w := newParquetRecordWriter("system.users", "snappy")
	a, _ := macfor.NewArtifact("system.users", "system", "user_accounts")
	a.Username = "envelope_user"

	rec := envelopeNamedRecord{ArtifactType: "rec_type", PluginID: "rec_plugin", Username: "alice"}
	if err := w.WriteRecord(*a, rec); err != nil {
		t.Fatalf("WriteRecord() error = %v", err)
	}

	data, err := w.encodeParquet(w.writers["user_accounts"])
	if err != nil {
		t.Fatalf("encodeParquet() error = %v", err)
	}

	type decodedRecord struct {
		EnvUsername string `parquet:"_username"`
		Username    string `parquet:"Username"`
	}
	reader := goparquet.NewGenericReader[decodedRecord](bytes.NewReader(data))
	defer func() { _ = reader.Close() }()

	decoded := make([]decodedRecord, 1)
	if n, err := reader.Read(decoded); n != 1 || (err != nil && !errors.Is(err, io.EOF)) {
		t.Fatalf("Read() n = %d, err = %v", n, err)
	}
	if decoded[0].EnvUsername != "envelope_user" {
		t.Errorf("_username = %q, want envelope_user", decoded[0].EnvUsername)
	}
	if decoded[0].Username != "alice" {
		t.Errorf("Username = %q, want alice", decoded[0].Username)
	}
}

func TestParquetRecordWriter_MultipleArtifactTypes(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}

	w := newParquetRecordWriter("browser.safari", "snappy")

	historyArtifact, _ := macfor.NewArtifact("browser.safari", "browser", "history")
	cookieArtifact, _ := macfor.NewArtifact("browser.safari", "browser", "cookies")

	type historyRec struct {
		URL   string `parquet:"url"`
		Title string `parquet:"title"`
	}
	type cookieRec struct {
		Domain string `parquet:"domain"`
		Name   string `parquet:"name"`
	}

	_ = w.WriteRecord(*historyArtifact, historyRec{URL: "http://example.com", Title: "Example"})
	_ = w.WriteRecord(*cookieArtifact, cookieRec{Domain: ".example.com", Name: "session"})

	count, err := w.Finalize(c)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}

	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Both files should exist.
	readFromZip(t, path, "records/browser.safari/history.parquet")
	readFromZip(t, path, "records/browser.safari/cookies.parquet")
}

func TestParquetRecordWriter_TypeMismatch(t *testing.T) {
	w := newParquetRecordWriter("test.plugin", "snappy")
	artifact, _ := macfor.NewArtifact("test.plugin", "test", "data")

	type recordA struct {
		Name string `parquet:"name"`
	}
	type recordB struct {
		Value int `parquet:"value"`
	}

	if err := w.WriteRecord(*artifact, recordA{Name: "test"}); err != nil {
		t.Fatalf("first WriteRecord() error = %v", err)
	}

	err := w.WriteRecord(*artifact, recordB{Value: 42})
	if err == nil {
		t.Error("expected error for type mismatch")
	}
}

func TestParquetRecordWriter_NonStructRejection(t *testing.T) {
	w := newParquetRecordWriter("test.plugin", "snappy")
	artifact, _ := macfor.NewArtifact("test.plugin", "test", "data")

	err := w.WriteRecord(*artifact, "not a struct")
	if err == nil {
		t.Error("expected error for non-struct record")
	}

	err = w.WriteRecord(*artifact, map[string]any{"key": "value"})
	if err == nil {
		t.Error("expected error for map record")
	}
}

func TestParquetRecordWriter_EmptyFinalize(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	w := newParquetRecordWriter("test.plugin", "snappy")

	count, err := w.Finalize(c)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestParquetRecordWriter_Metadata(t *testing.T) {
	w := newParquetRecordWriter(testPluginShellHistory, "snappy")
	artifact := makeTestParquetArtifact()

	rec := testParquetRecord{Command: "test", User: "admin", LineNum: 1}
	_ = w.WriteRecord(artifact, rec)

	buf := w.writers[testArtifactTypeHistory]
	data, err := w.encodeParquet(buf)
	if err != nil {
		t.Fatalf("encodeParquet() error = %v", err)
	}

	file, err := goparquet.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	metadata := file.Metadata()
	kvMap := make(map[string]string)
	for _, kv := range metadata.KeyValueMetadata {
		kvMap[kv.Key] = kv.Value
	}

	if v, ok := kvMap["macfor.schema.plugin"]; !ok || v != testPluginShellHistory {
		t.Errorf("macfor.schema.plugin = %q, want %q", v, testPluginShellHistory)
	}
	if v, ok := kvMap["macfor.schema.artifact"]; !ok || v != testArtifactTypeHistory {
		t.Errorf("macfor.schema.artifact = %q, want %q", v, testArtifactTypeHistory)
	}
	if _, ok := kvMap["macfor.created_at"]; !ok {
		t.Error("macfor.created_at not present in metadata")
	}
}

func TestParquetRecordWriter_RowGroupFlushing(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.zip")

	c, err := NewContainer(Config{OutputPath: path})
	if err != nil {
		t.Fatalf("NewContainer() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	w := newParquetRecordWriter("test.plugin", "snappy")
	artifact, _ := macfor.NewArtifact("test.plugin", "test", "data")

	type simpleRec struct {
		Value int64  `parquet:"value"`
		Name  string `parquet:"name"`
	}

	// Write more than one row group's worth of records.
	totalRecords := defaultRowGroupSize + defaultRowGroupSize/2 // 1.5x row group size
	for i := 0; i < totalRecords; i++ {
		rec := simpleRec{Value: int64(i), Name: fmt.Sprintf("record-%d", i)}
		if err := w.WriteRecord(*artifact, rec); err != nil {
			t.Fatalf("WriteRecord(%d) error = %v", i, err)
		}
	}

	count, err := w.Finalize(c)
	if err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if count != totalRecords {
		t.Errorf("Finalize() count = %d, want %d", count, totalRecords)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read back and verify row group count and total rows.
	parquetPath := "records/test.plugin/data.parquet"
	content := readFromZip(t, path, parquetPath)

	file, err := goparquet.OpenFile(bytes.NewReader([]byte(content)), int64(len(content)))
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}

	rowGroups := file.RowGroups()
	if len(rowGroups) != 2 {
		t.Errorf("row groups = %d, want 2", len(rowGroups))
	}

	totalRows := int64(0)
	for _, rg := range rowGroups {
		totalRows += rg.NumRows()
	}
	if totalRows != int64(totalRecords) {
		t.Errorf("total rows = %d, want %d", totalRows, totalRecords)
	}

	// First row group should have exactly defaultRowGroupSize rows.
	if len(rowGroups) >= 1 && rowGroups[0].NumRows() != int64(defaultRowGroupSize) {
		t.Errorf("first row group rows = %d, want %d", rowGroups[0].NumRows(), defaultRowGroupSize)
	}
}

func TestParquetRecordWriter_CompressionCodecs(t *testing.T) {
	codecs := []string{"snappy", "zstd", "gzip", "none"}

	for _, codec := range codecs {
		t.Run(codec, func(t *testing.T) {
			w := newParquetRecordWriter("test.plugin", codec)
			artifact, _ := macfor.NewArtifact("test.plugin", "test", "data")

			type simpleRec struct {
				Value string `parquet:"value"`
			}

			_ = w.WriteRecord(*artifact, simpleRec{Value: "test"})

			buf := w.writers["data"]
			data, err := w.encodeParquet(buf)
			if err != nil {
				t.Fatalf("encodeParquet() error = %v", err)
			}

			// Verify it's valid Parquet.
			_, err = goparquet.OpenFile(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("OpenFile() error = %v", err)
			}
		})
	}
}
