// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	macforparquet "github.com/macfor-io/macfor-community/pkg/macfor/parquet"
)

// Collector adapts the evidence container to the macfor.Collector interface.
// It handles writing files and records to the container, tracks errors,
// and logs collection activity.
//
// Collector also implements macfor.OptionProvider, exposing the options
// configured for its plugin with Container.SetPluginOptions.
type Collector struct {
	container *Container
	pluginID  string
	pluginMu  sync.Mutex

	// filesCollected tracks the number of files added by this collector.
	filesCollected int

	// bytesCollected tracks the total size of files added by this collector.
	bytesCollected int64

	// recordsCreated tracks the number of records added by this collector.
	recordsCreated int

	// errors tracks non-fatal errors that occurred during collection.
	errors []PluginError

	// added holds the errors passed to AddError, so errors a plugin also
	// returns from Collect are not counted twice.
	added []error

	// recordWriter handles serialisation of structured records.
	recordWriter RecordWriter

	// logBuffer accumulates log entries to be written to collection.log.
	logBuffer bytes.Buffer
	logMu     sync.Mutex

	// startedAt is when this collector started.
	startedAt time.Time
}

// NewCollector creates a new Collector that writes to the given container.
// The pluginID is used for logging and manifest tracking. The record writer
// is selected based on the container's RecordFormat configuration.
func NewCollector(container *Container, pluginID string) *Collector {
	var rw RecordWriter
	switch container.RecordFormat() {
	case macforparquet.FormatParquet:
		rw = newParquetRecordWriter(pluginID, container.ParquetCompression())
	case macforparquet.FormatBoth:
		rw = newBothRecordWriter(pluginID, container.ParquetCompression())
	case macforparquet.FormatJSONL:
		rw = newJSONLRecordWriter(pluginID)
	}

	return &Collector{
		container:    container,
		pluginID:     pluginID,
		recordWriter: rw,
		startedAt:    time.Now(),
	}
}

// Option returns the value of the named option configured for this
// collector's plugin, or "" when it is not set. Options configured for other
// plugins are never visible. It implements macfor.OptionProvider.
func (c *Collector) Option(name string) string {
	return c.container.pluginOption(c.pluginID, name)
}

// AddFile adds a file artifact to the evidence container.
// The artifact metadata describes the file, and src provides the content.
// The content is copied and hashed during addition.
//
// The file is stored at the path computed by artifact.ContainerPath().
// A chain of custody entry is logged for each file added.
func (c *Collector) AddFile(ctx context.Context, artifact macfor.Artifact, src io.Reader) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Compute the container path
	containerPath := artifact.ContainerPath()

	// Add the file to the container, preserving the source modified time.
	counter := &countingReader{r: src}
	if err := c.container.AddFileModified(containerPath, artifact.OriginalModTime, counter); err != nil {
		c.AddError(artifact, fmt.Errorf("adding file to container: %w", err))
		return err
	}

	// Get the hash for the custody entry
	hashes := c.container.Hashes()
	hash := hashes[containerPath]

	// Log chain of custody entry. The ZIP header only holds whole seconds,
	// so the exact source modified time is recorded here.
	details := map[string]any{
		custodyKeyPlugin: c.pluginID,
		"source_path":    artifact.SourcePath,
		"username":       artifact.Username,
	}
	if !artifact.OriginalModTime.IsZero() {
		details["source_modified"] = artifact.OriginalModTime.Format(time.RFC3339Nano)
	}
	c.container.AddCustodyEntry(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionFileAdded,
		Actor:     actorMacfor,
		Target:    containerPath,
		Hash:      hash,
		Details:   details,
	})

	c.pluginMu.Lock()
	c.filesCollected++
	c.bytesCollected += counter.n
	c.pluginMu.Unlock()

	c.Log(macfor.LogDebug, "Added file: %s -> %s", artifact.SourcePath, containerPath)
	return nil
}

// AddRecord adds a structured record to the evidence container.
// Records are serialized to JSONL format (one JSON object per line) for
// the specified artifact. Multiple records for the same artifact type
// are appended to the same JSONL file.
//
// The record can be any value that marshals to JSON.
func (c *Collector) AddRecord(ctx context.Context, artifact macfor.Artifact, record any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Delegate to the record writer for serialisation.
	if err := c.recordWriter.WriteRecord(artifact, record); err != nil {
		c.AddError(artifact, fmt.Errorf("marshaling record: %w", err))
		return err
	}

	c.pluginMu.Lock()
	c.recordsCreated++
	c.pluginMu.Unlock()

	return nil
}

// AddError records a non-fatal error that occurred during collection.
// These errors are logged and included in the manifest but don't stop collection.
func (c *Collector) AddError(artifact macfor.Artifact, err error) {
	pluginErr := PluginError{
		Artifact:  artifact.SourcePath,
		Operation: artifact.Type,
		Message:   err.Error(),
		Timestamp: time.Now(),
	}

	c.pluginMu.Lock()
	c.errors = append(c.errors, pluginErr)
	c.added = append(c.added, err)
	c.pluginMu.Unlock()

	// Log to collection log
	c.Log(macfor.LogError, "Error collecting %s: %v", artifact.SourcePath, err)

	// Also add custody entry for the error
	c.container.AddCustodyEntry(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionError,
		Actor:     actorMacfor,
		Target:    artifact.SourcePath,
		Details: map[string]any{
			custodyKeyPlugin: c.pluginID,
			"operation":      artifact.Type,
			"error":          err.Error(),
		},
	})
}

// RecordCollectError records the error a plugin returned from Collect.
// Plugins commonly report per-artifact failures by joining them into the
// returned error instead of calling AddError, so each joined error is
// recorded separately. Errors already recorded via AddError are skipped.
// The plugin is expected to have logged them, so they are not logged again.
func (c *Collector) RecordCollectError(err error) {
	for _, e := range splitJoined(err) {
		c.pluginMu.Lock()
		dup := false
		for _, a := range c.added {
			if errors.Is(e, a) {
				dup = true
				break
			}
		}
		if !dup {
			c.errors = append(c.errors, PluginError{
				Operation: "collect",
				Message:   e.Error(),
				Timestamp: time.Now(),
			})
		}
		c.pluginMu.Unlock()

		if dup {
			continue
		}
		c.container.AddCustodyEntry(ChainOfCustodyEntry{
			Timestamp: time.Now(),
			Action:    CustodyActionError,
			Actor:     actorMacfor,
			Target:    c.pluginID,
			Details: map[string]any{
				custodyKeyPlugin: c.pluginID,
				"operation":      "collect",
				"error":          e.Error(),
			},
		})
	}
}

// splitJoined flattens errors created by errors.Join into their components.
func splitJoined(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range j.Unwrap() {
			out = append(out, splitJoined(e)...)
		}
		return out
	}
	return []error{err}
}

// Log writes a message to the collection log.
// The message is buffered and written to collection.log when Finalize is called.
//
//nolint:goprintffuncname // interface method name is Log, not Logf
func (c *Collector) Log(level macfor.LogLevel, format string, args ...any) {
	timestamp := time.Now().Format(time.RFC3339)
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] [%s] [%s] %s\n", timestamp, level.String(), c.pluginID, message)

	c.logMu.Lock()
	c.logBuffer.WriteString(logLine)
	c.logMu.Unlock()
}

// Finalize writes all buffered records and logs to the container and returns
// a PluginSummary with collection statistics. This should be called after
// the plugin has finished collecting artifacts.
func (c *Collector) Finalize() (PluginSummary, error) {
	completedAt := time.Now()

	// Flush all buffered records to the container.
	if _, err := c.recordWriter.Finalize(c.container); err != nil {
		return PluginSummary{}, fmt.Errorf("flushing records: %w", err)
	}

	// Write collection log
	c.logMu.Lock()
	if c.logBuffer.Len() > 0 {
		logPath := fmt.Sprintf("logs/%s/collection.log", c.pluginID)
		if err := c.container.AddFile(logPath, &c.logBuffer); err != nil {
			c.logMu.Unlock()
			return PluginSummary{}, fmt.Errorf("writing collection log: %w", err)
		}
	}
	c.logMu.Unlock()

	// Build plugin summary
	c.pluginMu.Lock()
	summary := PluginSummary{
		ID:                c.pluginID,
		StartedAt:         c.startedAt,
		CompletedAt:       completedAt,
		FilesCollected:    c.filesCollected,
		BytesCollected:    c.bytesCollected,
		RecordsCreated:    c.recordsCreated,
		ErrorsEncountered: len(c.errors),
		Errors:            c.errors,
	}
	c.pluginMu.Unlock()

	return summary, nil
}

// FilesCollected returns the number of files added by this collector.
func (c *Collector) FilesCollected() int {
	c.pluginMu.Lock()
	defer c.pluginMu.Unlock()
	return c.filesCollected
}

// RecordsCreated returns the number of records added by this collector.
func (c *Collector) RecordsCreated() int {
	c.pluginMu.Lock()
	defer c.pluginMu.Unlock()
	return c.recordsCreated
}

// ErrorCount returns the number of errors recorded by this collector.
func (c *Collector) ErrorCount() int {
	c.pluginMu.Lock()
	defer c.pluginMu.Unlock()
	return len(c.errors)
}

// Ensure Collector implements macfor.Collector.
var (
	_ macfor.Collector      = (*Collector)(nil)
	_ macfor.OptionProvider = (*Collector)(nil)
)

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}
