// Package shell implements the shell history collector plugin for macfor.
package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// ShRecord represents a parsed sh history command.
type ShRecord struct {
	// Type identifies this as a shell command record.
	Type string `json:"type"`

	// User is the username who executed the command.
	User string `json:"user"`

	// Shell identifies the shell type (always "sh" for sh records).
	Shell string `json:"shell"`

	// Command is the actual command that was executed.
	Command string `json:"command"`

	// Timestamp is always nil for sh since it doesn't support timestamps.
	Timestamp *time.Time `json:"timestamp,omitempty"`

	// LineNumber is the line number in the source history file.
	LineNumber int `json:"line_number"`

	// SourceFile is the path to the history file this command was parsed from.
	SourceFile string `json:"source_file"`
}

// collectShHistory collects and parses sh history for a user.
// It handles both raw file collection and parsing of commands.
//
// The function:
//  1. Collects the raw ~/.sh_history file
//  2. Parses commands (one per line)
//
// Note: sh does not support timestamps in history files.
//
// Error handling:
//   - Missing file: Returns nil (expected, user may not use sh)
//   - Permission denied: Logs warning, continues with other users
//   - Empty file: Collects file but skips record generation
//   - Parse errors: Logs warning, continues with remaining lines
//
//nolint:dupl // Shell collectors follow the same pattern by design
func collectShHistory(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	historyPath, err := locator.Path(src, user, shellArtifactIDs[TypeSh])
	if err != nil {
		return err
	}
	if historyPath == "" {
		out.Log(macfor.LogDebug, "No sh history for user %s: no path for this OS", user.Username)
		return nil
	}

	// Check if the file exists
	fileInfo, err := src.Stat(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing file is expected - user may not use sh
			out.Log(macfor.LogDebug, "No sh history for user %s: file not found", user.Username)
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			// Permission denied - log warning and continue
			out.Log(macfor.LogWarn, "Cannot access sh history for user %s: permission denied", user.Username)
			return nil
		}
		// Other stat errors
		return fmt.Errorf("stat sh history for %s: %w", user.Username, err)
	}

	// Create artifact for the raw file
	artifact, err := macfor.NewArtifact(PluginID, "shell", "sh_history")
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	artifact.WithSourcePath(historyPath).
		WithUsername(user.Username).
		WithDescription(fmt.Sprintf("Sh command history for user %s", user.Username)).
		WithOriginalFile(fileInfo.ModTime(), fileInfo.Size()).
		WithMetadata("shell_type", "sh")

	// Open the file for reading
	reader, err := src.Open(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			out.Log(macfor.LogWarn, "Cannot read sh history for user %s: permission denied", user.Username)
			return nil
		}
		return fmt.Errorf("open sh history for %s: %w", user.Username, err)
	}
	defer func() { _ = reader.Close() }()

	// Read the entire file content into memory so we can:
	// 1. Add the raw file to the collector
	// 2. Parse the commands
	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read sh history for %s: %w", user.Username, err)
	}

	// Add raw file to collector
	if err := out.AddFile(ctx, *artifact, strings.NewReader(string(content))); err != nil {
		out.AddError(*artifact, err)
		out.Log(macfor.LogError, "Failed to add sh history file for user %s: %v", user.Username, err)
		// Continue with parsing even if file addition fails
	}

	// Skip parsing if file is empty
	if len(content) == 0 {
		out.Log(macfor.LogDebug, "Sh history for user %s is empty", user.Username)
		return nil
	}

	// Parse commands and add records
	records, parseWarnings := parseShHistory(string(content), user.Username, historyPath)

	// Log any parse warnings
	for _, warning := range parseWarnings {
		out.Log(macfor.LogWarn, "Sh history parse warning for %s: %s", user.Username, warning)
	}

	// Add each parsed record
	for _, record := range records {
		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.AddError(*artifact, fmt.Errorf("add record at line %d: %w", record.LineNumber, err))
		}
	}

	out.Log(macfor.LogInfo, "Collected %d sh commands for user %s", len(records), user.Username)

	return nil
}

// parseShHistory parses sh history content into structured records.
// Sh uses a simple format with one command per line. Unlike bash,
// sh does not support HISTTIMEFORMAT timestamps.
//
// Returns the parsed records and any warnings encountered during parsing.
func parseShHistory(content, username, sourcePath string) ([]ShRecord, []string) {
	var records []ShRecord
	var warnings []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), maxScanBufferSize)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Every non-empty line is a command
		record := ShRecord{
			Type:       typeShellCommand,
			User:       username,
			Shell:      string(TypeSh),
			Command:    line,
			LineNumber: lineNumber,
			SourceFile: sourcePath,
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("scanner error: %v", err))
	}

	return records, warnings
}
