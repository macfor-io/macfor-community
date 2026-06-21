package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// collectMockPlugin is a mock plugin for collect tests that supports detection and collection.
type collectMockPlugin struct {
	info            macfor.PluginInfo
	detectResult    macfor.DetectionResult
	detectErr       error
	collectErr      error
	collectCallback func(ctx context.Context, src macfor.Source, out macfor.Collector) error
}

func (m *collectMockPlugin) Info() macfor.PluginInfo {
	return m.info
}

func (m *collectMockPlugin) Detect(_ context.Context, _ macfor.Source) (macfor.DetectionResult, error) {
	return m.detectResult, m.detectErr
}

func (m *collectMockPlugin) Collect(ctx context.Context, src macfor.Source, out macfor.Collector) error {
	if m.collectCallback != nil {
		return m.collectCallback(ctx, src, out)
	}
	return m.collectErr
}

// registerCollectTestPlugins registers mock plugins for collect testing.
func registerCollectTestPlugins() func() {
	macfor.ClearRegistry()

	// Register an applicable plugin that succeeds.
	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.applicable",
			Name:        "Applicable Test",
			Description: "Test plugin that is applicable",
			Version:     "1.0.0",
			Categories:  []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "found test artifacts",
			Confidence: 1.0,
		},
	})

	// Register a non-applicable plugin.
	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.notapplicable",
			Name:        "Not Applicable Test",
			Description: "Test plugin that is not applicable",
			Version:     "1.0.0",
			Categories:  []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: false,
			Reason:     "no test artifacts found",
		},
	})

	// Register a plugin in a different category.
	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:          "other.plugin",
			Name:        "Other Plugin",
			Description: "Plugin in other category",
			Version:     "1.0.0",
			Categories:  []string{"other"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "always applicable",
		},
	})

	return func() {
		macfor.ClearRegistry()
	}
}

// resetCollectFlags resets collect command flags to defaults.
func resetCollectFlags() {
	collectOutput = ""
	collectPlugins = nil
	collectCategory = ""
	collectDryRun = false
	collectJSON = false
	collectFrom = ""
}

func TestCollectCommand_MissingOutput(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Run collect without --output.
	rootCmd.SetArgs([]string{"collect"})
	err := rootCmd.Execute()

	if err == nil {
		t.Fatal("expected error for missing --output")
	}

	if !strings.Contains(err.Error(), "--output is required") {
		t.Errorf("expected '--output is required' error, got: %v", err)
	}
}

func TestCollectCommand_DryRunNoOutput(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	// Dry run should work without --output.
	rootCmd.SetArgs([]string{"collect", "--dry-run"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Dry Run") {
		t.Error("expected 'Dry Run' in output")
	}
	if !strings.Contains(output, "Detection Results") {
		t.Error("expected 'Detection Results' in output")
	}
}

func TestCollectCommand_DryRunTableOutput(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Verify header.
	if !strings.Contains(output, "PLUGIN") {
		t.Error("output missing PLUGIN header")
	}
	if !strings.Contains(output, "APPLICABLE") {
		t.Error("output missing APPLICABLE header")
	}
	if !strings.Contains(output, "REASON") {
		t.Error("output missing REASON header")
	}

	// Verify plugins are listed.
	if !strings.Contains(output, "test.applicable") {
		t.Error("output missing test.applicable plugin")
	}
	if !strings.Contains(output, "test.notapplicable") {
		t.Error("output missing test.notapplicable plugin")
	}

	// Verify summary.
	if !strings.Contains(output, "Summary:") {
		t.Error("output missing Summary")
	}
	if !strings.Contains(output, "Duration:") {
		t.Error("output missing Duration")
	}
}

func TestCollectCommand_DryRunJSONOutput(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "--json"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	var result CollectResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	if !result.DetectionOnly {
		t.Error("expected DetectionOnly to be true for dry run")
	}

	// Check plugins are included.
	if len(result.PluginsRun) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(result.PluginsRun))
	}

	// Find the applicable plugin.
	var found bool
	for _, p := range result.PluginsRun {
		if p.ID == "test.applicable" {
			found = true
			if !p.Applicable {
				t.Error("test.applicable should be applicable")
			}
			if p.Reason != "found test artifacts" {
				t.Errorf("unexpected reason: %s", p.Reason)
			}
		}
	}
	if !found {
		t.Error("test.applicable not found in results")
	}
}

func TestCollectCommand_PluginFilter(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "--plugin", "test.applicable"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Only the specified plugin should be in output.
	if !strings.Contains(output, "test.applicable") {
		t.Error("output missing test.applicable plugin")
	}
	if strings.Contains(output, "test.notapplicable") {
		t.Error("output should not contain test.notapplicable")
	}
	if strings.Contains(output, "other.plugin") {
		t.Error("output should not contain other.plugin")
	}
}

func TestCollectCommand_CategoryFilter(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "--category", "other"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Only the 'other' category plugin should be in output.
	if !strings.Contains(output, "other.plugin") {
		t.Error("output missing other.plugin")
	}
	// test category plugins should not be in output.
	if strings.Contains(output, "test.applicable") {
		t.Error("output should not contain test.applicable")
	}
}

func TestCollectCommand_PluginNotFound(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "--plugin", "nonexistent"})
	err := rootCmd.Execute()

	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}

	if !strings.Contains(err.Error(), "plugin not found") {
		t.Errorf("expected 'plugin not found' error, got: %v", err)
	}
}

func TestCollectCommand_NoPluginsMatchCategory(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "--category", "nonexistent"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No plugins matched") {
		t.Errorf("expected 'No plugins matched' message, got: %s", output)
	}
}

func TestCollectCommand_ActualCollection(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")

	// Clear and register a simple mock plugin that writes data.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	collectedRecords := 0
	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.collect",
			Name:        "Collect Test",
			Description: "Test plugin for collection",
			Version:     "1.0.0",
			Categories:  []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "always applicable",
		},
		collectCallback: func(_ context.Context, _ macfor.Source, out macfor.Collector) error {
			// Simulate collecting some records.
			out.Log(macfor.LogInfo, "Starting collection")
			collectedRecords++
			return nil
		},
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--output", outputPath})
	err = rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify output was created.
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("evidence container was not created")
	}

	// Verify collection callback was called.
	if collectedRecords != 1 {
		t.Errorf("expected 1 collection, got %d", collectedRecords)
	}

	output := buf.String()
	if !strings.Contains(output, "Collection Complete") {
		t.Error("output missing 'Collection Complete'")
	}
	if !strings.Contains(output, "Evidence container:") {
		t.Error("output missing 'Evidence container:'")
	}
}

func TestCollectCommand_JSONOutput(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")

	// Clear and register a simple mock plugin.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:          "test.collect",
			Name:        "Collect Test",
			Description: "Test plugin for collection",
			Version:     "1.0.0",
			Categories:  []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "always applicable",
		},
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--output", outputPath, "--json"})
	err = rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	var result CollectResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid JSON output: %v\noutput: %s", err, output)
	}

	if result.ContainerPath == "" {
		t.Error("expected ContainerPath in result")
	}
	if len(result.PluginsRun) == 0 {
		t.Error("expected PluginsRun in result")
	}
	if result.Duration == "" {
		t.Error("expected Duration in result")
	}
}

func TestCollectCommand_AddZipExtension(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Output path without .zip extension.
	outputPath := filepath.Join(tmpDir, "evidence")
	expectedPath := outputPath + ".zip"

	// Clear and register a simple mock plugin.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.collect",
			Name:       "Collect Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "always applicable",
		},
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--output", outputPath})
	err = rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify .zip was added.
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("expected %s to be created", expectedPath)
	}
}

func TestCollectCommand_NoApplicablePlugins(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	// Create temp directory for output.
	tmpDir, err := os.MkdirTemp("", "macfor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "evidence.zip")

	// Clear and register only non-applicable plugins.
	macfor.ClearRegistry()
	defer macfor.ClearRegistry()

	macfor.Register(&collectMockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.notapplicable",
			Name:       "Not Applicable",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: false,
			Reason:     "not applicable",
		},
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--output", outputPath})
	err = rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "No applicable plugins") {
		t.Errorf("expected 'No applicable plugins' message, got: %s", output)
	}

	// Container should not be created.
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Error("container should not be created when no plugins are applicable")
	}
}

func TestCollectCommand_Help(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--help"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	expectedStrings := []string{
		"Collect forensic artifacts",
		"--output",
		"--plugin",
		"--category",
		"--dry-run",
		"--json",
		"Examples:",
		"macfor collect --output",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(output, expected) {
			t.Errorf("help output missing expected string: %q", expected)
		}
	}
}

func TestResolvePlugins_AllPlugins(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins, err := resolvePlugins(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(plugins))
	}
}

func TestResolvePlugins_ByID(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins, err := resolvePlugins([]string{"test.applicable"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin, got %d", len(plugins))
	}

	if plugins[0].Info().ID != "test.applicable" {
		t.Errorf("expected test.applicable, got %s", plugins[0].Info().ID)
	}
}

func TestResolvePlugins_ByCategory(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins, err := resolvePlugins(nil, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(plugins))
	}

	for _, p := range plugins {
		found := false
		for _, cat := range p.Info().Categories {
			if cat == "test" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("plugin %s should be in test category", p.Info().ID)
		}
	}
}

func TestResolvePlugins_NotFound(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	_, err := resolvePlugins([]string{"nonexistent"}, "")
	if err == nil {
		t.Fatal("expected error for nonexistent plugin")
	}

	if !strings.Contains(err.Error(), "plugin not found") {
		t.Errorf("expected 'plugin not found' error, got: %v", err)
	}
}

func TestFilterApplicable(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins := macfor.All()

	detectionResults := map[string]macfor.DetectionResult{
		"test.applicable":    {Applicable: true},
		"test.notapplicable": {Applicable: false},
		"other.plugin":       {Applicable: true},
	}

	applicable := filterApplicable(plugins, detectionResults)

	if len(applicable) != 2 {
		t.Errorf("expected 2 applicable plugins, got %d", len(applicable))
	}

	for _, p := range applicable {
		if !detectionResults[p.Info().ID].Applicable {
			t.Errorf("plugin %s should not be in applicable list", p.Info().ID)
		}
	}
}

func TestBuildCollectResult(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins := macfor.All()
	startTime := time.Now().Add(-time.Second)

	detectionResults := map[string]macfor.DetectionResult{
		"test.applicable":    {Applicable: true, Reason: "found"},
		"test.notapplicable": {Applicable: false, Reason: "not found"},
		"other.plugin":       {Applicable: true, Reason: "other found"},
	}

	result := buildCollectResult(plugins, detectionResults, nil, "/path/to/evidence.zip", startTime)

	if result.ContainerPath != "/path/to/evidence.zip" {
		t.Errorf("expected ContainerPath '/path/to/evidence.zip', got %q", result.ContainerPath)
	}

	if len(result.PluginsRun) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(result.PluginsRun))
	}

	if result.DurationMs <= 0 {
		t.Error("expected positive duration")
	}
}

func TestCollectResult_JSONSerialization(t *testing.T) {
	result := CollectResult{
		Success:       true,
		ContainerPath: "/path/to/evidence.zip",
		PluginsRun: []PluginRunResult{
			{
				ID:             "test.plugin",
				Name:           "Test Plugin",
				Applicable:     true,
				FilesCollected: 5,
				RecordsCreated: 100,
				Errors:         1,
			},
		},
		TotalFiles:   5,
		TotalRecords: 100,
		TotalErrors:  1,
		Duration:     "1.5s",
		DurationMs:   1500,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled CollectResult
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.Success != result.Success {
		t.Error("Success mismatch")
	}
	if unmarshaled.ContainerPath != result.ContainerPath {
		t.Error("ContainerPath mismatch")
	}
	if len(unmarshaled.PluginsRun) != 1 {
		t.Error("PluginsRun length mismatch")
	}
	if unmarshaled.TotalFiles != result.TotalFiles {
		t.Error("TotalFiles mismatch")
	}
}

// mockSource implements macfor.Source for testing.
type mockSource struct{}

func (m *mockSource) Type() macfor.SourceType                     { return macfor.SourceTypeLive }
func (m *mockSource) OSVersion() (string, error)                  { return "14.0", nil }
func (m *mockSource) Hostname() (string, error)                   { return "test-host", nil }
func (m *mockSource) Open(_ string) (io.ReadCloser, error)        { return nil, nil }
func (m *mockSource) Stat(_ string) (macfor.FileInfo, error)      { return nil, nil }
func (m *mockSource) ReadDir(_ string) ([]macfor.DirEntry, error) { return nil, nil }
func (m *mockSource) Glob(_ string) ([]string, error)             { return nil, nil }
func (m *mockSource) Walk(_ string, _ macfor.WalkFunc) error      { return nil }
func (m *mockSource) Users() ([]macfor.User, error)               { return nil, nil }
func (m *mockSource) ResolvePath(path string) string              { return path }
func (m *mockSource) ReadPlist(_ string, _ any) error             { return nil }
func (m *mockSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{OSName: "macOS", OSVersion: "14.0", Architecture: "arm64", Hostname: "test-host", SourceType: macfor.SourceTypeLive}, nil
}

func TestRunDetection(t *testing.T) {
	cleanup := registerCollectTestPlugins()
	defer cleanup()

	plugins := macfor.All()
	src := &mockSource{}

	resetTestState()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	results := runDetection(context.Background(), plugins, src, rootCmd)

	if len(results) != 3 {
		t.Errorf("expected 3 detection results, got %d", len(results))
	}

	if !results["test.applicable"].Applicable {
		t.Error("test.applicable should be applicable")
	}
	if results["test.notapplicable"].Applicable {
		t.Error("test.notapplicable should not be applicable")
	}
}

func TestRunCollect_FromNonExistentPath(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--from", "/nonexistent/path/that/does/not/exist", "--dry-run"})
	err := rootCmd.Execute()

	if err == nil {
		t.Fatal("expected error for nonexistent --from path")
	}

	if !strings.Contains(err.Error(), "path does not exist") {
		t.Errorf("expected 'path does not exist' error, got: %v", err)
	}
}

func TestRunCollect_FromFilePath(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	// Create a temp file (not a directory).
	tmpFile, err := os.CreateTemp("", "macfor-test-file-*")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--from", tmpPath, "--dry-run"})
	err = rootCmd.Execute()

	if err == nil {
		t.Fatal("expected error for file path passed to --from")
	}

	if !strings.Contains(err.Error(), "path is not a directory") {
		t.Errorf("expected 'path is not a directory' error, got: %v", err)
	}
}

func TestRunCollect_FromValidDirectory_DryRun(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	// Create a temp directory with minimal macOS tree.
	tmpDir, err := os.MkdirTemp("", "macfor-test-from-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create minimal structure so directory source can initialise.
	_ = os.MkdirAll(filepath.Join(tmpDir, "Users"), 0o750)

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--from", tmpDir, "--dry-run"})
	err = rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Source: directory") {
		t.Errorf("expected 'Source: directory' in output, got: %s", output)
	}
}

func TestCollectCommand_HelpShowsFromFlag(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--help"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "--from") {
		t.Error("help output missing --from flag")
	}
	if !strings.Contains(output, "mounted disk image") {
		t.Error("help output missing disk image example")
	}
}

func TestCollectCommand_MultiplePluginFlags(t *testing.T) {
	resetTestState()
	resetCollectFlags()

	cleanup := registerCollectTestPlugins()
	defer cleanup()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)

	rootCmd.SetArgs([]string{"collect", "--dry-run", "-p", "test.applicable", "-p", "other.plugin"})
	err := rootCmd.Execute()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// Both specified plugins should be in output.
	if !strings.Contains(output, "test.applicable") {
		t.Error("output missing test.applicable plugin")
	}
	if !strings.Contains(output, "other.plugin") {
		t.Error("output missing other.plugin")
	}
	// Unspecified plugin should not be in output.
	if strings.Contains(output, "test.notapplicable") {
		t.Error("output should not contain test.notapplicable")
	}
}
