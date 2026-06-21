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

// FishRecord represents a parsed fish history command.
type FishRecord struct {
	// Type identifies this as a shell command record.
	Type string `json:"type"`

	// User is the username who executed the command.
	User string `json:"user"`

	// Shell identifies the shell type (always "fish" for fish records).
	Shell string `json:"shell"`

	// Command is the actual command that was executed.
	Command string `json:"command"`

	// Timestamp is when the command was executed (nil if not available).
	Timestamp *time.Time `json:"timestamp,omitempty"`

	// Paths contains any paths referenced in the command (fish tracks these).
	Paths []string `json:"paths,omitempty"`

	// LineNumber is the line number in the source history file where the
	// "- cmd:" line appears.
	LineNumber int `json:"line_number"`

	// SourceFile is the path to the history file this command was parsed from.
	SourceFile string `json:"source_file"`
}

// collectFishHistory collects and parses fish history for a user.
// It handles both raw file collection and parsing of commands.
//
// The function:
//  1. Collects the raw ~/.local/share/fish/fish_history file
//  2. Parses YAML format with cmd:, when:, and paths: fields
//
// Error handling:
//   - Missing file: Returns nil (expected, user may not use fish)
//   - Permission denied: Logs warning, continues with other users
//   - Empty file: Collects file but skips record generation
//   - Parse errors: Logs warning, continues with remaining entries
//
//nolint:dupl // Shell collectors follow the same pattern by design
func collectFishHistory(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	historyPath, err := locator.Path(src, user, shellArtifactIDs[TypeFish])
	if err != nil {
		return err
	}
	if historyPath == "" {
		out.Log(macfor.LogDebug, "No fish history for user %s: no path for this OS", user.Username)
		return nil
	}

	// Check if the file exists
	fileInfo, err := src.Stat(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Missing file is expected - user may not use fish
			out.Log(macfor.LogDebug, "No fish history for user %s: file not found", user.Username)
			return nil
		}
		if errors.Is(err, fs.ErrPermission) {
			// Permission denied - log warning and continue
			out.Log(macfor.LogWarn, "Cannot access fish history for user %s: permission denied", user.Username)
			return nil
		}
		// Other stat errors
		return fmt.Errorf("stat fish history for %s: %w", user.Username, err)
	}

	// Create artifact for the raw file
	artifact, err := macfor.NewArtifact(PluginID, "shell", "fish_history")
	if err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	artifact.WithSourcePath(historyPath).
		WithUsername(user.Username).
		WithDescription(fmt.Sprintf("Fish command history for user %s", user.Username)).
		WithOriginalFile(fileInfo.ModTime(), fileInfo.Size()).
		WithMetadata("shell_type", "fish")

	// Open the file for reading
	reader, err := src.Open(historyPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			out.Log(macfor.LogWarn, "Cannot read fish history for user %s: permission denied", user.Username)
			return nil
		}
		return fmt.Errorf("open fish history for %s: %w", user.Username, err)
	}
	defer func() { _ = reader.Close() }()

	// Read the entire file content into memory so we can:
	// 1. Add the raw file to the collector
	// 2. Parse the commands
	content, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read fish history for %s: %w", user.Username, err)
	}

	// Add raw file to collector
	if err := out.AddFile(ctx, *artifact, strings.NewReader(string(content))); err != nil {
		out.AddError(*artifact, err)
		out.Log(macfor.LogError, "Failed to add fish history file for user %s: %v", user.Username, err)
		// Continue with parsing even if file addition fails
	}

	// Skip parsing if file is empty
	if len(content) == 0 {
		out.Log(macfor.LogDebug, "Fish history for user %s is empty", user.Username)
		return nil
	}

	// Parse commands and add records
	records, parseWarnings := parseFishHistory(string(content), user.Username, historyPath)

	// Log any parse warnings
	for _, warning := range parseWarnings {
		out.Log(macfor.LogWarn, "Fish history parse warning for %s: %s", user.Username, warning)
	}

	// Add each parsed record
	for _, record := range records {
		if err := out.AddRecord(ctx, *artifact, record); err != nil {
			out.AddError(*artifact, fmt.Errorf("add record at line %d: %w", record.LineNumber, err))
		}
	}

	out.Log(macfor.LogInfo, "Collected %d fish commands for user %s", len(records), user.Username)

	return nil
}

// parseFishHistory parses fish history content into structured records.
// Fish uses a YAML-like format:
//
//   - cmd: ls -la
//     when: 1706012400
//     paths:
//   - /var/log
//
// Returns the parsed records and any warnings encountered during parsing.
func parseFishHistory(content, username, sourcePath string) ([]FishRecord, []string) {
	var records []FishRecord
	var warnings []string

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, bufio.MaxScanTokenSize), maxScanBufferSize)
	lineNumber := 0

	// Current entry being parsed
	var currentCmd string
	var currentWhen *time.Time
	var currentPaths []string
	var currentCmdLine int
	inPaths := false

	// Helper to finalize the current entry
	finalizeEntry := func() {
		if currentCmd != "" {
			record := FishRecord{
				Type:       typeShellCommand,
				User:       username,
				Shell:      string(TypeFish),
				Command:    currentCmd,
				Timestamp:  currentWhen,
				Paths:      currentPaths,
				LineNumber: currentCmdLine,
				SourceFile: sourcePath,
			}
			records = append(records, record)
		}
		// Reset state
		currentCmd = ""
		currentWhen = nil
		currentPaths = nil
		currentCmdLine = 0
		inPaths = false
	}

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check for new entry (starts with "- cmd:")
		if strings.HasPrefix(line, "- cmd:") {
			// Finalize previous entry if any
			finalizeEntry()

			// Start new entry
			currentCmdLine = lineNumber
			currentCmd = strings.TrimPrefix(line, "- cmd:")
			currentCmd = strings.TrimSpace(currentCmd)
			// Handle commands that might be on the next line if this line is empty
			if currentCmd == "" {
				// Command might be empty or on a continuation
				currentCmd = ""
			}
			continue
		}

		// Check for when: field
		if strings.HasPrefix(strings.TrimSpace(line), "when:") {
			inPaths = false
			whenStr := strings.TrimPrefix(strings.TrimSpace(line), "when:")
			whenStr = strings.TrimSpace(whenStr)
			if ts := parseFishTimestamp(whenStr); ts != nil {
				currentWhen = ts
			}
			continue
		}

		// Check for paths: field (starts the paths list)
		if strings.HasPrefix(strings.TrimSpace(line), "paths:") {
			inPaths = true
			continue
		}

		// If we're in paths mode and line starts with "- ", it's a path entry
		if inPaths && strings.HasPrefix(strings.TrimSpace(line), "- ") {
			path := strings.TrimPrefix(strings.TrimSpace(line), "- ")
			path = strings.TrimSpace(path)
			if path != "" {
				currentPaths = append(currentPaths, path)
			}
			continue
		}

		// If line doesn't match expected patterns, exit paths mode
		if inPaths && !strings.HasPrefix(strings.TrimSpace(line), "- ") {
			inPaths = false
		}
	}

	// Finalize last entry
	finalizeEntry()

	if err := scanner.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("scanner error: %v", err))
	}

	return records, warnings
}

// parseFishTimestamp attempts to parse a fish history timestamp.
// The timestamp is a Unix epoch integer.
// Returns the parsed time if successful, nil otherwise.
func parseFishTimestamp(s string) *time.Time {
	unix, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}

	// Sanity check: timestamp should be reasonable
	const minTimestamp = 631152000  // 1990-01-01
	const maxTimestamp = 4102444800 // 2100-01-01
	if unix < minTimestamp || unix > maxTimestamp {
		return nil
	}

	t := time.Unix(unix, 0).UTC()
	return &t
}
