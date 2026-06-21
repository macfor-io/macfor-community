package artifacts

import (
	"strconv"
	"strings"
)

// This file declares the transform vocabulary the declarative extract mappers
// support, as plain data, so `manifest validate` can reject unknown transform
// references at build/validation time. The implementations live in the mapper
// packages (tablemapper, plistmapper); each mapper carries a guard test
// asserting its registry stays in sync with the names declared here.

// TransformTruncatePrefix is the prefix of the parameterized truncate
// transform ("truncate:N", truncating a value to N bytes with a "..." suffix).
// It is valid for every extract-capable format.
const TransformTruncatePrefix = "truncate:"

// Named transform identifiers. The mapper registries key their
// implementations on these same constants, so the validator vocabulary and
// the implementations cannot drift apart on spelling.
const (
	TransformCoreDataTimestamp = "coredata_timestamp"
	TransformCoreDataRFC3339   = "coredata_rfc3339"
	TransformUnixRFC3339       = "unix_rfc3339"
	TransformBoolFromInt       = "bool_from_int"
	TransformByteLength        = "byte_length"
	TransformBase64            = "base64"
	TransformNullifyEmpty      = "nullify_empty"
	TransformBasename          = "basename"
)

// tableTransforms are the named transforms supported by the SQLite/JSONL
// table mapper.
var tableTransforms = []string{
	TransformCoreDataTimestamp,
	TransformCoreDataRFC3339,
	TransformUnixRFC3339,
	TransformBoolFromInt,
	TransformByteLength,
	TransformBase64,
	TransformNullifyEmpty,
}

// plistTransforms are the named transforms supported by the property-list
// mapper.
var plistTransforms = []string{
	TransformBasename,
}

// TransformsForFormat returns the named transforms valid for extract fields of
// artifacts with the given format, excluding the parameterized "truncate:N"
// (valid for all extract-capable formats). Returns nil for formats without a
// declarative extract backend.
func TransformsForFormat(format string) []string {
	switch format {
	case FormatSQLite, FormatJSONL:
		return append([]string(nil), tableTransforms...)
	case FormatBinaryPlist, FormatXMLPlist:
		return append([]string(nil), plistTransforms...)
	default:
		return nil
	}
}

// IsValidTransform reports whether a transform reference is valid for extract
// fields of artifacts with the given format.
func IsValidTransform(format, name string) bool {
	if arg, ok := strings.CutPrefix(name, TransformTruncatePrefix); ok {
		n, err := strconv.Atoi(arg)
		return err == nil && n >= 0
	}
	for _, t := range TransformsForFormat(format) {
		if t == name {
			return true
		}
	}
	return false
}

// extractCapableFormat reports whether the format has a declarative extract
// backend (the table mapper for SQLite/JSONL, the plist mapper for property
// lists).
func extractCapableFormat(format string) bool {
	switch format {
	case FormatSQLite, FormatJSONL, FormatBinaryPlist, FormatXMLPlist:
		return true
	}
	return false
}

// isTableFormat reports whether the format is extracted by the table mapper.
func isTableFormat(format string) bool {
	return format == FormatSQLite || format == FormatJSONL
}
