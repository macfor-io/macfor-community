// Package users implements the user accounts collector plugin for macfor.
// It enumerates every local account in the directory services node and
// records account metadata. It never collects password hashes or keys.
package users

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/collect"
)

//go:embed artifacts.yaml
var manifestData []byte

// manifest is the parsed plugin manifest.
var manifest *artifacts.Manifest

// locator resolves artifact paths from the manifest via the shared collect
// package. Account locations carry no app-version constraints.
var locator *collect.Locator

// PluginID is the unique identifier for the user accounts plugin.
const PluginID = "system.users"

// MinMacOSVersion is the minimum supported macOS version (Sierra).
const MinMacOSVersion = "10.12"

// ArtifactType identifies a specific user accounts artifact type.
type ArtifactType string

// Artifact type constants.
const (
	ArtifactLocalAccounts ArtifactType = "local_accounts"
)

// CollectionOrder defines the order in which artifacts are collected.
var CollectionOrder = []ArtifactType{ArtifactLocalAccounts}

// parserDSLocal is the parser-kind name declared by the manifest's `parser:`
// field for the local directory node's account plists.
const parserDSLocal = "users.dslocal"

// artifactHandler collects and parses one artifact type. Account artifacts are
// system-wide, so handlers take no user.
type artifactHandler func(ctx context.Context, src macfor.Source, out macfor.Collector) error

// nativeParsers maps the parser-kind names declared in artifacts.yaml to their
// implementations. Each account plist is a single dictionary of string arrays
// with embedded property-list data, which the declarative plist mapper does
// not cover. A dispatch guard test keeps this registry in sync with the
// manifest.
var nativeParsers = map[string]artifactHandler{
	parserDSLocal: collectLocalAccounts,
}

// systemUser is the zero user passed to the locator. Account paths are
// absolute and contain no ~ or {username} tokens.
var systemUser = macfor.User{}

// Plugin implements the macfor.Plugin interface for user account collection.
type Plugin struct{}

// RegisterPlugin registers the user accounts plugin with the macfor registry.
// This is called automatically via init() but can also be called explicitly
// for testing or dynamic registration scenarios.
func RegisterPlugin() {
	macfor.Register(&Plugin{})
}

//nolint:gochecknoinits // Plugin registration requires init
func init() {
	manifest = artifacts.MustParse(manifestData)
	locator = collect.NewLocator(manifest)
	RegisterPlugin()
}

// Info returns metadata about the user accounts plugin.
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
// It verifies the OS version is supported and that account plists are
// readable. On a live system the directory node is readable only by root, so
// a non-root collection reports the plugin as not applicable with that reason.
func (p *Plugin) Detect(ctx context.Context, src macfor.Source) (macfor.DetectionResult, error) {
	result := macfor.DetectionResult{
		Applicable: false,
		Confidence: 0.0,
		Metadata:   make(map[string]any),
	}

	osVersion, err := src.OSVersion()
	if err != nil {
		return result, err
	}
	result.OSVersion = osVersion

	if !artifacts.VersionAtLeast(osVersion, MinMacOSVersion) {
		result.Reason = "macOS version " + osVersion + " is below minimum supported version " + MinMacOSVersion
		return result, nil
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	paths, err := accountPlistPaths(src)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, macfor.ErrPermissionDenied) {
			result.Reason = "local account plists are not readable (requires root)"
			return result, nil
		}
		return result, err
	}

	if len(paths) == 0 {
		result.Reason = "no local account plists found"
		return result, nil
	}

	result.Applicable = true
	result.Confidence = 1.0
	result.Reason = "found local account plists"
	result.Metadata["account_plists"] = len(paths)

	return result, nil
}

// Collect enumerates the local accounts and adds one record per account.
func (p *Plugin) Collect(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	var collectionErrors []error

	for _, artifactType := range CollectionOrder {
		if ctx.Err() != nil {
			out.Log(macfor.LogWarn, "Collection cancelled")
			return ctx.Err()
		}

		if err := p.collectArtifact(ctx, artifactType, src, out); err != nil {
			collectionErrors = append(collectionErrors, fmt.Errorf("%s: %w", artifactType, err))
			out.Log(macfor.LogError, "Error collecting %s: %v", artifactType, err)
		}
	}

	if len(collectionErrors) > 0 {
		return errors.Join(collectionErrors...)
	}

	return nil
}

// collectArtifact dispatches collection of one artifact based on the parser
// the manifest declares for it, keeping the manifest authoritative for
// parse-path selection.
func (p *Plugin) collectArtifact(ctx context.Context, artifactType ArtifactType, src macfor.Source, out macfor.Collector) error {
	def := artifacts.GetArtifactByID(manifest, string(artifactType))
	if def == nil {
		return fmt.Errorf("artifact %q not defined in manifest", artifactType)
	}

	handler, ok := nativeParsers[def.Parser]
	if !ok {
		return fmt.Errorf("artifact %q references unknown parser %q", artifactType, def.Parser)
	}
	return handler(ctx, src, out)
}

// Manifest returns the plugin's parsed artifact manifest, exposing it to
// manifest tooling via the macfor.ManifestProvider interface.
func (p *Plugin) Manifest() *artifacts.Manifest {
	return manifest
}
