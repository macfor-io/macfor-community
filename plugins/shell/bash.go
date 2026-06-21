// Package shell implements the shell history collector plugin for macfor.
package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// BashRecord represents a parsed bash history command.
type BashRecord struct {
	// Type identifies this as a shell command record.
	Type string `json:"type"`

	// User is the username who executed the command.
	User string `json:"user"`

	// Shell identifies the shell type (always "bash" for bash records).
	Shell string `json:"shell"`

	// Command is the actual command that was executed.
	Command string `json:"command"`

	// Timestamp is when the command was executed (nil if not available).
	Timestamp *time.Time `json:"timestamp,omitempty"`

	// LineNumber is the line number in the source history file.
	LineNumber int `json:"line_number"`

	// SourceFile is the path to the history file this command was parsed from.
	SourceFile string `json:"source_file"`
}

// collectBashHistory collects and parses bash history for a user.
// It handles both raw file collection and parsing of commands.
//
// The function:
//  1. Collects the raw ~/.bash_history file
//  2. Parses commands (one per line)
//  3. Handles HISTTIMEFORMAT timestamps if present (lines starting with #timestamp)
//
// Error handling:
//   - Missing file: Returns nil (expected, user may not use bash)
//   - Permission denied: Logs warning, continues with other users
//   - Empty file: Collects file but skips record generation
//   - Parse errors: Logs warning, continues with remaining lines
//
//nolint:dupl // Shell collectors follow the same pattern by design
func collectBashHistory(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	historyPath, err := locator.Path(src, user, shellArtifactIDs[TypeBash])
	if err != nil {
		return err
	}
	if historyPath == "" {
		out.Log(macfor.LogDebug, "No bash history for user %s: no path for this OS", user.Username)
		return nil
	}

	// Check if the file exists
	fileInfo, err := src.Stat(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing file is expected - user may not use bash
			out.Log(macfor.LogDebug, "No bash history for user %s: file not found", user.Username)
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			// Permission denied - log warning and continue
			out.Log(macfor.LogWarn, "Cannot access bash history for user %s: permission denied", user.Username)
			return nil
		}
		// Other stat errors
		return fmt.Errorf("stat bash history for %s: %w", user.Username, err)
	}

	// Create artifact for the raw file
	artifact, err := macfor.NewArtifact(PluginID, "shell", "bash_history")
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	artifact.WithSourcePath(historyPath).
		WithUsername(user.Username).
		WithDescription(fmt.Sprintf("Bash command history for user %s", user.Username)).
		WithOriginalFile(fileInfo.ModTime(), fileInfo.Size()).
		WithMetadata("shell_type", "bash")

	// Open the file for reading
	reader, err := src.Open(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			out.Log(macfor.LogWarn, "Cannot read bash history for user %s: permission denied", user.Username)
			return nil
		}
		return fmt.Errorf("open bash history for %s: %w", user.Username, err)
	}
	defer func() { _ = reader.Close() }()

	// Read the entire file content into memory so we can:
	// 1. Add the raw file to the collector
	// 2. Parse the commands
	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read bash history for %s: %w", user.Username, err)
	}

	// Add raw file to collector
	if err := out.AddFile(ctx, *artifact, strings.NewReader(string(content))); err != nil {
		out.AddError(*artifact, err)
		out.Log(macfor.LogError, "Failed to add bash history file for user %s: %v", user.Username, err)
		// Continue with parsing even if file addition fails
	}

	// Skip parsing if file is empty
	if len(content) == 0 {
		out.Log(macfor.LogDebug, "Bash history for user %s is empty", user.Username)
		return nil
	}

	// Parse commands and add records
	records, parseWarnings := parseBashHistory(string(content), user.Username, historyPath)

	// Log any parse warnings
	for _, warning := range parseWarnings {
		out.Log(macfor.LogWarn, "Bash history parse warning for %s: %s", user.Username, warning)
	}

	// Add each parsed record
	for _, record := range records {
		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.AddError(*artifact, fmt.Errorf("add record at line %d: %w", record.LineNumber, err))
		}
	}

	out.Log(macfor.LogInfo, "Collected %d bash commands for user %s", len(records), user.Username)

	return nil
}

// parseBashHistory parses bash history content into structured records.
// It handles both plain format (one command per line) and timestamped format
// (lines starting with #timestamp from HISTTIMEFORMAT).
//
// Returns the parsed records and any warnings encountered during parsing.
func parseBashHistory(content, username, sourcePath string) ([]BashRecord, []string) {
	var records []BashRecord
	var warnings []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), maxScanBufferSize)
	lineNumber := 0
	var pendingTimestamp *time.Time

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check for HISTTIMEFORMAT timestamp line
		// Format: #timestamp (e.g., #1706012400)
		if ts, ok := parseBashTimestamp(line); ok {
			pendingTimestamp = ts
			continue
		}

		// This is a command line
		record := BashRecord{
			Type:       typeShellCommand,
			User:       username,
			Shell:      string(TypeBash),
			Command:    line,
			LineNumber: lineNumber,
			SourceFile: sourcePath,
		}

		// Apply pending timestamp if available
		if pendingTimestamp != nil {
			record.Timestamp = pendingTimestamp
			pendingTimestamp = nil
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("scanner error: %v", err))
	}

	return records, warnings
}

// parseBashTimestamp attempts to parse a HISTTIMEFORMAT timestamp line.
// The format is #timestamp where timestamp is a Unix epoch.
// Returns the parsed time and true if successful, nil and false otherwise.
func parseBashTimestamp(line string) (*time.Time, bool) {
	if !strings.HasPrefix(line, "#") {
		return nil, false
	}

	// Extract the timestamp part (everything after #)
	tsStr := strings.TrimPrefix(line, "#")
	tsStr = strings.TrimSpace(tsStr)

	// Must be a valid integer
	unix, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return nil, false
	}

	// Sanity check: timestamp should be reasonable
	// Unix timestamps before 1990 or after 2100 are likely not timestamps
	const minTimestamp = 631152000  // 1990-01-01
	const maxTimestamp = 4102444800 // 2100-01-01
	if unix < minTimestamp || unix > maxTimestamp {
		return nil, false
	}

	t := time.Unix(unix, 0).UTC()
	return &t, true
}
