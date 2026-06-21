package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/spf13/cobra"
)

// manifestMarkdown selects markdown output for the matrix command.
var manifestMarkdown bool

// Catalog label constants.
const (
	// parsedByExtract labels artifacts parsed by the declarative table mapper.
	parsedByExtract = "declarative (extract)"

	// parsedByNative labels artifacts collected and parsed by native plugin code
	// (the location is manifest-driven; the parsing is hand-written Go).
	parsedByNative = "native"

	// parsedByPlanned labels artifacts whose location is declared for forward
	// documentation but which the plugin does not yet collect.
	parsedByPlanned = "planned"

	// versionRangeAll labels an unbounded (always-applicable) version range.
	versionRangeAll = "all"
)

// newManifestMatrixCmd builds the `manifest matrix` command, which renders the
// artifact location catalog: what artifacts a plugin collects, where they live,
// and the OS/app-version ranges each location applies to. It is the canonical
// view for managing artifact-location changes across OS and app versions.
func newManifestMatrixCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "matrix",
		Short: "Show the artifact location catalog (per OS and app version)",
		Long: `Show, per plugin, every collectible artifact and the version-aware
locations it maps to. Unlike "manifest paths" (which resolves one OS version),
the matrix lists each location's OS-version and app-version applicability so
changes across releases can be reviewed and managed at a glance.

The "Parsed by" column shows how each artifact's records are produced:
declarative table extraction ("declarative (extract)"), a named native
parser ("native: <name>"), or hand-written native code ("native").
Artifacts declared for forward documentation but not yet collected show
as "planned".

Examples:
  # Catalog for one plugin (terminal table)
  macfor manifest matrix --plugin browser.safari

  # All plugins
  macfor manifest matrix

  # Markdown (for the docs/kb site)
  macfor manifest matrix --plugin browser.safari --markdown

  # JSON (for tooling)
  macfor manifest matrix --json`,
		RunE: runManifestMatrix,
	}

	cmd.Flags().StringVarP(&manifestPluginID, "plugin", "p", "", "Filter by plugin ID")
	cmd.Flags().BoolVar(&manifestJSON, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&manifestMarkdown, "markdown", false, "Output as Markdown tables")

	return cmd
}

// CatalogRow is a single (artifact, location) entry in the catalog.
type CatalogRow struct {
	Artifact string `json:"artifact"`
	Name     string `json:"name"`
	Format   string `json:"format"`
	ParsedBy string `json:"parsed_by"`
	OSRange  string `json:"os_range"`
	AppRange string `json:"app_range"`
	Type     string `json:"type"`
	Location string `json:"location"`
	Optional bool   `json:"optional"`
}

// CatalogResult is the artifact location catalog for one plugin.
type CatalogResult struct {
	PluginID   string       `json:"plugin_id"`
	PluginName string       `json:"plugin_name"`
	Rows       []CatalogRow `json:"rows"`
}

// runManifestMatrix executes the catalog rendering.
func runManifestMatrix(cmd *cobra.Command, _ []string) error {
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

	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Info().ID < plugins[j].Info().ID
	})

	var results []CatalogResult
	for _, p := range plugins {
		manifest := getPluginManifest(p.Info().ID)
		if manifest == nil {
			continue
		}
		results = append(results, buildCatalog(manifest))
	}

	switch {
	case manifestJSON:
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	case manifestMarkdown:
		return outputCatalogMarkdown(cmd, results)
	default:
		return outputCatalogTable(cmd, results)
	}
}

// buildCatalog flattens a manifest into one row per (artifact, location).
func buildCatalog(m *artifacts.Manifest) CatalogResult {
	result := CatalogResult{
		PluginID:   m.Plugin.ID,
		PluginName: m.Plugin.Name,
	}

	for i := range m.Artifacts {
		a := &m.Artifacts[i]
		parsedBy := parsedByLabel(a)
		for _, ps := range a.Paths {
			pathType := ps.Type
			if pathType == "" {
				pathType = artifacts.PathTypeFile
			}
			result.Rows = append(result.Rows, CatalogRow{
				Artifact: a.ID,
				Name:     a.Name,
				Format:   a.Format,
				ParsedBy: parsedBy,
				OSRange:  formatVersionRange(ps.OSMin, ps.OSMax),
				AppRange: formatVersionRange(ps.AppMin, ps.AppMax),
				Type:     pathType,
				Location: ps.Pattern,
				Optional: a.Optional,
			})
		}
	}
	return result
}

// parsedByLabel describes how an artifact's records are produced. Planned
// artifacts (declared for forward documentation, not yet collected) are reported
// as "planned". Otherwise the artifact is collected: by the declarative table
// mapper (extract), by a named native parser, or — the norm for the pro plugins
// whose locations are manifest-driven but whose parsing is hand-written — by
// native code ("native").
func parsedByLabel(a *artifacts.Artifact) string {
	switch {
	case a.Planned:
		return parsedByPlanned
	case a.Extract != nil:
		return parsedByExtract
	case a.Parser != "":
		return "native: " + a.Parser
	default:
		return parsedByNative
	}
}

// formatVersionRange renders an inclusive [min, max] version range compactly.
// Empty bounds are unbounded: "" / "" -> "all".
func formatVersionRange(minVer, maxVer string) string {
	switch {
	case minVer == "" && maxVer == "":
		return versionRangeAll
	case minVer != "" && maxVer == "":
		return minVer + "+"
	case minVer == "" && maxVer != "":
		return "≤" + maxVer
	default:
		return minVer + "–" + maxVer
	}
}

// outputCatalogTable renders the catalog as aligned terminal tables.
func outputCatalogTable(cmd *cobra.Command, results []CatalogResult) error {
	out := cmd.OutOrStdout()
	if len(results) == 0 {
		_, _ = fmt.Fprintln(out, "No manifests found")
		return nil
	}

	for _, r := range results {
		_, _ = fmt.Fprintf(out, "\n%s (%s) — %d locations\n", r.PluginName, r.PluginID, len(r.Rows))
		tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ARTIFACT\tFORMAT\tPARSED BY\tOS\tAPP\tTYPE\tLOCATION")
		for _, row := range r.Rows {
			opt := ""
			if row.Optional {
				opt = " (optional)"
			}
			_, _ = fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Artifact, opt, row.Format, row.ParsedBy, row.OSRange, row.AppRange, row.Type, row.Location)
		}
		_ = tw.Flush()
	}
	return nil
}

// outputCatalogMarkdown renders the catalog as Markdown tables, one per plugin.
func outputCatalogMarkdown(cmd *cobra.Command, results []CatalogResult) error {
	out := cmd.OutOrStdout()
	for _, r := range results {
		_, _ = fmt.Fprintf(out, "## %s (`%s`)\n\n", r.PluginName, r.PluginID)
		_, _ = fmt.Fprintln(out, "| Artifact | Format | Parsed by | OS | App | Type | Location |")
		_, _ = fmt.Fprintln(out, "|----------|--------|-----------|----|-----|------|----------|")
		for _, row := range r.Rows {
			name := row.Artifact
			if row.Optional {
				name += " _(optional)_"
			}
			_, _ = fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | `%s` |\n",
				name, row.Format, row.ParsedBy, mdDash(row.OSRange), mdDash(row.AppRange), row.Type, row.Location)
		}
		_, _ = fmt.Fprintln(out)
	}
	return nil
}

// mdDash renders an "all" version range as an em dash for compactness in Markdown.
func mdDash(s string) string {
	if s == versionRangeAll {
		return "—"
	}
	return s
}
