package macfor

import (
	"errors"
	"fmt"
)

// Sentinel errors for common failure modes in forensic collection.
// Use errors.Is() to check for these error types.
var (
	// ErrPermissionDenied indicates the operation failed due to insufficient permissions.
	ErrPermissionDenied = errors.New("permission denied")

	// ErrNotFound indicates the requested file, artifact, or resource was not found.
	ErrNotFound = errors.New("not found")

	// ErrParseError indicates a failure to parse file contents.
	ErrParseError = errors.New("parse error")

	// ErrIOError indicates a general I/O failure during read or write operations.
	ErrIOError = errors.New("I/O error")

	// ErrUnsupportedVersion indicates the artifact version is not supported by this plugin.
	ErrUnsupportedVersion = errors.New("unsupported version")

	// ErrCorrupted indicates the artifact data appears to be corrupted or invalid.
	ErrCorrupted = errors.New("corrupted data")
)

// CollectionError wraps an error with additional context about the artifact
// being collected when the error occurred.
type CollectionError struct {
	// Artifact is the artifact that was being collected when the error occurred.
	Artifact Artifact

	// Op is the operation that was being performed (e.g., "read", "parse", "hash").
	Op string

	// Err is the underlying error.
	Err error
}

// Error returns the error message with context.
func (e *CollectionError) Error() string {
	if e.Artifact.SourcePath != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Artifact.SourcePath, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

// Unwrap returns the underlying error for errors.Is and errors.As.
func (e *CollectionError) Unwrap() error {
	return e.Err
}

// NewCollectionError creates a new CollectionError with the given context.
func NewCollectionError(artifact Artifact, op string, err error) *CollectionError {
	return &CollectionError{
		Artifact: artifact,
		Op:       op,
		Err:      err,
	}
}

// ParseError provides detailed context for parsing failures.
type ParseError struct {
	// File is the path to the file being parsed.
	File string

	// Line is the line number where the error occurred (0 if not applicable).
	Line int

	// Offset is the byte offset where the error occurred (0 if not applicable).
	Offset int64

	// Message describes what went wrong.
	Message string

	// Err is the underlying error, if any.
	Err error
}

// Error returns the error message with context.
func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("parse %s:%d: %s", e.File, e.Line, e.Message)
	}
	if e.Offset > 0 {
		return fmt.Sprintf("parse %s (offset %d): %s", e.File, e.Offset, e.Message)
	}
	return fmt.Sprintf("parse %s: %s", e.File, e.Message)
}

// Unwrap returns the underlying errors for errors.Is and errors.As.
// Returns both ErrParseError and the underlying error if present.
func (e *ParseError) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrParseError, e.Err}
	}
	return []error{ErrParseError}
}

// NewParseError creates a new ParseError with the given context.
func NewParseError(file string, line int, message string) *ParseError {
	return &ParseError{
		File:    file,
		Line:    line,
		Message: message,
	}
}

// WrapPermissionDenied wraps an error as a permission denied error.
// Both ErrPermissionDenied and the underlying error are available via errors.Is/As.
func WrapPermissionDenied(path string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrPermissionDenied, path, err)
}

// WrapNotFound wraps an error as a not found error.
// Both ErrNotFound and the underlying error are available via errors.Is/As.
func WrapNotFound(path string, err error) error {
	return fmt.Errorf("%w: %s: %w", ErrNotFound, path, err)
}

// WrapIOError wraps an error as an I/O error.
// Both ErrIOError and the underlying error are available via errors.Is/As.
func WrapIOError(op, path string, err error) error {
	return fmt.Errorf("%w: %s %s: %w", ErrIOError, op, path, err)
}

// WrapParseError wraps an error as a parse error with file context.
// Returns a ParseError that unwraps to both ErrParseError and the underlying error.
func WrapParseError(file string, line int, offset int64, message string, err error) *ParseError {
	return &ParseError{
		File:    file,
		Line:    line,
		Offset:  offset,
		Message: message,
		Err:     err,
	}
}
