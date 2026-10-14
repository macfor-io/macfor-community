// Package artifacts provides a configuration-driven approach to declaring
// artifact locations, version compatibility, and collection metadata.
//
// The artifact manifest system replaces hardcoded paths in plugin code with
// declarative YAML manifests that serve as the single source of truth for
// artifact definitions.
package artifacts

// Manifest represents a complete plugin artifact manifest.
type Manifest struct {
	// SchemaVersion is the manifest schema version for forward compatibility.
	SchemaVersion string `yaml:"schema_version"`

	// Plugin contains plugin metadata.
	Plugin Plugin `yaml:"plugin"`

	// Artifacts defines the collectible artifacts.
	Artifacts []Artifact `yaml:"artifacts"`

	// Profiles holds plugin-specific named profile/location strings (used by
	// the browser plugins for values such as the Local State filename, the
	// default profile directory, or the profiles.ini filename). Keys are
	// plugin-defined; resolve them via collect.ProfileValue so the manifest
	// remains the single source of truth.
	Profiles map[string]string `yaml:"profiles,omitempty"`

	// Variants enumerates application variants (e.g. Chrome / Chrome Canary /
	// Chromium, or Firefox / Developer Edition / Nightly) with their data
	// directory and application-bundle locations. Resolve via collect.Variants.
	Variants []VariantSpec `yaml:"variants,omitempty"`

	// source holds the exact bytes the manifest was parsed from (nil for a
	// manifest built in code). Unexported, so it never round-trips through YAML.
	source []byte
}

// Source returns the exact bytes the manifest was parsed from — for an
// embedded manifest, the plugin's artifacts.yaml as built into the binary. It
// returns nil for a manifest that wasn't created by Parse or
// ParseWithoutValidation. Manifest provenance hashes these bytes, so a recorded
// hash can be checked with `shasum -a 256 artifacts.yaml`. Callers must not
// modify the returned slice.
func (m *Manifest) Source() []byte {
	return m.source
}

// VariantSpec describes one application variant's data directory and bundle
// location. Browser plugins enumerate variants from the manifest rather than
// hardcoding per-variant path tables.
type VariantSpec struct {
	// ID is the variant identifier (e.g. "chrome", "chromium").
	ID string `yaml:"id"`

	// Name is the human-readable variant name (e.g. "Google Chrome").
	Name string `yaml:"name"`

	// Path is the home-relative data directory for the variant
	// (e.g. "Library/Application Support/Google/Chrome").
	Path string `yaml:"path"`

	// AppPath is the absolute application-bundle path used for version
	// detection (e.g. "/Applications/Google Chrome.app"). Optional.
	AppPath string `yaml:"app_path,omitempty"`
}

// Plugin contains plugin metadata.
type Plugin struct {
	// ID is the unique plugin identifier matching Plugin.Info().ID.
	ID string `yaml:"id"`

	// Name is the human-readable plugin name.
	Name string `yaml:"name"`

	// Description is a brief description of the plugin.
	Description string `yaml:"description"`

	// Version is the plugin version in semver format.
	Version string `yaml:"version"`

	// Categories are classification tags for filtering.
	Categories []string `yaml:"categories"`

	// Platforms defines OS compatibility constraints.
	Platforms []Platform `yaml:"platforms"`
}

// Platform defines OS compatibility constraints.
type Platform struct {
	// OS is the operating system identifier (e.g., "darwin").
	OS string `yaml:"os"`

	// MinVersion is the minimum supported OS version (inclusive).
	MinVersion string `yaml:"min_version,omitempty"`

	// MaxVersion is the maximum supported OS version (inclusive).
	// Empty string means no upper bound.
	MaxVersion string `yaml:"max_version,omitempty"`
}

// Artifact defines a single collectible artifact.
type Artifact struct {
	// ID is the unique identifier within the plugin.
	ID string `yaml:"id"`

	// Name is the human-readable artifact name.
	Name string `yaml:"name"`

	// Category is the forensic category for organisation.
	Category string `yaml:"category"`

	// Format is the file format hint for collection strategy.
	Format string `yaml:"format"`

	// Paths are the version-aware path specifications.
	Paths []PathSpec `yaml:"paths"`

	// Related are file patterns for related files to collect.
	Related []string `yaml:"related,omitempty"`

	// Schema contains database schema information for different versions.
	Schema []SchemaSpec `yaml:"schema,omitempty"`

	// Parser is a hint for format variations.
	Parser string `yaml:"parser,omitempty"`

	// Extract declares declarative tabular extraction (SQLite/JSONL). Optional;
	// when nil the artifact is parsed by native Go.
	Extract *ExtractSpec `yaml:"extract,omitempty"`

	// Optional indicates whether missing artifact is expected.
	Optional bool `yaml:"optional,omitempty"`

	// RequiresRoot indicates whether artifact requires elevated privileges.
	RequiresRoot bool `yaml:"requires_root,omitempty"`

	// Planned marks an artifact whose location is declared for forward-looking
	// documentation but is not yet collected by the plugin. The catalog reports
	// these as "planned" rather than implying the tool collects them, and the
	// per-plugin drift guard treats them as legitimately uncollected.
	Planned bool `yaml:"planned,omitempty"`

	// Metadata contains custom key-value pairs for plugin-specific needs.
	Metadata map[string]string `yaml:"metadata,omitempty"`
}

// PathSpec defines a path pattern with version constraints.
type PathSpec struct {
	// Pattern is the path pattern with expansion tokens.
	// Supported tokens: ~, {username}, *, **.
	Pattern string `yaml:"pattern"`

	// Type is the path type: file, glob, or directory.
	// Defaults to "file".
	Type string `yaml:"type,omitempty"`

	// OSMin is the minimum macOS version for this path (inclusive).
	OSMin string `yaml:"os_min,omitempty"`

	// OSMax is the maximum macOS version for this path (inclusive).
	OSMax string `yaml:"os_max,omitempty"`

	// AppMin is the minimum application version for this path (inclusive).
	AppMin string `yaml:"app_min,omitempty"`

	// AppMax is the maximum application version for this path (inclusive).
	AppMax string `yaml:"app_max,omitempty"`
}

// SchemaSpec defines database schema for a version range.
type SchemaSpec struct {
	// OSRange is the macOS version range [min, max] for this schema.
	// Empty string for min/max means unbounded.
	OSRange [2]string `yaml:"os_range"`

	// Tables are the table names present in this schema version.
	Tables []string `yaml:"tables,omitempty"`

	// NewColumns are columns added in this version, keyed by table name.
	NewColumns map[string][]string `yaml:"new_columns,omitempty"`

	// RemovedColumns are columns removed in this version, keyed by table name.
	RemovedColumns map[string][]string `yaml:"removed_columns,omitempty"`

	// VersionID is the internal schema version identifier.
	VersionID string `yaml:"version_id,omitempty"`
}

// Format constants for artifact file formats.
const (
	FormatSQLite        = "sqlite"
	FormatBinaryPlist   = "binary_plist"
	FormatXMLPlist      = "xml_plist"
	FormatText          = "text"
	FormatYAML          = "yaml"
	FormatJSON          = "json"
	FormatBinary        = "binary"
	FormatBinaryCookies = "binary_cookies"
	FormatDirectory     = "directory"
	FormatLevelDB       = "leveldb"
	FormatSNSS          = "snss"
	FormatJSONL         = "jsonl"
)

// Category constants for artifact forensic categories.
const (
	CategoryHistory        = "history"
	CategoryDownloads      = "downloads"
	CategoryBookmarks      = "bookmarks"
	CategoryCookies        = "cookies"
	CategoryStorage        = "storage"
	CategorySession        = "session"
	CategoryExtensions     = "extensions"
	CategoryLogs           = "logs"
	CategoryCommandHistory = "command_history"
	CategoryConfig         = "config"
	CategoryCache          = "cache"
	CategoryCredentials    = "credentials"
	CategoryAutofill       = "autofill"
	CategoryContacts       = "contacts"
	CategoryMedia          = "media"
	CategoryMetadata       = "metadata"
	CategoryMessaging      = "messaging"
	CategoryCalls          = "calls"
	CategoryEmail          = "email"
	CategoryPersistence    = "persistence"
)

// PathType constants for path specification types.
const (
	PathTypeFile      = "file"
	PathTypeGlob      = "glob"
	PathTypeDirectory = "directory"
)
