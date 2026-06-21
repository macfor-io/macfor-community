// Package shell implements the shell history collector plugin for macfor.
package shell

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// ZshRecord represents a parsed zsh history command.
type ZshRecord struct {
	// Type identifies this as a shell command record.
	Type string `json:"type"`

	// User is the username who executed the command.
	User string `json:"user"`

	// Shell identifies the shell type (always "zsh" for zsh records).
	Shell string `json:"shell"`

	// Command is the actual command that was executed.
	Command string `json:"command"`

	// Timestamp is when the command was executed (nil if not available).
	Timestamp *time.Time `json:"timestamp,omitempty"`

	// Duration is the execution duration in seconds (0 if not available).
	// Zsh extended history format includes duration as the second number.
	Duration int `json:"duration,omitempty"`

	// LineNumber is the line number in the source history file.
	LineNumber int `json:"line_number"`

	// SourceFile is the path to the history file this command was parsed from.
	SourceFile string `json:"source_file"`
}

// zshExtendedRe matches the zsh extended history format: `: timestamp:duration;command`
// Example: `: 1706012400:0;ls -la`.
var zshExtendedRe = regexp.MustCompile(`^: (\d+):(\d+);(.*)$`)

// collectZshHistory collects and parses zsh history for a user.
// It handles both raw file collection and parsing of commands.
//
// The function:
//  1. Collects the raw ~/.zsh_history file
//  2. Parses extended history format `: timestamp:duration;command`
//  3. Handles multi-line commands (lines ending with `\`)
//
// Error handling:
//   - Missing file: Returns nil (expected, user may not use zsh)
//   - Permission denied: Logs warning, continues with other users
//   - Empty file: Collects file but skips record generation
//   - Parse errors: Logs warning, continues with remaining lines
//
//nolint:dupl // Shell collectors follow the same pattern by design
func collectZshHistory(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	historyPath, err := locator.Path(src, user, shellArtifactIDs[TypeZsh])
	if err != nil {
		return err
	}
	if historyPath == "" {
		out.Log(macfor.LogDebug, "No zsh history for user %s: no path for this OS", user.Username)
		return nil
	}

	// Check if the file exists
	fileInfo, err := src.Stat(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing file is expected - user may not use zsh
			out.Log(macfor.LogDebug, "No zsh history for user %s: file not found", user.Username)
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			// Permission denied - log warning and continue
			out.Log(macfor.LogWarn, "Cannot access zsh history for user %s: permission denied", user.Username)
			return nil
		}
		// Other stat errors
		return fmt.Errorf("stat zsh history for %s: %w", user.Username, err)
	}

	// Create artifact for the raw file
	artifact, err := macfor.NewArtifact(PluginID, "shell", "zsh_history")
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	artifact.WithSourcePath(historyPath).
		WithUsername(user.Username).
		WithDescription(fmt.Sprintf("Zsh command history for user %s", user.Username)).
		WithOriginalFile(fileInfo.ModTime(), fileInfo.Size()).
		WithMetadata("shell_type", "zsh")

	// Open the file for reading
	reader, err := src.Open(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			out.Log(macfor.LogWarn, "Cannot read zsh history for user %s: permission denied", user.Username)
			return nil
		}
		return fmt.Errorf("open zsh history for %s: %w", user.Username, err)
	}
	defer func() { _ = reader.Close() }()

	// Read the entire file content into memory so we can:
	// 1. Add the raw file to the collector
	// 2. Parse the commands
	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read zsh history for %s: %w", user.Username, err)
	}

	// Add raw file to collector
	if err := out.AddFile(ctx, *artifact, strings.NewReader(string(content))); err != nil {
		out.AddError(*artifact, err)
		out.Log(macfor.LogError, "Failed to add zsh history file for user %s: %v", user.Username, err)
		// Continue with parsing even if file addition fails
	}

	// Skip parsing if file is empty
	if len(content) == 0 {
		out.Log(macfor.LogDebug, "Zsh history for user %s is empty", user.Username)
		return nil
	}

	// Parse commands and add records
	records, parseWarnings := parseZshHistory(string(content), user.Username, historyPath)

	// Log any parse warnings
	for _, warning := range parseWarnings {
		out.Log(macfor.LogWarn, "Zsh history parse warning for %s: %s", user.Username, warning)
	}

	// Add each parsed record
	for _, record := range records {
		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.AddError(*artifact, fmt.Errorf("add record at line %d: %w", record.LineNumber, err))
		}
	}

	out.Log(macfor.LogInfo, "Collected %d zsh commands for user %s", len(records), user.Username)

	return nil
}

// parseZshHistory parses zsh history content into structured records.
// It handles both extended format (`: timestamp:duration;command`) and basic format
// (one command per line). Multi-line commands (lines ending with `\`) are combined.
//
// Returns the parsed records and any warnings encountered during parsing.
func parseZshHistory(content, username, sourcePath string) ([]ZshRecord, []string) {
	var records []ZshRecord
	var warnings []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), maxScanBufferSize)
	lineNumber := 0

	// Track multi-line command state
	var pendingCommand strings.Builder
	var pendingTimestamp *time.Time
	var pendingDuration int
	var pendingLineNumber int

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Check if we're continuing a multi-line command
		if pendingCommand.Len() > 0 {
			// Remove the trailing backslash from the previous line
			// and append this line
			pendingCommand.WriteString("\n")
			pendingCommand.WriteString(line)

			// Check if this line also continues (ends with unescaped backslash)
			if isLineContinuation(line) {
				continue
			}

			// Multi-line command is complete
			cmd := pendingCommand.String()
			// Trim the trailing backslash from the last continued line if present
			cmd = trimContinuationBackslash(cmd)

			record := ZshRecord{
				Type:       typeShellCommand,
				User:       username,
				Shell:      string(TypeZsh),
				Command:    cmd,
				Timestamp:  pendingTimestamp,
				Duration:   pendingDuration,
				LineNumber: pendingLineNumber,
				SourceFile: sourcePath,
			}
			records = append(records, record)

			// Reset pending state
			pendingCommand.Reset()
			pendingTimestamp = nil
			pendingDuration = 0
			pendingLineNumber = 0
			continue
		}

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Try to parse as extended format
		timestamp, duration, cmd, isExtended := parseZshExtendedLine(line)

		if !isExtended {
			// Basic format - the entire line is the command
			cmd = line
		}

		// Check if this line continues (ends with unescaped backslash)
		if isLineContinuation(cmd) {
			pendingCommand.WriteString(cmd)
			pendingTimestamp = timestamp
			pendingDuration = duration
			pendingLineNumber = lineNumber
			continue
		}

		// Single-line command
		record := ZshRecord{
			Type:       typeShellCommand,
			User:       username,
			Shell:      string(TypeZsh),
			Command:    cmd,
			Timestamp:  timestamp,
			Duration:   duration,
			LineNumber: lineNumber,
			SourceFile: sourcePath,
		}
		records = append(records, record)
	}

	// Handle any pending multi-line command at end of file
	if pendingCommand.Len() > 0 {
		cmd := pendingCommand.String()
		cmd = trimContinuationBackslash(cmd)

		record := ZshRecord{
			Type:       typeShellCommand,
			User:       username,
			Shell:      string(TypeZsh),
			Command:    cmd,
			Timestamp:  pendingTimestamp,
			Duration:   pendingDuration,
			LineNumber: pendingLineNumber,
			SourceFile: sourcePath,
		}
		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("scanner error: %v", err))
	}

	return records, warnings
}

// parseZshExtendedLine attempts to parse a line in zsh extended history format.
// The format is `: timestamp:duration;command`
// Returns the parsed timestamp, duration, command, and whether it was extended format.
func parseZshExtendedLine(line string) (*time.Time, int, string, bool) {
	matches := zshExtendedRe.FindStringSubmatch(line)
	if matches == nil {
		return nil, 0, "", false
	}

	// Parse timestamp
	unix, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return nil, 0, "", false
	}

	// Sanity check: timestamp should be reasonable
	const minTimestamp = 631152000  // 1990-01-01
	const maxTimestamp = 4102444800 // 2100-01-01
	if unix < minTimestamp || unix > maxTimestamp {
		return nil, 0, "", false
	}

	// Parse duration
	duration, err := strconv.Atoi(matches[2])
	if err != nil {
		duration = 0
	}

	t := time.Unix(unix, 0).UTC()
	return &t, duration, matches[3], true
}

// isLineContinuation checks if a line ends with an unescaped backslash,
// indicating a multi-line command continuation in zsh.
func isLineContinuation(line string) bool {
	if len(line) == 0 {
		return false
	}

	// Count trailing backslashes
	backslashCount := 0
	for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
		backslashCount++
	}

	// Odd number of backslashes means the final one is unescaped (continuation)
	return backslashCount%2 == 1
}

// trimContinuationBackslash removes the trailing continuation backslash from a command
// while preserving escaped backslashes.
func trimContinuationBackslash(cmd string) string {
	if len(cmd) == 0 {
		return cmd
	}

	// Count trailing backslashes
	backslashCount := 0
	for i := len(cmd) - 1; i >= 0 && cmd[i] == '\\'; i-- {
		backslashCount++
	}

	// If odd number, remove the final unescaped backslash
	if backslashCount%2 == 1 {
		return cmd[:len(cmd)-1]
	}

	return cmd
}
