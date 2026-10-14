package container

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"time"

	goparquet "github.com/parquet-go/parquet-go"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

// defaultRowGroupSize is the number of rows to buffer before flushing
// a row group. This bounds memory usage for large-record-count plugins.
const defaultRowGroupSize = 64 * 1024

// parquetRecordWriter implements RecordWriter by buffering records per artifact
// type and writing Parquet files on Finalize.
type parquetRecordWriter struct {
	pluginID    string
	compression string
	writers     map[string]*artifactParquetBuffer
	mu          sync.Mutex
	count       int
}

// artifactParquetBuffer holds buffered records and an active Parquet writer
// for a single artifact type.
type artifactParquetBuffer struct {
	artifactType string
	goType       reflect.Type
	mergedType   reflect.Type
	records      []parquetBufferedRecord

	// Streaming writer state — created lazily on first record.
	output *bytes.Buffer
	writer *goparquet.Writer //nolint:staticcheck // SA1019: parquet-go Writer is deprecated upstream; migration tracked separately.
}

// parquetBufferedRecord stores a record alongside its artifact metadata
// for common field injection.
type parquetBufferedRecord struct {
	record   any
	username string
}

// newParquetRecordWriter creates a new Parquet record writer.
func newParquetRecordWriter(pluginID string, compression string) *parquetRecordWriter {
	return &parquetRecordWriter{
		pluginID:    pluginID,
		compression: compression,
		writers:     make(map[string]*artifactParquetBuffer),
	}
}

// WriteRecord buffers a record for later Parquet encoding. When the buffer
// for an artifact type reaches defaultRowGroupSize, the current records are
// flushed as a row group to keep memory bounded.
func (w *parquetRecordWriter) WriteRecord(artifact macfor.Artifact, record any) error {
	rt := reflect.TypeOf(record)

	// Dereference pointer.
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}

	if rt.Kind() != reflect.Struct {
		return fmt.Errorf("parquet writer requires struct records, got %s", rt.Kind())
	}

	artifactType := artifact.Type
	if artifactType == "" {
		artifactType = "parsed"
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	buf, ok := w.writers[artifactType]
	if !ok {
		mergedType := buildMergedType(rt)
		buf = &artifactParquetBuffer{
			artifactType: artifactType,
			goType:       rt,
			mergedType:   mergedType,
		}
		w.writers[artifactType] = buf
	} else if buf.goType != rt {
		return fmt.Errorf("type mismatch for artifact %q: expected %s, got %s", artifactType, buf.goType, rt)
	}

	buf.records = append(buf.records, parquetBufferedRecord{
		record:   record,
		username: artifact.Username,
	})
	w.count++

	// Flush row group if buffer is full.
	if len(buf.records) >= defaultRowGroupSize {
		if err := w.flushRowGroup(buf); err != nil {
			return fmt.Errorf("flushing row group: %w", err)
		}
	}

	return nil
}

// flushRowGroup writes the current buffered records as a row group to the
// streaming Parquet writer. The writer is lazily created on first flush.
// Caller must hold w.mu.
func (w *parquetRecordWriter) flushRowGroup(buf *artifactParquetBuffer) error {
	if len(buf.records) == 0 {
		return nil
	}

	// Lazily create the Parquet writer on first flush.
	if buf.writer == nil {
		sampleMerged := w.buildMergedValue(buf.mergedType, buf.records[0], buf)
		schema := goparquet.SchemaOf(sampleMerged.Interface())

		buf.output = &bytes.Buffer{}
		compression := resolveCompression(w.compression)
		metadata := w.buildMetadata(buf)

		writerOpts := []goparquet.WriterOption{
			schema,
			compression,
		}
		for _, kv := range metadata {
			writerOpts = append(writerOpts, goparquet.KeyValueMetadata(kv.Key, kv.Value))
		}

		buf.writer = goparquet.NewWriter(buf.output, writerOpts...)
	}

	// Write all buffered records.
	for _, br := range buf.records {
		merged := w.buildMergedValue(buf.mergedType, br, buf)
		if err := buf.writer.Write(merged.Interface()); err != nil {
			return fmt.Errorf("writing record: %w", err)
		}
	}

	// Flush the writer to start a new row group.
	if err := buf.writer.Flush(); err != nil {
		return fmt.Errorf("flushing row group: %w", err)
	}

	// Release the record references.
	buf.records = buf.records[:0]

	return nil
}

// Finalize encodes all buffered records to Parquet files and writes them
// to the container.
func (w *parquetRecordWriter) Finalize(c *Container) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for artifactType, buf := range w.writers {
		// Flush remaining records.
		if len(buf.records) > 0 {
			if err := w.flushRowGroup(buf); err != nil {
				return 0, fmt.Errorf("final flush for %s: %w", artifactType, err)
			}
		}

		// If no writer was created, no records were ever written.
		if buf.writer == nil {
			continue
		}

		// Close the writer to write the Parquet footer.
		if err := buf.writer.Close(); err != nil {
			return 0, fmt.Errorf("closing parquet writer for %s: %w", artifactType, err)
		}

		path := fmt.Sprintf("records/%s/%s.parquet", w.pluginID, artifactType)
		reader := bytes.NewReader(buf.output.Bytes())
		if err := c.AddFile(path, reader); err != nil {
			return 0, fmt.Errorf("writing parquet to %s: %w", path, err)
		}
	}

	return w.count, nil
}

// encodeParquet encodes all records in a buffer to Parquet bytes.
// This is used by tests that need direct encoding without a container.
func (w *parquetRecordWriter) encodeParquet(buf *artifactParquetBuffer) ([]byte, error) {
	// Create a sample merged value to infer schema.
	sampleMerged := w.buildMergedValue(buf.mergedType, buf.records[0], buf)

	schema := goparquet.SchemaOf(sampleMerged.Interface())

	var output bytes.Buffer
	compression := resolveCompression(w.compression)

	// Build metadata key-value pairs.
	metadata := w.buildMetadata(buf)

	writerOpts := make([]goparquet.WriterOption, 0, 2+len(metadata))
	writerOpts = append(writerOpts, schema, compression)
	for _, kv := range metadata {
		writerOpts = append(writerOpts, goparquet.KeyValueMetadata(kv.Key, kv.Value))
	}

	writer := goparquet.NewWriter(&output, writerOpts...)

	for _, br := range buf.records {
		merged := w.buildMergedValue(buf.mergedType, br, buf)
		if err := writer.Write(merged.Interface()); err != nil {
			return nil, fmt.Errorf("writing record: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("closing parquet writer: %w", err)
	}

	return output.Bytes(), nil
}

// Go field names of the common fields prepended to every plugin record. The
// column names come from the fields' parquet tags; these names only have to be
// unique within the merged struct, so they are prefixed to stay clear of
// plugin record fields such as Username.
const (
	mergedFieldArtifactType = "MacforEnvelopeArtifactType"
	mergedFieldPluginID     = "MacforEnvelopePluginID"
	mergedFieldUsername     = "MacforEnvelopeUsername"
	mergedFieldCollectedAt  = "MacforEnvelopeCollectedAt"
)

// buildMergedType creates a runtime struct type that prepends common fields
// to the plugin record type.
func buildMergedType(pluginType reflect.Type) reflect.Type {
	commonFields := []reflect.StructField{
		{
			Name: mergedFieldArtifactType,
			Type: reflect.TypeOf(""),
			Tag:  `parquet:"_artifact_type"`,
		},
		{
			Name: mergedFieldPluginID,
			Type: reflect.TypeOf(""),
			Tag:  `parquet:"_plugin_id"`,
		},
		{
			Name: mergedFieldUsername,
			Type: reflect.TypeOf(""),
			Tag:  `parquet:"_username"`,
		},
		{
			Name: mergedFieldCollectedAt,
			Type: reflect.TypeOf(time.Time{}),
			Tag:  `parquet:"_collected_at,timestamp(microsecond)"`,
		},
	}

	// Append all exported fields from the plugin type.
	for i := 0; i < pluginType.NumField(); i++ {
		f := pluginType.Field(i)
		if f.IsExported() {
			commonFields = append(commonFields, f)
		}
	}

	return reflect.StructOf(commonFields)
}

// buildMergedValue creates a merged struct value combining common fields
// with the plugin record data.
func (w *parquetRecordWriter) buildMergedValue(mergedType reflect.Type, br parquetBufferedRecord, buf *artifactParquetBuffer) reflect.Value {
	merged := reflect.New(mergedType).Elem()

	// Set common fields.
	merged.FieldByName(mergedFieldArtifactType).SetString(buf.artifactType)
	merged.FieldByName(mergedFieldPluginID).SetString(w.pluginID)
	merged.FieldByName(mergedFieldUsername).SetString(br.username)
	merged.FieldByName(mergedFieldCollectedAt).Set(reflect.ValueOf(time.Now().UTC()))

	// Copy plugin record fields.
	rv := reflect.ValueOf(br.record)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.IsExported() {
			merged.FieldByName(f.Name).Set(rv.Field(i))
		}
	}

	return merged
}

// buildMetadata creates Parquet file metadata key-value pairs.
func (w *parquetRecordWriter) buildMetadata(buf *artifactParquetBuffer) []keyValue {
	md := []keyValue{
		{"macfor.schema.plugin", w.pluginID},
		{"macfor.schema.artifact", buf.artifactType},
		{"macfor.created_at", time.Now().UTC().Format(time.RFC3339)},
	}

	// Look up registry entry for version and checksum.
	entry := parquet.GlobalRegistry().Lookup(w.pluginID, buf.artifactType)
	if entry != nil {
		md = append(md, keyValue{"macfor.schema.version", entry.Version.String()})
		md = append(md, keyValue{"macfor.schema.checksum", entry.Checksum})
	}

	return md
}

// keyValue is a simple string key-value pair for Parquet metadata.
type keyValue struct {
	Key   string
	Value string
}

// compressionSnappy is the Snappy compression codec name.
const compressionSnappy = "snappy"

// resolveCompression maps a compression name to a parquet-go compression option.
func resolveCompression(name string) goparquet.WriterOption {
	switch name {
	case compressionSnappy:
		return goparquet.Compression(&goparquet.Snappy)
	case "zstd":
		return goparquet.Compression(&goparquet.Zstd)
	case "gzip":
		return goparquet.Compression(&goparquet.Gzip)
	case "none", "":
		return goparquet.Compression(&goparquet.Uncompressed)
	default:
		return goparquet.Compression(&goparquet.Snappy)
	}
}

// Compile-time check.
var _ RecordWriter = (*parquetRecordWriter)(nil)
