// Package safari implements the Safari browser collector plugin for macfor.
// It collects and parses Safari browsing history, downloads, bookmarks,
// cookies, extensions, local storage, and session data.
package safari

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/collect"
)

//go:embed artifacts.yaml
var manifestData []byte

// manifest is the parsed plugin manifest.
var manifest *artifacts.Manifest

// locator resolves artifact paths from the manifest (see paths.go and the
// shared collect package).
var locator *collect.Locator

// PluginID is the unique identifier for the Safari browser plugin.
const PluginID = "browser.safari"

// MinMacOSVersion is the minimum supported macOS version (Sierra).
const MinMacOSVersion = "10.12"

// Safari artifact locations are defined in the embedded artifacts.yaml manifest
// and resolved at runtime via the shared collect.Locator. Only the constants
// below remain in Go: LocalStoragePath (used by tests to place fixtures) and
// SafariAppInfoPlist (a system path used for version detection, not a per-user
// artifact).
const (
	// LocalStoragePath is the path to Safari local storage databases,
	// relative to the user home directory.
	LocalStoragePath = "Library/WebKit/WebsiteData/LocalStorage"

	// SafariAppInfoPlist is the path to Safari app Info.plist for version detection.
	SafariAppInfoPlist = "/Applications/Safari.app/Contents/Info.plist"
)

// Context keys supplied to the table mapper for declarative extraction. They
// match the `context:` references in artifacts.yaml.
const (
	ctxUsername   = "username"
	ctxSourceFile = "source_file"
	ctxOrigin     = "origin"
)

// categoryBrowser is the artifact category for Safari artifacts.
const categoryBrowser = "browser"

// browserSafari is the browser name stamped on Safari records.
const browserSafari = "safari"

// ArtifactType identifies a specific Safari artifact type.
type ArtifactType string

// Artifact type constants.
const (
	ArtifactHistory      ArtifactType = "history"
	ArtifactDownloads    ArtifactType = "downloads"
	ArtifactBookmarks    ArtifactType = "bookmarks"
	ArtifactReadingList  ArtifactType = "reading_list"
	ArtifactCookies      ArtifactType = "cookies"
	ArtifactExtensions   ArtifactType = "extensions"
	ArtifactLocalStorage ArtifactType = "local_storage"
	ArtifactLastSession  ArtifactType = "last_session"
	ArtifactTopSites     ArtifactType = "top_sites"
)

// CollectionOrder defines the order in which artifacts are collected.
// The order ensures dependencies are met (e.g., History.db is collected first
// as the primary artifact).
var CollectionOrder = []ArtifactType{
	ArtifactHistory,
	ArtifactDownloads,
	ArtifactBookmarks,
	ArtifactCookies,
	ArtifactExtensions,
	ArtifactLocalStorage,
	ArtifactLastSession,
}

// CollectionOptions controls which Safari artifacts are collected.
// By default, all artifacts are collected. Use these flags to skip
// specific artifact types for privacy or performance reasons.
type CollectionOptions struct {
	// SkipCookies skips collection of Cookies.binarycookies.
	// Useful for privacy-sensitive collections where session tokens
	// should not be captured.
	SkipCookies bool

	// SkipLocalStorage skips collection of LocalStorage databases.
	// Useful when website data is not needed or for faster collection.
	SkipLocalStorage bool

	// HistoryOnly collects only the History.db file, skipping all other artifacts.
	// Useful for quick triage or when only browsing history is needed.
	HistoryOnly bool
}

// DefaultCollectionOptions returns the default options that collect all artifacts.
func DefaultCollectionOptions() CollectionOptions {
	return CollectionOptions{}
}

// Plugin implements the macfor.Plugin interface for Safari artifact collection.
type Plugin struct {
	// Options controls which artifacts are collected.
	// If nil, defaults to collecting all artifacts.
	Options *CollectionOptions
}

// RegisterPlugin registers the Safari plugin with the macfor registry.
// This is called automatically via init() but can also be called explicitly
// for testing or dynamic registration scenarios.
func RegisterPlugin() {
	macfor.Register(&Plugin{})
}

//nolint:gochecknoinits // Plugin registration requires init
func init() {
	manifest = artifacts.MustParse(manifestData)
	locator = collect.NewLocator(manifest, collect.WithAppVersion(detectSafariVersion))
	RegisterPlugin()
}

// Info returns metadata about the Safari browser plugin.
// The information is derived from the embedded artifacts.yaml manifest.
func (p *Plugin) Info() macfor.PluginInfo {
	platforms := make([]macfor.PlatformSpec, len(manifest.Plugin.Platforms))
	for i, plat := range manifest.Plugin.Platforms {
		platforms[i] = macfor.PlatformSpec{
			OS:         plat.OS,
			MinVersion: plat.MinVersion,
			MaxVersion: plat.MaxVersion,
		}
	}

	return macfor.PluginInfo{
		ID:          manifest.Plugin.ID,
		Name:        manifest.Plugin.Name,
		Description: manifest.Plugin.Description,
		Version:     manifest.Plugin.Version,
		Categories:  manifest.Plugin.Categories,
		Platforms:   platforms,
		Author:      "macfor",
	}
}

// Detect checks whether this plugin is applicable to the source.
// It verifies the OS version is supported, checks if Safari is installed,
// and looks for Safari artifacts for any user.
func (p *Plugin) Detect(ctx context.Context, src macfor.Source) (macfor.DetectionResult, error) {
	result := macfor.DetectionResult{
		Applicable: false,
		Confidence: 0.0,
		Metadata:   make(map[string]any),
	}

	// Check OS version
	osVersion, err := src.OSVersion()
	if err != nil {
		return result, err
	}
	result.OSVersion = osVersion

	// Verify macOS version is supported
	if !artifacts.VersionAtLeast(osVersion, MinMacOSVersion) {
		result.Reason = "macOS version " + osVersion + " is below minimum supported version " + MinMacOSVersion
		return result, nil
	}

	// Detect Safari version
	safariVersion := detectSafariVersion(src)
	if safariVersion != "" {
		result.AppVersion = safariVersion
		result.Metadata["safari_version"] = safariVersion
	}

	// Enumerate users
	users, err := src.Users()
	if err != nil {
		return result, err
	}

	if len(users) == 0 {
		result.Reason = "no users found on system"
		return result, nil
	}

	// Check for Safari artifacts and track what's present
	artifactsDetected := make(map[ArtifactType]int)
	usersWithSafari := 0

	for _, user := range users {
		// Check context for cancellation
		if ctx.Err() != nil {
			return result, ctx.Err()
		}

		userHasArtifacts := false

		// Presence is driven entirely by the embedded manifest: each artifact's
		// paths — including their version constraints and path types (file, glob,
		// directory) — come from artifacts.yaml rather than hardcoded constants.
		for _, artifactType := range CollectionOrder {
			if locator.Present(src, user, string(artifactType)) {
				artifactsDetected[artifactType]++
				userHasArtifacts = true
			}
		}

		if userHasArtifacts {
			usersWithSafari++
		}
	}

	// Determine if plugin is applicable
	if len(artifactsDetected) == 0 {
		result.Reason = "no Safari artifacts found for any user"
		return result, nil
	}

	// Build detection result
	result.Applicable = true
	result.Confidence = 1.0
	result.Reason = "found Safari artifacts"

	// Store detected artifacts in metadata
	detectedArtifacts := make([]string, 0, len(artifactsDetected))
	for artifact, count := range artifactsDetected {
		detectedArtifacts = append(detectedArtifacts, string(artifact))
		result.Metadata[string(artifact)+"_count"] = count
	}
	result.Metadata["artifacts_detected"] = detectedArtifacts
	result.Metadata["users_with_safari"] = usersWithSafari
	result.Metadata["total_users"] = len(users)

	return result, nil
}

// Collect performs the actual artifact collection.
// It iterates through all users, collecting Safari artifacts for each user
// in the order defined by CollectionOrder.
//
// The method aggregates errors without stopping collection. Individual artifact
// failures are logged and collected, but do not abort the overall process.
// The aggregated errors are returned so the orchestrator can report partial failures.
// Missing files are not treated as errors (each sub-collector returns nil).
//
// Collection respects the plugin's Options field for selective collection:
//   - HistoryOnly: Only collects History.db
//   - SkipCookies: Skips Cookies.binarycookies
//   - SkipLocalStorage: Skips LocalStorage databases
func (p *Plugin) Collect(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	// Enumerate users
	users, err := src.Users()
	if err != nil {
		return fmt.Errorf("enumerating users: %w", err)
	}

	if len(users) == 0 {
		out.Log(macfor.LogWarn, "No users found on system")
		return nil
	}

	// Get effective options
	opts := p.getOptions()

	out.Log(macfor.LogInfo, "Starting Safari collection for %d user(s)", len(users))

	var collectionErrors []error

	// Collect Safari artifacts for each user
	for _, user := range users {
		out.Log(macfor.LogInfo, "Collecting Safari data for user: %s", user.Username)

		// Collect artifacts in defined order
		for _, artifactType := range CollectionOrder {
			// Check for context cancellation
			if ctx.Err() != nil {
				out.Log(macfor.LogWarn, "Collection cancelled")
				return ctx.Err()
			}

			// Skip artifacts based on options
			if p.shouldSkipArtifact(artifactType, opts) {
				out.Log(macfor.LogDebug, "Skipping %s for user %s (disabled by options)", artifactType, user.Username)
				continue
			}

			err := p.collectArtifact(ctx, artifactType, user, src, out)
			if err != nil {
				collectionErrors = append(collectionErrors, fmt.Errorf("%s for %s: %w", artifactType, user.Username, err))
				out.Log(macfor.LogError, "Error collecting %s for %s: %v", artifactType, user.Username, err)
			}
		}
	}

	// Log summary
	p.logSummary(len(users), collectionErrors, out)

	// Return combined errors if any occurred
	if len(collectionErrors) > 0 {
		return errors.Join(collectionErrors...)
	}

	return nil
}

// getOptions returns the effective collection options.
// If Options is nil, returns default options (collect all).
func (p *Plugin) getOptions() CollectionOptions {
	if p.Options != nil {
		return *p.Options
	}
	return DefaultCollectionOptions()
}

// shouldSkipArtifact determines if an artifact type should be skipped
// based on the collection options.
func (p *Plugin) shouldSkipArtifact(artifactType ArtifactType, opts CollectionOptions) bool {
	// If HistoryOnly is set, skip everything except history
	if opts.HistoryOnly && artifactType != ArtifactHistory {
		return true
	}

	// Skip cookies if requested
	if opts.SkipCookies && artifactType == ArtifactCookies {
		return true
	}

	// Skip local storage if requested
	if opts.SkipLocalStorage && artifactType == ArtifactLocalStorage {
		return true
	}

	return false
}

// artifactHandler collects and parses one artifact type for a single user.
// Each handler owns its file acquisition and record extraction.
type artifactHandler func(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error

// Parser-kind names for Safari's native (non-declarative) parsers. These are
// referenced from the `parser:` field of the matching artifact in
// artifacts.yaml, so an artifact's parse path can be redirected there without
// touching dispatch code.
const (
	parserBookmarks   = "safari.bookmarks"
	parserLastSession = "safari.lastsession"
	parserCookies     = "safari.cookies"
)

// nativeParsers organizes the custom parsers for formats that are not
// declaratively expressible — recursive plist trees (bookmarks, last session)
// and the opaque binary cookies format — keyed by the parser-kind name declared
// in the manifest. To switch an artifact onto a different parser, change its
// `parser:` field in artifacts.yaml and register the handler here.
var nativeParsers = map[string]artifactHandler{
	parserBookmarks:   collectBookmarksForUser,
	parserLastSession: collectLastSessionForUser,
	parserCookies:     collectCookiesForUser,
}

// declarativeHandlers handles artifacts whose records are produced by a
// declarative mapper (their manifest entry carries an `extract:` block) — the
// table mapper for SQLite, the plist mapper for property lists.
var declarativeHandlers = map[ArtifactType]artifactHandler{
	ArtifactHistory:      collectHistoryForUser,
	ArtifactLocalStorage: collectLocalStorageForUser,
	ArtifactExtensions:   collectExtensionsForUser,
	ArtifactDownloads:    collectDownloadsForUser,
}

// collectArtifact dispatches collection of one artifact based on how the
// manifest says it is parsed: an `extract:` block routes to the declarative
// table mapper; a `parser:` name routes to the matching native parser. This
// keeps the manifest authoritative for parse-path selection.
func (p *Plugin) collectArtifact(ctx context.Context, artifactType ArtifactType, user macfor.User, src macfor.Source, out macfor.Collector) error {
	def := artifacts.GetArtifactByID(manifest, string(artifactType))
	if def == nil {
		return fmt.Errorf("artifact %q not defined in manifest", artifactType)
	}

	switch {
	case def.Extract != nil:
		handler, ok := declarativeHandlers[artifactType]
		if !ok {
			return fmt.Errorf("artifact %q declares extract but has no declarative handler", artifactType)
		}
		return handler(ctx, user, src, out)
	case def.Parser != "":
		handler, ok := nativeParsers[def.Parser]
		if !ok {
			return fmt.Errorf("artifact %q references unknown parser %q", artifactType, def.Parser)
		}
		return handler(ctx, user, src, out)
	default:
		out.Log(macfor.LogDebug, "No parser configured for %s; skipping", artifactType)
		return nil
	}
}

// logSummary logs a summary of the collection results.
func (p *Plugin) logSummary(userCount int, errs []error, out macfor.Collector) {
	if len(errs) == 0 {
		out.Log(macfor.LogInfo, "Safari collection completed for %d user(s) with no errors", userCount)
	} else {
		out.Log(macfor.LogInfo, "Safari collection completed for %d user(s) with %d error(s)", userCount, len(errs))
	}
}

// detectSafariVersion reads the Safari app Info.plist to determine the installed version.
// Returns empty string if Safari is not installed or version cannot be determined.
// It is also the locator's AppVersionFunc, so app_min/app_max constraints in
// the manifest are enforced during path resolution.
func detectSafariVersion(src macfor.Source) string {
	// Try to read Safari's Info.plist
	var plistData struct {
		CFBundleShortVersionString string `plist:"CFBundleShortVersionString"`
		CFBundleVersion            string `plist:"CFBundleVersion"`
	}

	err := src.ReadPlist(SafariAppInfoPlist, &plistData)
	if err != nil {
		return ""
	}

	// Prefer CFBundleShortVersionString (e.g., "17.2.1")
	if plistData.CFBundleShortVersionString != "" {
		return plistData.CFBundleShortVersionString
	}

	// Fall back to CFBundleVersion
	return plistData.CFBundleVersion
}

// Manifest returns the plugin's parsed artifact manifest, exposing it to
// manifest tooling via the macfor.ManifestProvider interface.
func (p *Plugin) Manifest() *artifacts.Manifest {
	return manifest
}
