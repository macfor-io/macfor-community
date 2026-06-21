package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// mockPlugin implements macfor.Plugin for testing.
type mockPlugin struct {
	info macfor.PluginInfo
}

func (m *mockPlugin) Info() macfor.PluginInfo {
	return m.info
}

func (m *mockPlugin) Detect(_ context.Context, _ macfor.Source) (macfor.DetectionResult, error) {
	return macfor.DetectionResult{}, nil
}

func (m *mockPlugin) Collect(_ context.Context, _ macfor.Source, _ macfor.Collector) error {
	return nil
}

// registerTestPlugins registers mock plugins for testing and returns a cleanup function.
func registerTestPlugins() func() {
	// Clear any existing plugins.
	macfor.ClearRegistry()

	// Register test plugins.
	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.alpha",
			Name:        "Alpha Test",
			Description: "First test plugin",
			Version:     "1.0.0",
			Categories:  []string{"test", "alpha"},
			Author:      "test-author",
		},
	})

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.beta",
			Name:        "Beta Test",
			Description: "Second test plugin",
			Version:     "2.0.0",
			Categories:  []string{"test", "beta"},
			Author:      "test-author",
		},
	})

	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:          "other.gamma",
			Name:        "Gamma Other",
			Description: "Third test plugin",
			Version:     "3.0.0",
			Categories:  []string{"other"},
		},
	})

	// Return cleanup function.
	return func() {
		macfor.ClearRegistry()
	}
}

func TestListPlugins_TableOutput(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify header is present.
	if !strings.Contains(output, "ID") {
		t.Error("output missing ID header")
	}
	if !strings.Contains(output, "NAME") {
		t.Error("output missing NAME header")
	}
	if !strings.Contains(output, "VERSION") {
		t.Error("output missing VERSION header")
	}
	if !strings.Contains(output, "CATEGORIES") {
		t.Error("output missing CATEGORIES header")
	}

	// Verify all plugins are listed.
	if !strings.Contains(output, "test.alpha") {
		t.Error("output missing test.alpha plugin")
	}
	if !strings.Contains(output, "test.beta") {
		t.Error("output missing test.beta plugin")
	}
	if !strings.Contains(output, "other.gamma") {
		t.Error("output missing other.gamma plugin")
	}

	// Verify plugin details.
	if !strings.Contains(output, "Alpha Test") {
		t.Error("output missing Alpha Test name")
	}
	if !strings.Contains(output, "1.0.0") {
		t.Error("output missing version 1.0.0")
	}
	if !strings.Contains(output, "test, alpha") {
		t.Error("output missing categories 'test, alpha'")
	}
}

func TestListPlugins_JSONOutput(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify JSON is valid.
	var plugins []PluginOutput
	if err := json.Unmarshal([]byte(output), &plugins); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	// Verify plugin count.
	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(plugins))
	}

	// Verify plugins are sorted by ID.
	expectedOrder := []string{"other.gamma", "test.alpha", "test.beta"}
	for i, expected := range expectedOrder {
		if plugins[i].ID != expected {
			t.Errorf("expected plugin %d to be %s, got %s", i, expected, plugins[i].ID)
		}
	}

	// Find and verify a specific plugin.
	var alpha *PluginOutput
	for i := range plugins {
		if plugins[i].ID == "test.alpha" {
			alpha = &plugins[i]
			break
		}
	}

	if alpha == nil {
		t.Fatal("test.alpha plugin not found in output")
	}

	if alpha.Name != "Alpha Test" {
		t.Errorf("expected name 'Alpha Test', got %q", alpha.Name)
	}
	if alpha.Description != "First test plugin" {
		t.Errorf("expected description 'First test plugin', got %q", alpha.Description)
	}
	if alpha.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", alpha.Version)
	}
	if alpha.Author != "test-author" {
		t.Errorf("expected author 'test-author', got %q", alpha.Author)
	}
	if len(alpha.Categories) != 2 || alpha.Categories[0] != "test" || alpha.Categories[1] != "alpha" {
		t.Errorf("expected categories [test, alpha], got %v", alpha.Categories)
	}
}

func TestListPlugins_CategoryFilter(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins", "--category", "test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify test category plugins are present.
	if !strings.Contains(output, "test.alpha") {
		t.Error("output missing test.alpha plugin")
	}
	if !strings.Contains(output, "test.beta") {
		t.Error("output missing test.beta plugin")
	}

	// Verify non-test category plugin is NOT present.
	if strings.Contains(output, "other.gamma") {
		t.Error("output should not contain other.gamma plugin")
	}
}

func TestListPlugins_CategoryFilterJSON(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins", "--category", "other", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	var plugins []PluginOutput
	if err := json.Unmarshal([]byte(output), &plugins); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	// Only one plugin should be in "other" category.
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}

	if plugins[0].ID != "other.gamma" {
		t.Errorf("expected other.gamma, got %s", plugins[0].ID)
	}
}

func TestListPlugins_NoPlugins(t *testing.T) {
	// Clear registry - no plugins registered.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "No plugins registered") {
		t.Errorf("expected 'No plugins registered' message, got: %s", output)
	}
}

func TestListPlugins_NoCategoryMatch(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins", "--category", "nonexistent"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, `No plugins found in category "nonexistent"`) {
		t.Errorf("expected no plugins message, got: %s", output)
	}
}

func TestListPlugins_NoPluginsJSON(t *testing.T) {
	// Clear registry - no plugins registered.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	var plugins []PluginOutput
	if err := json.Unmarshal([]byte(output), &plugins); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}

	if len(plugins) != 0 {
		t.Errorf("expected empty array, got %d plugins", len(plugins))
	}
}

func TestListPlugins_ShortCategoryFlag(t *testing.T) {
	cleanup := registerTestPlugins()
	defer cleanup()

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	// Use short flag -c.
	rootCmd.SetArgs([]string{"list", "plugins", "-c", "alpha"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Only alpha category plugin should be present.
	if !strings.Contains(output, "test.alpha") {
		t.Error("output missing test.alpha plugin")
	}
	if strings.Contains(output, "test.beta") {
		t.Error("output should not contain test.beta plugin")
	}
}

func TestListCommand_Help(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"list", "--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	if !strings.Contains(output, "List available resources") {
		t.Error("help missing description")
	}
	if !strings.Contains(output, "plugins") {
		t.Error("help missing plugins subcommand")
	}
}

func TestListPluginsCommand_Help(t *testing.T) {
	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"list", "plugins", "--help"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify examples are shown.
	if !strings.Contains(output, "macfor list plugins") {
		t.Error("help missing example")
	}
	if !strings.Contains(output, "--json") {
		t.Error("help missing --json flag")
	}
	if !strings.Contains(output, "--category") {
		t.Error("help missing --category flag")
	}
	if !strings.Contains(output, "-c") {
		t.Error("help missing -c short flag")
	}
}

func TestListPlugins_SortedOutput(t *testing.T) {
	// Clear and register plugins in reverse order.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	// Register in reverse alphabetical order.
	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{ID: "z.last", Name: "Last", Version: "1.0.0"},
	})
	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{ID: "a.first", Name: "First", Version: "1.0.0"},
	})
	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{ID: "m.middle", Name: "Middle", Version: "1.0.0"},
	})

	resetTestState()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Reset flags before test.
	listCategory = ""
	listJSON = false

	rootCmd.SetArgs([]string{"list", "plugins"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	lines := strings.Split(output, "\n")

	// Find plugin lines (skip header).
	var pluginIDs []string
	for _, line := range lines {
		if strings.HasPrefix(line, "a.") || strings.HasPrefix(line, "m.") || strings.HasPrefix(line, "z.") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				pluginIDs = append(pluginIDs, parts[0])
			}
		}
	}

	// Verify alphabetical order.
	expected := []string{"a.first", "m.middle", "z.last"}
	if len(pluginIDs) != len(expected) {
		t.Fatalf("expected %d plugins, got %d", len(expected), len(pluginIDs))
	}

	for i, id := range expected {
		if pluginIDs[i] != id {
			t.Errorf("expected plugin %d to be %s, got %s", i, id, pluginIDs[i])
		}
	}
}
