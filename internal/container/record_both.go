// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import "github.com/macfor-io/macfor-community/pkg/macfor"

// bothRecordWriter implements RecordWriter by delegating to both a JSONL
// and a Parquet writer, producing output in both formats.
type bothRecordWriter struct {
	jsonl   RecordWriter
	parquet RecordWriter
}

// newBothRecordWriter creates a record writer that writes in both JSONL and Parquet formats.
func newBothRecordWriter(pluginID, compression string) *bothRecordWriter {
	return &bothRecordWriter{
		jsonl:   newJSONLRecordWriter(pluginID),
		parquet: newParquetRecordWriter(pluginID, compression),
	}
}

// WriteRecord delegates the record to both the JSONL and Parquet writers.
func (w *bothRecordWriter) WriteRecord(artifact macfor.Artifact, record any) error {
	if err := w.jsonl.WriteRecord(artifact, record); err != nil {
		return err
	}
	return w.parquet.WriteRecord(artifact, record)
}

// Finalize flushes both writers to the container.
func (w *bothRecordWriter) Finalize(c *Container) (int, error) {
	_, err := w.jsonl.Finalize(c)
	if err != nil {
		return 0, err
	}
	return w.parquet.Finalize(c)
}

// Compile-time check.
var _ RecordWriter = (*bothRecordWriter)(nil)
