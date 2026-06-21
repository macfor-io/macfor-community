package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// jsonlRecordWriter implements RecordWriter by buffering JSON Lines records
// in memory, grouped by record path. On Finalize, each buffer is flushed
// to the container as a separate file.
type jsonlRecordWriter struct {
	pluginID string
	writers  map[string]*jsonlBuffer
	mu       sync.Mutex
	count    int
}

// jsonlBuffer holds the in-memory buffer for a single JSONL record file.
type jsonlBuffer struct {
	buf bytes.Buffer
}

// newJSONLRecordWriter creates a new JSONL record writer for the given plugin.
func newJSONLRecordWriter(pluginID string) *jsonlRecordWriter {
	return &jsonlRecordWriter{
		pluginID: pluginID,
		writers:  make(map[string]*jsonlBuffer),
	}
}

// WriteRecord marshals the record to compact JSON and appends it to the
// appropriate buffer, determined by the artifact's record path.
func (w *jsonlRecordWriter) WriteRecord(artifact macfor.Artifact, record any) error {
	recordPath := artifact.RecordPath()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshaling record for %s: %w", recordPath, err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	buf, ok := w.writers[recordPath]
	if !ok {
		buf = &jsonlBuffer{}
		w.writers[recordPath] = buf
	}

	buf.buf.Write(data)
	buf.buf.WriteByte('\n')
	w.count++
	return nil
}

// Finalize writes all buffered JSONL data to the container. Each buffer is
// written as a separate file at the record path determined during WriteRecord.
func (w *jsonlRecordWriter) Finalize(c *Container) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for path, buf := range w.writers {
		if buf.buf.Len() > 0 {
			if err := c.AddFile(path, &buf.buf); err != nil {
				return 0, fmt.Errorf("writing records to %s: %w", path, err)
			}
		}
	}
	return w.count, nil
}

// Compile-time check.
var _ RecordWriter = (*jsonlRecordWriter)(nil)
