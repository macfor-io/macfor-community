package macfor

import (
	"context"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

// Plugin is the interface all artifact collectors must implement.
// Plugins are self-contained modules that know how to detect and collect
// specific artifact types from a forensic source.
type Plugin interface {
	// Info returns metadata about this plugin including its ID, name,
	// supported platforms, and categorization.
	Info() PluginInfo

	// Detect checks whether this plugin is applicable to the given source.
	// It examines OS version, file existence, and other factors to determine
	// if collection should proceed.
	Detect(ctx context.Context, src Source) (DetectionResult, error)

	// Collect performs the actual artifact collection from the source,
	// writing files and records to the provided Collector.
	Collect(ctx context.Context, src Source, out Collector) error
}

// ManifestProvider is implemented by plugins that expose their parsed artifact
// manifest. Manifest tooling (`macfor manifest matrix`, `paths`, `docs`) reads
// manifests through this interface, so the embedded artifacts.yaml each plugin
// already parses at init is the single source served everywhere — no
// filesystem lookups at runtime.
//
// Implementing it is a one-line method on the plugin:
//
//	func (p *Plugin) Manifest() *artifacts.Manifest { return manifest }
type ManifestProvider interface {
	// Manifest returns the plugin's parsed artifact manifest.
	Manifest() *artifacts.Manifest
}

// PluginInfo contains metadata about a plugin.
type PluginInfo struct {
	// ID is the unique identifier for this plugin (e.g., "shell.history", "browsers.safari").
	ID string

	// Name is the human-readable name of the plugin.
	Name string

	// Description provides a brief explanation of what artifacts this plugin collects.
	Description string

	// Version is the semantic version of this plugin.
	Version string

	// Categories are tags used for filtering and grouping plugins
	// (e.g., ["user", "command", "shell"]).
	Categories []string

	// Platforms specifies which platforms and OS versions this plugin supports.
	Platforms []PlatformSpec

	// Author is the plugin author's name or organization (optional).
	Author string
}

// PlatformSpec defines platform and version constraints for a plugin.
type PlatformSpec struct {
	// OS is the operating system identifier (e.g., "darwin", "linux", "windows").
	OS string

	// MinVersion is the minimum OS version required (e.g., "10.12" for macOS Sierra).
	// Empty string means no minimum version constraint.
	MinVersion string

	// MaxVersion is the maximum OS version supported (inclusive).
	// Empty string means no maximum version constraint.
	MaxVersion string
}

// DetectionResult contains the outcome of a plugin's detection phase.
type DetectionResult struct {
	// Applicable indicates whether this plugin should run on the source.
	Applicable bool

	// Reason explains why the plugin is or is not applicable.
	Reason string

	// AppVersion is the version of the application being collected (if applicable).
	AppVersion string

	// OSVersion is the OS version detected on the source.
	OSVersion string

	// Confidence indicates how confident the detection is (0.0 to 1.0).
	Confidence float64

	// Workflow identifies which collection workflow to use based on version
	// (e.g., "v14", "v15-17", "v18+").
	Workflow string

	// Metadata contains additional detection-specific key-value pairs.
	Metadata map[string]any
}
