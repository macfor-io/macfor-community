// Package container implements the evidence container writer that creates
// ZIP-based forensic containers with integrity verification.
package container

import (
	"bytes"
	"context"
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
type Collector struct {
	container *Container
	pluginID  string
	pluginMu  sync.Mutex

	// filesCollected tracks the number of files added by this collector.
	filesCollected int

	// recordsCreated tracks the number of records added by this collector.
	recordsCreated int

	// errors tracks non-fatal errors that occurred during collection.
	errors []PluginError

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

	// Add the file to the container
	if err := c.container.AddFile(containerPath, src); err != nil {
		c.AddError(artifact, fmt.Errorf("adding file to container: %w", err))
		return err
	}

	// Get the hash for the custody entry
	hashes := c.container.Hashes()
	hash := hashes[containerPath]

	// Log chain of custody entry
	c.container.AddCustodyEntry(ChainOfCustodyEntry{
		Timestamp: time.Now(),
		Action:    CustodyActionFileAdded,
		Actor:     actorMacfor,
		Target:    containerPath,
		Hash:      hash,
		Details: map[string]any{
			"plugin":      c.pluginID,
			"source_path": artifact.SourcePath,
			"username":    artifact.Username,
		},
	})

	c.pluginMu.Lock()
	c.filesCollected++
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
			"plugin":    c.pluginID,
			"operation": artifact.Type,
			"error":     err.Error(),
		},
	})
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
var _ macfor.Collector = (*Collector)(nil)
