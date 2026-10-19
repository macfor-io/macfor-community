package macfor

import (
	"context"
	"io"
)

// LogLevel represents the severity level of a log message.
type LogLevel int

const (
	// LogDebug is for detailed debugging information.
	LogDebug LogLevel = iota

	// LogInfo is for general informational messages.
	LogInfo

	// LogWarn is for warning messages that don't prevent operation.
	LogWarn

	// LogError is for error messages indicating failures.
	LogError
)

// String returns the string representation of the log level.
func (l LogLevel) String() string {
	switch l {
	case LogDebug:
		return "DEBUG"
	case LogInfo:
		return "INFO"
	case LogWarn:
		return "WARN"
	case LogError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// Collector receives artifacts and records from plugins during collection.
// It handles writing files to the evidence container, serializing structured
// records, and logging collection progress.
type Collector interface {
	// AddFile adds a file artifact to the evidence container.
	// The artifact metadata describes the file, and src provides the content.
	// The content is copied and hashed during addition. It is stored at
	// artifact.ContainerPath(), or at a counter variant of it (History~2)
	// when another file already occupies that path.
	AddFile(ctx context.Context, artifact Artifact, src io.Reader) error

	// AddRecord adds a structured record to the evidence container.
	// Records are serialized to JSONL format for the specified artifact.
	// The record can be any value that marshals to JSON.
	AddRecord(ctx context.Context, artifact Artifact, record any) error

	// AddError records a non-fatal error that occurred during collection.
	// These errors are logged and included in the manifest but don't stop collection.
	AddError(artifact Artifact, err error)

	// Log writes a message to the collection log.
	Log(level LogLevel, format string, args ...any)
}

// Note: Artifact type is defined in artifact.go
