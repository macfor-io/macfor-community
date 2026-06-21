// Package parquet provides types and utilities for Parquet record output
// in macfor evidence containers.
package parquet

import (
	"fmt"
	"strings"
)

// RecordFormat specifies the serialisation format for parsed artifact records.
type RecordFormat string

const (
	// FormatJSONL is the default JSON Lines format (one JSON object per line).
	FormatJSONL RecordFormat = "jsonl"

	// FormatParquet uses Apache Parquet columnar format.
	FormatParquet RecordFormat = "parquet"

	// FormatBoth writes records in both JSONL and Parquet formats.
	FormatBoth RecordFormat = "both"
)

// ParseRecordFormat parses a string into a RecordFormat.
// Accepts "jsonl", "parquet", or "both" (case-insensitive).
func ParseRecordFormat(s string) (RecordFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "jsonl":
		return FormatJSONL, nil
	case "parquet":
		return FormatParquet, nil
	case "both":
		return FormatBoth, nil
	default:
		return "", fmt.Errorf("invalid record format %q: must be one of jsonl, parquet, both", s)
	}
}

// String returns the string representation of the RecordFormat.
func (f RecordFormat) String() string {
	return string(f)
}

// Compression specifies the compression codec for Parquet files.
type Compression string

const (
	// CompressionSnappy uses Snappy compression (default, fast).
	CompressionSnappy Compression = "snappy"

	// CompressionZstd uses Zstandard compression (better ratio).
	CompressionZstd Compression = "zstd"

	// CompressionGzip uses gzip compression (widely compatible).
	CompressionGzip Compression = "gzip"

	// CompressionNone disables compression.
	CompressionNone Compression = "none"
)

// ParseCompression parses a string into a Compression codec.
// Accepts "snappy", "zstd", "gzip", "none", or "uncompressed" (case-insensitive).
func ParseCompression(s string) (Compression, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "snappy":
		return CompressionSnappy, nil
	case "zstd":
		return CompressionZstd, nil
	case "gzip":
		return CompressionGzip, nil
	case "none", "uncompressed":
		return CompressionNone, nil
	default:
		return "", fmt.Errorf("invalid compression codec %q: must be one of snappy, zstd, gzip, none", s)
	}
}

// String returns the string representation of the Compression codec.
func (c Compression) String() string {
	return string(c)
}
