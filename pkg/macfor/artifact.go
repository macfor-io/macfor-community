package macfor

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Artifact contains metadata about a collected artifact.
type Artifact struct {
	// PluginID is the ID of the plugin that collected this artifact.
	// Must be in format "category.name" (e.g., "shell.history").
	PluginID string

	// Category is the high-level category of this artifact (e.g., "shell", "browser").
	Category string

	// Type describes the specific artifact type (e.g., "history", "cookies").
	Type string

	// SourcePath is the original path on the source system where this artifact was found.
	SourcePath string

	// Username is the username associated with this artifact, if user-specific.
	// Empty string for system-level artifacts.
	Username string

	// Description provides a human-readable description of this artifact.
	Description string

	// CollectedAt is the timestamp when this artifact was collected.
	CollectedAt time.Time

	// OriginalModTime is the modification time of the original file.
	OriginalModTime time.Time

	// OriginalSize is the size in bytes of the original file.
	OriginalSize int64

	// Metadata contains additional artifact-specific key-value pairs.
	Metadata map[string]any
}

// NewArtifact creates a new Artifact with the given required fields and validates it.
// PluginID, Category, and Type are required. CollectedAt defaults to the current time
// if not explicitly set after creation.
func NewArtifact(pluginID, category, artifactType string) (*Artifact, error) {
	a := &Artifact{
		PluginID:    pluginID,
		Category:    category,
		Type:        artifactType,
		CollectedAt: time.Now(),
		Metadata:    make(map[string]any),
	}

	if err := a.Validate(); err != nil {
		return nil, err
	}

	return a, nil
}

// Validate checks that the artifact has all required fields and valid formats.
// Returns an error describing the first validation failure, or nil if valid.
func (a *Artifact) Validate() error {
	if a.PluginID == "" {
		return fmt.Errorf("artifact validation: %w: PluginID is required", ErrParseError)
	}

	if !strings.Contains(a.PluginID, ".") {
		return fmt.Errorf("artifact validation: %w: PluginID must be in format 'category.name' (got %q)", ErrParseError, a.PluginID)
	}

	if a.Category == "" {
		return fmt.Errorf("artifact validation: %w: Category is required", ErrParseError)
	}

	if a.Type == "" {
		return fmt.Errorf("artifact validation: %w: Type is required", ErrParseError)
	}

	return nil
}

// ContainerPath returns the path where this artifact should be stored within
// the evidence container. The path format is:
//
//	artifacts/{category}/{plugin_id}/files/{user}/{filename}
//
// For system-level artifacts (empty Username), "system" is used as the user directory.
// The filename is extracted from SourcePath if available, otherwise falls back to Type.
//
// This is the requested path, not necessarily the stored one. Two files from
// the same plugin and user that share a base name (e.g. Chrome's
// Default/History and Profile 1/History) map to the same ContainerPath; the
// evidence container then stores the later file at a counter variant
// (History~2, report~2.md) and records the actual path in hashes.json and the
// chain-of-custody entry (with "requested_path" and "renamed_for_collision").
// Records that reference a collected file by ContainerPath therefore cannot
// know a collision suffix; resolve such references through the custody log.
func (a *Artifact) ContainerPath() string {
	// Determine the user directory
	userDir := a.Username
	if userDir == "" {
		userDir = "system"
	}

	// Extract filename from SourcePath or fall back to Type
	filename := a.Type
	if a.SourcePath != "" {
		filename = filepath.Base(a.SourcePath)
	}

	// Build the container path
	return filepath.Join(
		"artifacts",
		a.Category,
		a.PluginID,
		"files",
		userDir,
		filename,
	)
}

// RecordPath returns the path where parsed records for this artifact should be stored.
// The path format is:
//
//	records/{plugin_id}/parsed.jsonl
func (a *Artifact) RecordPath() string {
	return filepath.Join("records", a.PluginID, "parsed.jsonl")
}

// WithSourcePath sets the source path and returns the artifact for chaining.
func (a *Artifact) WithSourcePath(path string) *Artifact {
	a.SourcePath = path
	return a
}

// WithUsername sets the username and returns the artifact for chaining.
func (a *Artifact) WithUsername(username string) *Artifact {
	a.Username = username
	return a
}

// WithDescription sets the description and returns the artifact for chaining.
func (a *Artifact) WithDescription(desc string) *Artifact {
	a.Description = desc
	return a
}

// WithOriginalFile sets the original file metadata and returns the artifact for chaining.
func (a *Artifact) WithOriginalFile(modTime time.Time, size int64) *Artifact {
	a.OriginalModTime = modTime
	a.OriginalSize = size
	return a
}

// WithMetadata adds a key-value pair to the metadata and returns the artifact for chaining.
func (a *Artifact) WithMetadata(key string, value any) *Artifact {
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}
	a.Metadata[key] = value
	return a
}
