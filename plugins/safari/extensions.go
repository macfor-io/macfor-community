package safari

import (
	"context"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// ExtensionRecord represents a Safari extension from Extensions.plist.
// Each record corresponds to a single installed Safari extension.
type ExtensionRecord struct {
	// Type identifies this record type for downstream processing.
	Type string `json:"type"`

	// Browser identifies the source browser.
	Browser string `json:"browser"`

	// User is the username who has this extension installed.
	User string `json:"user"`

	// BundleID is the unique bundle identifier for the extension.
	// Example: "com.agilebits.onepassword-safari".
	BundleID string `json:"bundle_id"`

	// ArchiveFileName is the original extension archive file name.
	// Example: "1Password.safariextz".
	ArchiveFileName string `json:"archive_file_name"`

	// Enabled indicates whether the extension is currently enabled.
	Enabled bool `json:"enabled"`

	// SourceFile is the path to the Extensions.plist file on the source system.
	SourceFile string `json:"source_file"`
}

// collectExtensionsForUser collects Safari extensions from Extensions.plist via
// the declarative plist mapper (driven by the manifest's extract block).
func collectExtensionsForUser(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error {
	return collectPlistForUser(ctx, user, src, out, plistCollectSpec{
		artifactType: ArtifactExtensions,
		category:     categoryBrowser,
		artifactKind: "extensions",
		description:  "Safari browser extensions",
		newRecord:    func() any { return &ExtensionRecord{} },
	})
}
