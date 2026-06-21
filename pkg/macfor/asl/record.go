// Package asl provides a parser for Apple System Log (ASL) binary files.
//
// ASL files are used by macOS for DiagnosticMessages and other system logging.
// The binary format is documented by libyal/dtformats. This package provides
// a streaming reader that parses records from ASL DB files.
package asl

import "time"

// Log level constants matching Apple System Log levels.
const (
	LevelEmergency = 0
	LevelAlert     = 1
	LevelCritical  = 2
	LevelError     = 3
	LevelWarning   = 4
	LevelNotice    = 5
	LevelInfo      = 6
	LevelDebug     = 7
)

// levelNames maps log levels to human-readable strings.
var levelNames = map[int]string{
	LevelEmergency: "Emergency",
	LevelAlert:     "Alert",
	LevelCritical:  "Critical",
	LevelError:     "Error",
	LevelWarning:   "Warning",
	LevelNotice:    "Notice",
	LevelInfo:      "Info",
	LevelDebug:     "Debug",
}

// LevelString returns the human-readable name for an ASL log level.
func LevelString(level int) string {
	if name, ok := levelNames[level]; ok {
		return name
	}
	return "Unknown"
}

// Record represents a single ASL log record.
type Record struct {
	// MessageID is the unique identifier for this log message.
	MessageID uint64 `json:"message_id"`

	// Timestamp is the time the message was logged, with nanosecond precision.
	Timestamp time.Time `json:"timestamp"`

	// Level is the ASL log level (0=Emergency through 7=Debug).
	Level int `json:"level"`

	// Flags contains record flags.
	Flags uint16 `json:"flags"`

	// PID is the process ID that generated the message.
	PID int `json:"pid"`

	// UID is the user ID of the process.
	UID int `json:"uid"`

	// GID is the group ID of the process.
	GID int `json:"gid"`

	// ReadUID is the UID required to read this record.
	ReadUID int `json:"read_uid"`

	// ReadGID is the GID required to read this record.
	ReadGID int `json:"read_gid"`

	// RefPID is a reference process ID.
	RefPID int `json:"ref_pid"`

	// Host is the hostname that generated the message.
	Host string `json:"host"`

	// Sender is the process or binary that sent the message.
	Sender string `json:"sender"`

	// Facility is the logging facility (e.g. "com.apple.coretls.connection").
	Facility string `json:"facility"`

	// Message is the log message text.
	Message string `json:"message"`

	// RefProcess is the reference process name.
	RefProcess string `json:"ref_process"`

	// Session is the session identifier.
	Session string `json:"session"`

	// KeyValues contains additional key-value pairs attached to the record.
	KeyValues map[string]string `json:"key_values,omitempty"`
}
