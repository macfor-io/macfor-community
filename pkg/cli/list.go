package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/spf13/cobra"
)

// List command flags.
var (
	listCategory string
	listJSON     bool
)

// PluginOutput represents a plugin in JSON output format.
type PluginOutput struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Categories  []string `json:"categories"`
	Author      string   `json:"author,omitempty"`
}

// newListCmd creates and returns the list command with all subcommands configured.
func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List available resources",
		Long:  `List available resources such as plugins and their metadata.`,
		Run: func(cmd *cobra.Command, _ []string) {
			_ = cmd.Help()
		},
	}

	// Create plugins subcommand.
	pluginsSubCmd := &cobra.Command{
		Use:   cmdPlugins,
		Short: "List available plugins",
		Long: `List all registered plugins with their metadata.

By default, displays plugins in a human-readable table format.
Use --json for machine-readable output.
Use --category to filter by plugin category.

Examples:
  # List all plugins
  macfor list plugins

  # List plugins in JSON format
  macfor list plugins --json

  # List plugins in the "shell" category
  macfor list plugins --category shell

  # List plugins in the "user" category as JSON
  macfor list plugins --category user --json`,
		RunE: runListPlugins,
	}

	// Add flags to plugins subcommand.
	pluginsSubCmd.Flags().StringVarP(&listCategory, "category", "c", "", "Filter plugins by category")
	pluginsSubCmd.Flags().BoolVar(&listJSON, "json", false, "Output in JSON format")

	// Add plugins as a subcommand of list.
	cmd.AddCommand(pluginsSubCmd)

	return cmd
}

// runListPlugins executes the plugin listing logic.
func runListPlugins(cmd *cobra.Command, _ []string) error {
	// Get plugins based on category filter.
	var plugins []macfor.Plugin
	if listCategory != "" {
		plugins = macfor.ByCategory(listCategory)
	} else {
		plugins = macfor.All()
	}

	// Sort plugins by ID for consistent output.
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Info().ID < plugins[j].Info().ID
	})

	// Output based on format.
	if listJSON {
		return outputPluginsJSON(cmd, plugins)
	}
	return outputPluginsTable(cmd, plugins)
}

// outputPluginsTable outputs plugins in a human-readable table format.
func outputPluginsTable(cmd *cobra.Command, plugins []macfor.Plugin) error {
	if len(plugins) == 0 {
		if listCategory != "" {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No plugins found in category %q\n", listCategory)
		} else {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No plugins registered")
		}
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)

	// Print header.
	_, _ = fmt.Fprintln(w, "ID\tNAME\tVERSION\tCATEGORIES")

	// Print each plugin.
	for _, p := range plugins {
		info := p.Info()
		categories := strings.Join(info.Categories, ", ")
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", info.ID, info.Name, info.Version, categories)
	}

	return w.Flush()
}

// outputPluginsJSON outputs plugins in JSON format.
func outputPluginsJSON(cmd *cobra.Command, plugins []macfor.Plugin) error {
	output := make([]PluginOutput, len(plugins))

	for i, p := range plugins {
		info := p.Info()
		output[i] = PluginOutput{
			ID:          info.ID,
			Name:        info.Name,
			Description: info.Description,
			Version:     info.Version,
			Categories:  info.Categories,
			Author:      info.Author,
		}
	}

	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
