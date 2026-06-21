package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
)

func TestManifestValidateCommand(t *testing.T) {
	// Create a temporary directory for test manifests.
	tmpDir := t.TempDir()

	// Create a valid manifest.
	validManifest := `schema_version: "1.0"
plugin:
  id: test.plugin
  name: Test Plugin
  description: A test plugin
  version: "1.0.0"
  categories:
    - test
  platforms:
    - os: darwin
      min_version: "10.12"
artifacts:
  - id: test_artifact
    name: Test Artifact
    category: history
    format: text
    paths:
      - pattern: "~/.test_history"
        type: file
`
	validPath := filepath.Join(tmpDir, "valid.yaml")
	if err := os.WriteFile(validPath, []byte(validManifest), 0o600); err != nil {
		t.Fatalf("Failed to write valid manifest: %v", err)
	}

	// Create an invalid manifest (missing required fields).
	invalidManifest := `schema_version: "1.0"
plugin:
  id: test.plugin
  name: Test Plugin
artifacts: []
`
	invalidPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte(invalidManifest), 0o600); err != nil {
		t.Fatalf("Failed to write invalid manifest: %v", err)
	}

	tests := []struct {
		name           string
		args           []string
		wantErr        bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:    "validate valid manifest",
			args:    []string{"manifest", "validate", validPath},
			wantErr: false,
			wantContains: []string{
				"✓",
				"valid.yaml",
				"valid",
			},
		},
		{
			name:    "validate invalid manifest",
			args:    []string{"manifest", "validate", invalidPath},
			wantErr: true,
			wantContains: []string{
				"✗",
				"invalid.yaml",
			},
		},
		{
			name:    "validate non-existent file",
			args:    []string{"manifest", "validate", filepath.Join(tmpDir, "nonexistent.yaml")},
			wantErr: true,
			wantContains: []string{
				"no matching files",
			},
		},
		{
			name:    "validate with glob pattern",
			args:    []string{"manifest", "validate", filepath.Join(tmpDir, "*.yaml")},
			wantErr: true, // Invalid manifest will cause error.
			wantContains: []string{
				"valid.yaml",
				"invalid.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetFlags()
			ReinitRootCmd()

			var buf bytes.Buffer
			SetOutput(&buf)

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			output := buf.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("Output should contain %q, got: %s", want, output)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(output, notWant) {
					t.Errorf("Output should not contain %q, got: %s", notWant, output)
				}
			}
		})
	}
}

func TestManifestValidateJSON(t *testing.T) {
	// Create a temporary directory for test manifests.
	tmpDir := t.TempDir()

	// Create a valid manifest.
	validManifest := `schema_version: "1.0"
plugin:
  id: test.plugin
  name: Test Plugin
  description: A test plugin
  version: "1.0.0"
  categories:
    - test
  platforms:
    - os: darwin
      min_version: "10.12"
artifacts:
  - id: test_artifact
    name: Test Artifact
    category: history
    format: text
    paths:
      - pattern: "~/.test_history"
        type: file
`
	validPath := filepath.Join(tmpDir, "valid.yaml")
	if err := os.WriteFile(validPath, []byte(validManifest), 0o600); err != nil {
		t.Fatalf("Failed to write valid manifest: %v", err)
	}

	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "validate", validPath, "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var results []ValidateResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	if !results[0].Valid {
		t.Errorf("Expected valid=true, got false")
	}

	if results[0].PluginID != "test.plugin" {
		t.Errorf("Expected plugin_id=test.plugin, got %s", results[0].PluginID)
	}
}

func TestManifestValidateNoArgs(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	// Running without args should validate registered plugins.
	rootCmd.SetArgs([]string{"manifest", "validate"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Should show registered plugins or "No plugins registered".
	if !strings.Contains(output, "valid") && !strings.Contains(output, "No plugins registered") {
		t.Errorf("Unexpected output: %s", output)
	}
}

func TestManifestPathsCommand(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	// Running paths without specific plugin should list all.
	rootCmd.SetArgs([]string{"manifest", "paths"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Should contain plugin info or "No plugins registered".
	if output == "" {
		t.Errorf("Expected some output, got empty string")
	}
}

func TestManifestPathsWithPlugin(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	// Try to get paths for shell plugin.
	rootCmd.SetArgs([]string{"manifest", "paths", "--plugin", "shell.history"})
	err := rootCmd.Execute()

	// This may fail if shell.history is not registered.
	if err != nil {
		if !strings.Contains(err.Error(), "plugin not found") {
			t.Fatalf("Unexpected error: %v", err)
		}
		return
	}

	output := buf.String()
	if !strings.Contains(output, "shell.history") {
		t.Errorf("Expected output to contain shell.history, got: %s", output)
	}
}

func TestManifestPathsJSON(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "paths", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var results []PathsResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Results should be valid JSON (array, possibly empty).
	if results == nil {
		t.Errorf("Expected array result, got nil")
	}
}

func TestManifestPathsWithOSVersion(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "paths", "--os-version", "10.15", "--json"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var results []PathsResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Each result should have the specified OS version.
	for _, r := range results {
		if r.OSVersion != "10.15" {
			t.Errorf("Expected os_version=10.15, got %s", r.OSVersion)
		}
	}
}

func TestManifestPathsInvalidPlugin(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "paths", "--plugin", "nonexistent.plugin"})
	err := rootCmd.Execute()

	if err == nil {
		t.Errorf("Expected error for nonexistent plugin")
	}

	if !strings.Contains(err.Error(), "plugin not found") {
		t.Errorf("Expected 'plugin not found' error, got: %v", err)
	}
}

func TestManifestHelpCommand(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "--help"})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "validate") {
		t.Errorf("Help should mention validate subcommand, got: %s", output)
	}
	if !strings.Contains(output, "paths") {
		t.Errorf("Help should mention paths subcommand, got: %s", output)
	}
}

func TestGetCurrentOSVersion(t *testing.T) {
	version := getCurrentOSVersion()

	// On macOS, should return a valid version string.
	// On other platforms, should return "unknown".
	if version == "" {
		t.Errorf("getCurrentOSVersion() returned empty string")
	}

	// If it's not unknown, it should look like a version number.
	if version != unknownVersion {
		parts := strings.Split(version, ".")
		if len(parts) < 2 {
			t.Errorf("Expected version with at least 2 parts, got: %s", version)
		}
	}
}

func TestFormatPathWithType(t *testing.T) {
	tests := []struct {
		path     string
		pathType string
		want     string
	}{
		{"~/.zsh_history", "file", "~/.zsh_history"},
		{"~/.zsh_history", "", "~/.zsh_history"},
		{"~/Library/Safari/LocalStorage/*.localstorage", "glob", "~/Library/Safari/LocalStorage/*.localstorage (glob)"},
		{"~/Library/Safari/Extensions/", "directory", "~/Library/Safari/Extensions/ (directory)"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := formatPathWithType(tt.path, tt.pathType)
			if got != tt.want {
				t.Errorf("formatPathWithType(%q, %q) = %q, want %q", tt.path, tt.pathType, got, tt.want)
			}
		})
	}
}

func TestManifestDocsCommand(t *testing.T) {
	tmpDir := t.TempDir()

	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	// Run docs command with custom output directory.
	rootCmd.SetArgs([]string{"manifest", "docs", "--output", tmpDir})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := buf.String()
	// Should show generated files or no plugins message.
	if !strings.Contains(output, "Generated") && !strings.Contains(output, "No plugins registered") {
		t.Errorf("Unexpected output: %s", output)
	}
}

func TestManifestDocsInvalidPlugin(t *testing.T) {
	tmpDir := t.TempDir()

	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "docs", "--output", tmpDir, "--plugin", "nonexistent.plugin"})
	err := rootCmd.Execute()

	if err == nil {
		t.Errorf("Expected error for nonexistent plugin")
	}

	if !strings.Contains(err.Error(), "plugin not found") {
		t.Errorf("Expected 'plugin not found' error, got: %v", err)
	}
}

func TestManifestDocsHelp(t *testing.T) {
	ResetFlags()
	ReinitRootCmd()

	var buf bytes.Buffer
	SetOutput(&buf)

	rootCmd.SetArgs([]string{"manifest", "docs", "--help"})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "documentation") {
		t.Errorf("Help should mention documentation, got: %s", output)
	}
	if !strings.Contains(output, "--output") {
		t.Errorf("Help should mention --output flag, got: %s", output)
	}
}

func TestGeneratePluginDoc(t *testing.T) {
	manifest := &artifacts.Manifest{
		SchemaVersion: "1.0",
		Plugin: artifacts.Plugin{
			ID:          "test.plugin",
			Name:        "Test Plugin",
			Description: "A test plugin for unit tests",
			Version:     "1.0.0",
			Categories:  []string{"test", "example"},
			Platforms: []artifacts.Platform{
				{OS: "darwin", MinVersion: "10.12", MaxVersion: ""},
			},
		},
		Artifacts: []artifacts.Artifact{
			{
				ID:       "test_artifact",
				Name:     "Test Artifact",
				Category: "history",
				Format:   "text",
				Optional: true,
				Paths: []artifacts.PathSpec{
					{Pattern: "~/.test_history", Type: "file"},
					{Pattern: "~/Library/Test/*.db", Type: "glob", OSMin: "11.0"},
				},
				Related:  []string{"~/.test_history.bak"},
				Metadata: map[string]string{"key": "value"},
			},
		},
	}

	doc := generatePluginDoc(manifest)

	// Check header.
	if !strings.Contains(doc, "# Test Plugin") {
		t.Errorf("Doc should contain plugin name header")
	}
	if !strings.Contains(doc, "`test.plugin`") {
		t.Errorf("Doc should contain plugin ID")
	}

	// Check description.
	if !strings.Contains(doc, "A test plugin for unit tests") {
		t.Errorf("Doc should contain description")
	}

	// Check platform support.
	if !strings.Contains(doc, "darwin") {
		t.Errorf("Doc should contain platform OS")
	}
	if !strings.Contains(doc, "10.12") {
		t.Errorf("Doc should contain min version")
	}

	// Check artifacts table.
	if !strings.Contains(doc, "`test_artifact`") {
		t.Errorf("Doc should contain artifact ID")
	}

	// Check paths.
	if !strings.Contains(doc, "`~/.test_history`") {
		t.Errorf("Doc should contain path pattern")
	}
	if !strings.Contains(doc, "macOS >= 11.0") {
		t.Errorf("Doc should contain version constraint")
	}

	// Check related files.
	if !strings.Contains(doc, "`~/.test_history.bak`") {
		t.Errorf("Doc should contain related file")
	}

	// Check metadata.
	if !strings.Contains(doc, "`key`") {
		t.Errorf("Doc should contain metadata key")
	}

	// Check footer.
	if !strings.Contains(doc, "auto-generated") {
		t.Errorf("Doc should contain auto-generated footer")
	}
}
