// Package shell implements the shell history collector plugin for macfor.
// It collects and parses shell history files from bash, zsh, fish, and sh shells.
package shell

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

// locator resolves artifact paths from the manifest via the shared collect
// package. Shell history locations carry no app-version constraints, so no
// AppVersionFunc is configured.
var locator *collect.Locator

// PluginID is the unique identifier for the shell history plugin.
const PluginID = "shell.history"

// typeShellCommand is the record type for a parsed shell history command.
const typeShellCommand = "shell_command"

// Type identifies a specific shell type (bash, zsh, fish, sh).
type Type string

// Shell type constants.
const (
	TypeBash Type = "bash"
	TypeZsh  Type = "zsh"
	TypeSh   Type = "sh"
	TypeFish Type = "fish"
)

// shellArtifactIDs maps shell types to their artifact IDs in artifacts.yaml.
// History file locations are defined in the manifest and resolved at runtime
// via the shared collect.Locator, not hardcoded here.
var shellArtifactIDs = map[Type]string{
	TypeBash: "bash_history",
	TypeZsh:  "zsh_history",
	TypeSh:   "sh_history",
	TypeFish: "fish_history",
}

// Parser-kind names declared by the manifest's `parser:` fields. To switch a
// shell's parser, change its `parser:` in artifacts.yaml and register the
// handler in nativeParsers.
const (
	parserZshExtended = "zsh_extended"
	parserBashSimple  = "bash_simple"
	parserFishYAML    = "fish_yaml"
	parserShSimple    = "sh_simple"
)

// artifactHandler collects and parses one shell's history for a single user.
type artifactHandler func(ctx context.Context, user macfor.User, src macfor.Source, out macfor.Collector) error

// nativeParsers maps the parser-kind names declared in artifacts.yaml to their
// implementations. Shell history formats (line-oriented text, fish YAML) have
// no declarative extract backend, so all four are native parsers. A dispatch
// guard test keeps this registry in sync with the manifest.
var nativeParsers = map[string]artifactHandler{
	parserZshExtended: collectZshHistory,
	parserBashSimple:  collectBashHistory,
	parserFishYAML:    collectFishHistory,
	parserShSimple:    collectShHistory,
}

// CollectionOrder defines the order in which shells are collected.
var CollectionOrder = []Type{TypeZsh, TypeBash, TypeFish, TypeSh}

// MinMacOSVersion is the minimum supported macOS version (Sierra).
const MinMacOSVersion = "10.12"

// maxScanBufferSize is the maximum line length for bufio.Scanner (1MB).
// The default 64KB limit can silently drop very long commands (e.g., base64
// payloads, curl with large JSON bodies).
const maxScanBufferSize = 1024 * 1024

// Plugin implements the macfor.Plugin interface for shell history collection.
type Plugin struct{}

// RegisterPlugin registers the shell history plugin with the macfor registry.
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

// Info returns metadata about the shell history plugin.
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
// It verifies the OS version is supported and checks if any history files exist.
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

	// Enumerate users
	users, err := src.Users()
	if err != nil {
		return result, err
	}

	if len(users) == 0 {
		result.Reason = "no users found on system"
		return result, nil
	}

	// Check for history files and track which shells are present
	shellsDetected := make(map[Type]int)
	usersWithHistory := 0

	for _, user := range users {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		userHasHistory := false

		// Presence is driven by the manifest: each shell's history location
		// (and path type) comes from artifacts.yaml via the locator.
		for shell, artifactID := range shellArtifactIDs {
			if locator.Present(src, user, artifactID) {
				shellsDetected[shell]++
				userHasHistory = true
			}
		}

		if userHasHistory {
			usersWithHistory++
		}
	}

	// Determine if plugin is applicable
	if len(shellsDetected) == 0 {
		result.Reason = "no shell history files found for any user"
		return result, nil
	}

	// Build detection result
	result.Applicable = true
	result.Confidence = 1.0
	result.Reason = "found shell history files"

	// Store detected shells in metadata
	detectedShells := make([]string, 0, len(shellsDetected))
	for shell, count := range shellsDetected {
		detectedShells = append(detectedShells, string(shell))
		result.Metadata[string(shell)+"_count"] = count
	}
	result.Metadata["shells_detected"] = detectedShells
	result.Metadata["users_with_history"] = usersWithHistory
	result.Metadata["total_users"] = len(users)

	return result, nil
}

// Collect performs the actual artifact collection.
// It iterates through all users, collecting shell history for each supported shell
// in the order: zsh → bash → fish → sh.
//
// The method aggregates errors without stopping collection. If any collection
// succeeds, the method returns nil even if some users or shells failed.
// Only if the user enumeration fails does this return an error.
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

	out.Log(macfor.LogInfo, "Starting shell history collection for %d user(s)", len(users))

	var collectionErrors []error

	// Collect shell history for each user in the defined order
	for _, user := range users {
		out.Log(macfor.LogInfo, "Collecting shell history for user: %s", user.Username)

		// Collect shells in defined order: zsh → bash → fish → sh
		for _, shellType := range CollectionOrder {
			// Check for context cancellation
			if ctx.Err() != nil {
				out.Log(macfor.LogWarn, "Collection cancelled")
				return ctx.Err()
			}

			err := p.collectShell(ctx, shellType, user, src, out)
			if err != nil {
				collectionErrors = append(collectionErrors, fmt.Errorf("%s history for %s: %w", shellType, user.Username, err))
				out.Log(macfor.LogError, "Error collecting %s history for %s: %v", shellType, user.Username, err)
			}
		}
	}

	out.Log(macfor.LogInfo, "Shell history collection completed for %d user(s)", len(users))

	// Return combined errors if any occurred
	if len(collectionErrors) > 0 {
		return errors.Join(collectionErrors...)
	}

	return nil
}

// collectShell dispatches collection of one shell's history based on the
// parser the manifest declares for its artifact, keeping the manifest
// authoritative for parse-path selection.
func (p *Plugin) collectShell(ctx context.Context, shellType Type, user macfor.User, src macfor.Source, out macfor.Collector) error {
	artifactID, ok := shellArtifactIDs[shellType]
	if !ok {
		return fmt.Errorf("unknown shell type: %s", shellType)
	}

	def := artifacts.GetArtifactByID(manifest, artifactID)
	if def == nil {
		return fmt.Errorf("artifact %q not defined in manifest", artifactID)
	}

	handler, ok := nativeParsers[def.Parser]
	if !ok {
		return fmt.Errorf("artifact %q references unknown parser %q", artifactID, def.Parser)
	}
	return handler(ctx, user, src, out)
}

// Manifest returns the plugin's parsed artifact manifest, exposing it to
// manifest tooling via the macfor.ManifestProvider interface.
func (p *Plugin) Manifest() *artifacts.Manifest {
	return manifest
}
