package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/spf13/cobra"
)

// unknownVersion is returned when the OS version cannot be determined.
const unknownVersion = "unknown"

// Manifest command flags.
var (
	manifestOSVersion string
	manifestJSON      bool
	manifestPluginID  string
	manifestOutputDir string
)

// newManifestCmd creates and returns the manifest command with all subcommands.
func newManifestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdManifest,
		Short: "Manage artifact manifests",
		Long: `Manage and inspect artifact manifest files.

Artifact manifests define the locations and version compatibility
of forensic artifacts collected by each plugin.`,
		Run: func(cmd *cobra.Command, _ []string) {
			_ = cmd.Help()
		},
	}

	// Add subcommands.
	cmd.AddCommand(newManifestValidateCmd())
	cmd.AddCommand(newManifestPathsCmd())
	cmd.AddCommand(newManifestMatrixCmd())
	cmd.AddCommand(newManifestDocsCmd())
	cmd.AddCommand(newManifestPackCmd())

	return cmd
}

// newManifestValidateCmd creates the validate subcommand.
func newManifestValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [files...]",
		Short: "Validate manifest files",
		Long: `Validate artifact manifest files for correctness.

If no files are specified, validates all manifests from registered plugins.

Examples:
  # Validate a specific manifest file
  macfor manifest validate plugins/safari/artifacts.yaml

  # Validate multiple manifest files
  macfor manifest validate plugins/*/artifacts.yaml

  # Validate all registered plugin manifests
  macfor manifest validate`,
		RunE: runManifestValidate,
	}

	cmd.Flags().BoolVar(&manifestJSON, "json", false, "Output in JSON format")

	return cmd
}

// ValidateResult represents the result of validating a manifest.
type ValidateResult struct {
	File     string `json:"file"`
	PluginID string `json:"plugin_id,omitempty"`
	Valid    bool   `json:"valid"`
	Error    string `json:"error,omitempty"`
}

// runManifestValidate executes the manifest validation logic.
func runManifestValidate(cmd *cobra.Command, args []string) error {
	var results []ValidateResult

	if len(args) == 0 {
		// Validate all registered plugins by checking their manifests are valid.
		// Since plugins use MustParse in init(), they would panic if invalid.
		// We just report they are valid.
		plugins := macfor.All()
		if len(plugins) == 0 {
			if manifestJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode([]ValidateResult{})
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins registered")
			return nil
		}

		for _, p := range plugins {
			info := p.Info()
			results = append(results, ValidateResult{
				File:     fmt.Sprintf("(embedded in %s)", info.ID),
				PluginID: info.ID,
				Valid:    true,
			})
		}
	} else {
		// Validate specified files.
		for _, pattern := range args {
			// Expand glob patterns.
			matches, err := filepath.Glob(pattern)
			if err != nil {
				results = append(results, ValidateResult{
					File:  pattern,
					Valid: false,
					Error: fmt.Sprintf("invalid glob pattern: %v", err),
				})
				continue
			}

			if len(matches) == 0 {
				results = append(results, ValidateResult{
					File:  pattern,
					Valid: false,
					Error: "no matching files",
				})
				continue
			}

			for _, file := range matches {
				result := validateManifestFile(file)
				results = append(results, result)
			}
		}
	}

	// Sort results by file.
	sort.Slice(results, func(i, j int) bool {
		return results[i].File < results[j].File
	})

	// Output results.
	if manifestJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	return outputValidateTable(cmd, results)
}

// validateManifestFile validates a single manifest file and returns the result.
func validateManifestFile(file string) ValidateResult {
	data, err := os.ReadFile(file) //nolint:gosec // File path is from user CLI input.
	if err != nil {
		return ValidateResult{
			File:  file,
			Valid: false,
			Error: fmt.Sprintf("reading file: %v", err),
		}
	}

	manifest, err := artifacts.Parse(data)
	if err != nil {
		return ValidateResult{
			File:  file,
			Valid: false,
			Error: err.Error(),
		}
	}

	return ValidateResult{
		File:     file,
		PluginID: manifest.Plugin.ID,
		Valid:    true,
	}
}

// outputValidateTable outputs validation results in a human-readable format.
func outputValidateTable(cmd *cobra.Command, results []ValidateResult) error {
	validCount := 0
	invalidCount := 0

	for _, r := range results {
		var status string
		if r.Valid {
			status = "✓"
			validCount++
		} else {
			status = "✗"
			invalidCount++
		}

		switch {
		case r.Error != "":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s - %s\n", status, r.File, r.Error)
		case r.PluginID != "":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s (%s) - valid\n", status, r.File, r.PluginID)
		default:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s - valid\n", status, r.File)
		}
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nSummary: %d valid, %d invalid\n", validCount, invalidCount)

	if invalidCount > 0 {
		return fmt.Errorf("%d manifest(s) failed validation", invalidCount)
	}
	return nil
}

// newManifestPathsCmd creates the paths subcommand.
func newManifestPathsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdPaths,
		Short: "List artifact paths for a given OS version",
		Long: `List all artifact paths that would be collected for a given OS version.

This is useful for understanding what paths a plugin will access on a
specific macOS version, especially when artifact locations change between
OS releases.

Examples:
  # List paths for a specific plugin on current system
  macfor manifest paths --plugin browser.safari

  # List paths for a specific OS version
  macfor manifest paths --plugin browser.safari --os-version 10.15

  # List all plugin paths
  macfor manifest paths

  # Output as JSON
  macfor manifest paths --json`,
		RunE: runManifestPaths,
	}

	cmd.Flags().StringVarP(&manifestPluginID, "plugin", "p", "", "Filter by plugin ID")
	cmd.Flags().StringVar(&manifestOSVersion, "os-version", "", "macOS version to resolve paths for (default: current system)")
	cmd.Flags().BoolVar(&manifestJSON, "json", false, "Output in JSON format")

	return cmd
}

// PathsResult represents the paths output for a plugin.
type PathsResult struct {
	PluginID   string          `json:"plugin_id"`
	PluginName string          `json:"plugin_name"`
	OSVersion  string          `json:"os_version"`
	Artifacts  []ArtifactPaths `json:"artifacts"`
}

// ArtifactPaths represents the paths for a single artifact.
type ArtifactPaths struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Format   string   `json:"format"`
	Optional bool     `json:"optional"`
	Paths    []string `json:"paths"`
	Related  []string `json:"related,omitempty"`
	Skipped  bool     `json:"skipped,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// runManifestPaths executes the manifest paths listing logic.
func runManifestPaths(cmd *cobra.Command, _ []string) error {
	// Determine OS version.
	osVersion := manifestOSVersion
	if osVersion == "" {
		// Try to get current system OS version.
		osVersion = getCurrentOSVersion()
	}

	// Get plugins to process.
	var plugins []macfor.Plugin
	if manifestPluginID != "" {
		p, found := macfor.ByID(manifestPluginID)
		if !found {
			return fmt.Errorf("plugin not found: %s", manifestPluginID)
		}
		plugins = []macfor.Plugin{p}
	} else {
		plugins = macfor.All()
	}

	if len(plugins) == 0 {
		if manifestJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode([]PathsResult{})
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins registered")
		return nil
	}

	// Sort plugins by ID.
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Info().ID < plugins[j].Info().ID
	})

	var results []PathsResult
	for _, p := range plugins {
		result := getPluginPaths(p, osVersion)
		results = append(results, result)
	}

	// Output results.
	if manifestJSON {
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	}

	return outputPathsTable(cmd, results)
}

// getPluginPaths gets all resolved paths for a plugin at a given OS version.
func getPluginPaths(p macfor.Plugin, osVersion string) PathsResult {
	info := p.Info()
	result := PathsResult{
		PluginID:   info.ID,
		PluginName: info.Name,
		OSVersion:  osVersion,
	}

	// Get the manifest for this plugin.
	// Since we can't access the embedded manifest directly, we use the plugin's
	// manifest through the registry. For now, we simulate path resolution using
	// sample paths based on the plugin ID.
	manifest := getPluginManifest(info.ID)
	if manifest == nil {
		// No manifest available for this plugin.
		return result
	}

	resolver := artifacts.NewResolver(osVersion)

	// Create a sample user for path expansion.
	sampleUser := artifacts.SimpleUser{
		Username: "{user}",
		HomeDir:  "~",
	}

	for _, artifact := range manifest.Artifacts {
		ap := ArtifactPaths{
			ID:       artifact.ID,
			Name:     artifact.Name,
			Category: artifact.Category,
			Format:   artifact.Format,
			Optional: artifact.Optional,
		}

		paths, err := resolver.ResolveArtifact(artifact, sampleUser)
		if err != nil {
			ap.Skipped = true
			ap.Reason = err.Error()
		} else {
			for _, rp := range paths {
				ap.Paths = append(ap.Paths, formatPathWithType(rp.Path, rp.Type))
			}
		}

		// Get related files.
		related := resolver.ResolveRelated(artifact, sampleUser)
		ap.Related = related

		result.Artifacts = append(result.Artifacts, ap)
	}

	return result
}

// formatPathWithType formats a path with its type indicator.
func formatPathWithType(path, pathType string) string {
	switch pathType {
	case artifacts.PathTypeGlob:
		return path + " (glob)"
	case artifacts.PathTypeDirectory:
		return path + " (directory)"
	default:
		return path
	}
}

// getPluginManifest returns the parsed manifest for a registered plugin via
// the macfor.ManifestProvider interface. The manifest is the one each plugin
// embeds and parses at init, so this works from any working directory and for
// any binary that registers the plugin. Returns nil for unregistered plugins
// and for plugins that do not (yet) expose their manifest.
func getPluginManifest(pluginID string) *artifacts.Manifest {
	p, found := macfor.ByID(pluginID)
	if !found {
		return nil
	}
	mp, ok := p.(macfor.ManifestProvider)
	if !ok {
		return nil
	}
	return mp.Manifest()
}

// getCurrentOSVersion attempts to get the current macOS version.
func getCurrentOSVersion() string {
	// Try to read from SystemVersion.plist.
	data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return unknownVersion
	}

	// Simple extraction of ProductVersion from plist.
	content := string(data)
	if idx := strings.Index(content, "<key>ProductVersion</key>"); idx != -1 {
		start := strings.Index(content[idx:], "<string>")
		if start != -1 {
			start += idx + len("<string>")
			end := strings.Index(content[start:], "</string>")
			if end != -1 {
				return content[start : start+end]
			}
		}
	}

	return unknownVersion
}

// outputPathsTable outputs paths results in a human-readable format.
func outputPathsTable(cmd *cobra.Command, results []PathsResult) error {
	for i, result := range results {
		if i > 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s (%s on macOS %s)\n",
			result.PluginID, result.PluginName, result.OSVersion)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("=", 60))

		if len(result.Artifacts) == 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  No artifacts defined")
			continue
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

		for _, artifact := range result.Artifacts {
			if artifact.Skipped {
				_, _ = fmt.Fprintf(w, "\n[skipped] %s:\t%s\n", artifact.ID, artifact.Reason)
				continue
			}

			optionalMark := ""
			if artifact.Optional {
				optionalMark = " (optional)"
			}

			_, _ = fmt.Fprintf(w, "\n%s%s:\n", artifact.ID, optionalMark)

			for _, path := range artifact.Paths {
				_, _ = fmt.Fprintf(w, "  %s\n", path)
			}

			for _, related := range artifact.Related {
				_, _ = fmt.Fprintf(w, "  %s\t(related)\n", related)
			}
		}

		_ = w.Flush()
	}

	return nil
}

// newManifestDocsCmd creates the docs subcommand.
func newManifestDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdDocs,
		Short: "Generate documentation from manifest files",
		Long: `Generate markdown documentation from artifact manifest files.

Creates a markdown file for each plugin describing its artifacts,
paths, and version compatibility.

Examples:
  # Generate docs to default directory (docs/artifacts/)
  macfor manifest docs

  # Generate docs to custom directory
  macfor manifest docs --output ./docs/plugins

  # Generate docs for a specific plugin only
  macfor manifest docs --plugin browser.safari`,
		RunE: runManifestDocs,
	}

	cmd.Flags().StringVarP(&manifestOutputDir, "output", "o", "docs/artifacts", "Output directory for generated documentation")
	cmd.Flags().StringVarP(&manifestPluginID, "plugin", "p", "", "Generate docs for specific plugin only")

	return cmd
}

// runManifestDocs executes the documentation generation logic.
func runManifestDocs(cmd *cobra.Command, _ []string) error {
	// Get plugins to process.
	var plugins []macfor.Plugin
	if manifestPluginID != "" {
		p, found := macfor.ByID(manifestPluginID)
		if !found {
			return fmt.Errorf("plugin not found: %s", manifestPluginID)
		}
		plugins = []macfor.Plugin{p}
	} else {
		plugins = macfor.All()
	}

	if len(plugins) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins registered")
		return nil
	}

	// Create output directory.
	if err := os.MkdirAll(manifestOutputDir, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Sort plugins by ID.
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Info().ID < plugins[j].Info().ID
	})

	generatedFiles := 0
	for _, p := range plugins {
		info := p.Info()
		manifest := getPluginManifest(info.ID)
		if manifest == nil {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "⚠ %s: no manifest file found, skipping\n", info.ID)
			continue
		}

		// Generate documentation.
		doc := generatePluginDoc(manifest)

		// Write to file.
		filename := strings.ReplaceAll(info.ID, ".", "-") + ".md"
		outputPath := filepath.Join(manifestOutputDir, filename)

		if err := os.WriteFile(outputPath, []byte(doc), 0o644); err != nil { //nolint:gosec // Output file from CLI.
			return fmt.Errorf("writing documentation for %s: %w", info.ID, err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "✓ Generated %s\n", outputPath)
		generatedFiles++
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nGenerated %d documentation file(s) in %s\n", generatedFiles, manifestOutputDir)
	return nil
}

// generatePluginDoc generates markdown documentation for a plugin manifest.
func generatePluginDoc(m *artifacts.Manifest) string {
	var sb strings.Builder

	// Header.
	sb.WriteString(fmt.Sprintf("# %s\n\n", m.Plugin.Name))
	sb.WriteString(fmt.Sprintf("**Plugin ID:** `%s`  \n", m.Plugin.ID))
	sb.WriteString(fmt.Sprintf("**Version:** %s  \n", m.Plugin.Version))
	sb.WriteString(fmt.Sprintf("**Categories:** %s\n\n", strings.Join(m.Plugin.Categories, ", ")))

	// Description.
	sb.WriteString("## Description\n\n")
	sb.WriteString(m.Plugin.Description + "\n\n")

	// Platform support.
	sb.WriteString("## Platform Support\n\n")
	sb.WriteString("| OS | Min Version | Max Version |\n")
	sb.WriteString("|:---|:------------|:------------|\n")
	for _, p := range m.Plugin.Platforms {
		maxVer := p.MaxVersion
		if maxVer == "" {
			maxVer = "latest"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", p.OS, p.MinVersion, maxVer))
	}
	sb.WriteString("\n")

	// Artifacts summary table.
	sb.WriteString("## Artifacts\n\n")
	sb.WriteString("| ID | Name | Category | Format | Optional |\n")
	sb.WriteString("|:---|:-----|:---------|:-------|:---------|\n")
	for _, a := range m.Artifacts {
		optional := "No"
		if a.Optional {
			optional = "Yes"
		}
		sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
			a.ID, a.Name, a.Category, a.Format, optional))
	}
	sb.WriteString("\n")

	// Detailed artifact sections.
	sb.WriteString("## Artifact Details\n\n")
	for _, a := range m.Artifacts {
		sb.WriteString(fmt.Sprintf("### %s\n\n", a.Name))
		sb.WriteString(fmt.Sprintf("**ID:** `%s`  \n", a.ID))
		sb.WriteString(fmt.Sprintf("**Category:** %s  \n", a.Category))
		sb.WriteString(fmt.Sprintf("**Format:** %s  \n", a.Format))
		if a.Parser != "" {
			sb.WriteString(fmt.Sprintf("**Parser:** %s  \n", a.Parser))
		}
		if a.Optional {
			sb.WriteString("**Optional:** Yes  \n")
		}
		if a.RequiresRoot {
			sb.WriteString("**Requires Root:** Yes  \n")
		}
		sb.WriteString("\n")

		// Paths.
		sb.WriteString("**Paths:**\n\n")
		for _, p := range a.Paths {
			pathType := p.Type
			if pathType == "" {
				pathType = artifacts.PathTypeFile
			}
			constraints := []string{}
			if p.OSMin != "" {
				constraints = append(constraints, fmt.Sprintf("macOS >= %s", p.OSMin))
			}
			if p.OSMax != "" {
				constraints = append(constraints, fmt.Sprintf("macOS <= %s", p.OSMax))
			}

			constraintStr := ""
			if len(constraints) > 0 {
				constraintStr = " (" + strings.Join(constraints, ", ") + ")"
			}

			sb.WriteString(fmt.Sprintf("- `%s` [%s]%s\n", p.Pattern, pathType, constraintStr))
		}
		sb.WriteString("\n")

		// Related files.
		if len(a.Related) > 0 {
			sb.WriteString("**Related Files:**\n\n")
			for _, r := range a.Related {
				sb.WriteString(fmt.Sprintf("- `%s`\n", r))
			}
			sb.WriteString("\n")
		}

		// Schema information.
		if len(a.Schema) > 0 {
			sb.WriteString("**Schema Versions:**\n\n")
			sb.WriteString("| macOS Range | Tables | Version ID |\n")
			sb.WriteString("|:------------|:-------|:-----------|\n")
			for _, s := range a.Schema {
				osRange := fmt.Sprintf("%s - %s", s.OSRange[0], s.OSRange[1])
				if s.OSRange[1] == "" {
					osRange = fmt.Sprintf("%s+", s.OSRange[0])
				}
				tables := strings.Join(s.Tables, ", ")
				versionID := s.VersionID
				if versionID == "" {
					versionID = "-"
				}
				sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", osRange, tables, versionID))
			}
			sb.WriteString("\n")
		}

		// Metadata.
		if len(a.Metadata) > 0 {
			sb.WriteString("**Metadata:**\n\n")
			for k, v := range a.Metadata {
				fmt.Fprintf(&sb, "- `%s`: %s\n", k, v)
			}
			sb.WriteString("\n")
		}
	}

	// Footer.
	sb.WriteString("---\n\n")
	sb.WriteString("*This documentation was auto-generated from the plugin's artifact manifest.*\n")

	return sb.String()
}
