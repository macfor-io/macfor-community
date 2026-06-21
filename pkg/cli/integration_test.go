package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/plugins/shell"
)

const darwinOS = "darwin"

// shellPlugin holds a reference to the shell plugin for registration in tests.
var shellPlugin = &shell.Plugin{}

// setupShellPlugin ensures the shell plugin is registered and returns a cleanup function.
func setupShellPlugin(t *testing.T) {
	t.Helper()
	// Clear and re-register to ensure clean state
	macfor.ClearRegistry()
	macfor.Register(shellPlugin)
}

// Integration tests that verify the CLI works with real components.
// These tests use the real shell plugin and live source.

func TestIntegration_CLIDryRunWithRealPlugin(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	t.Run("dry run shows shell.history plugin", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect", "--dry-run", "-p", "shell.history"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		// Should show shell.history plugin
		if !strings.Contains(output, "shell.history") {
			t.Error("output missing shell.history")
		}

		// Should show detection results
		if !strings.Contains(output, "Dry Run") || !strings.Contains(output, "Detection Results") {
			t.Error("output missing dry run header")
		}

		// Should show applicable status
		if !strings.Contains(output, "Yes") && !strings.Contains(output, "No") {
			t.Error("output missing applicable status")
		}

		t.Logf("Dry run output:\n%s", output)
	})

	t.Run("dry run JSON output with real plugin", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect", "--dry-run", "--json", "-p", "shell.history"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		var result CollectResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("invalid JSON output: %v", err)
		}

		if !result.DetectionOnly {
			t.Error("expected DetectionOnly to be true")
		}

		// Find shell.history in results
		found := false
		for _, pr := range result.PluginsRun {
			if pr.ID == "shell.history" {
				found = true
				t.Logf("shell.history: applicable=%v, reason=%s", pr.Applicable, pr.Reason)
				break
			}
		}

		if !found {
			t.Error("shell.history not found in results")
		}
	})
}

func TestIntegration_CLICollectWithRealPlugin(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	tmpDir := t.TempDir()

	t.Run("full collection with shell plugin", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		outputPath := filepath.Join(tmpDir, "evidence.zip")

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect", "-o", outputPath, "-p", "shell.history"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		// Check output format
		if !strings.Contains(output, "Collection Complete") && !strings.Contains(output, "No applicable plugins") {
			t.Error("output missing expected completion message")
		}

		// Check if container was created (only if plugin was applicable)
		if _, err := os.Stat(outputPath); err == nil {
			// Container was created - verify its contents
			verifyContainerContents(t, outputPath)
		} else if !os.IsNotExist(err) {
			t.Errorf("unexpected error checking container: %v", err)
		} else {
			t.Log("Container not created (shell.history may not be applicable)")
		}

		t.Logf("Collection output:\n%s", output)
	})

	t.Run("collection with JSON output", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		outputPath := filepath.Join(tmpDir, "evidence-json.zip")

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect", "-o", outputPath, "-p", "shell.history", "--json"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		var result CollectResult
		if err := json.Unmarshal([]byte(output), &result); err != nil {
			t.Fatalf("invalid JSON output: %v", err)
		}

		if result.Duration == "" {
			t.Error("expected duration in result")
		}

		t.Logf("JSON result: success=%v, files=%d, records=%d, errors=%d",
			result.Success, result.TotalFiles, result.TotalRecords, result.TotalErrors)
	})
}

func TestIntegration_CLIListPlugins(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	t.Run("list plugins shows shell.history", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"list", "plugins"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		if !strings.Contains(output, "shell.history") {
			t.Error("output missing shell.history plugin")
		}
		if !strings.Contains(output, "Shell History") {
			t.Error("output missing Shell History name")
		}

		t.Logf("List plugins output:\n%s", output)
	})

	t.Run("list plugins by category", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"list", "plugins", "-c", "shell"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		if !strings.Contains(output, "shell.history") {
			t.Error("output missing shell.history for shell category")
		}
	})

	t.Run("list plugins JSON output", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

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

		// Find shell.history
		found := false
		for _, p := range plugins {
			if p.ID == "shell.history" {
				found = true
				if p.Name != "Shell History" {
					t.Errorf("expected name 'Shell History', got '%s'", p.Name)
				}
				break
			}
		}

		if !found {
			t.Error("shell.history not found in JSON output")
		}
	})
}

func TestIntegration_CLIVersionAndHelp(t *testing.T) {
	t.Run("version flag", func(t *testing.T) {
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"--version"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		// Version output is "macfor dev (commit: ..., built: ...)"
		if !strings.Contains(output, "macfor") {
			t.Error("version output missing 'macfor'")
		}
		t.Logf("Version output: %s", output)
	})

	t.Run("help flag", func(t *testing.T) {
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"--help"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()

		expectedStrings := []string{
			"macfor collects forensic artifacts",
			"collect",
			"list",
			"--verbose",
			"--quiet",
		}

		for _, expected := range expectedStrings {
			if !strings.Contains(output, expected) {
				t.Errorf("help output missing: %s", expected)
			}
		}
	})

	t.Run("collect help", func(t *testing.T) {
		resetTestState()

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
			"evidence container",
		}

		for _, expected := range expectedStrings {
			if !strings.Contains(output, expected) {
				t.Errorf("collect help missing: %s", expected)
			}
		}
	})
}

func TestIntegration_CLIVerboseMode(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	t.Run("verbose flag enables debug output", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		// Verbose mode should provide more detailed output
		rootCmd.SetArgs([]string{"-v", "collect", "--dry-run", "-p", "shell.history"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Just verify it runs without error in verbose mode
		output := buf.String()
		t.Logf("Verbose output length: %d bytes", len(output))
	})
}

func TestIntegration_CLIQuietMode(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	t.Run("quiet flag reduces output", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"-q", "collect", "--dry-run", "-p", "shell.history"})
		err := rootCmd.Execute()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Quiet mode should still produce some output (the result table)
		output := buf.String()
		t.Logf("Quiet output length: %d bytes", len(output))
	})
}

func TestIntegration_CLIErrorHandling(t *testing.T) {
	t.Run("nonexistent plugin", func(t *testing.T) {
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect", "--dry-run", "-p", "nonexistent.plugin"})
		err := rootCmd.Execute()

		if err == nil {
			t.Fatal("expected error for nonexistent plugin")
		}

		if !strings.Contains(err.Error(), "plugin not found") {
			t.Errorf("expected 'plugin not found' error, got: %v", err)
		}
	})

	t.Run("missing output path", func(t *testing.T) {
		setupShellPlugin(t)
		resetTestState()
		resetCollectFlags()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"collect"})
		err := rootCmd.Execute()

		if err == nil {
			t.Fatal("expected error for missing output")
		}

		if !strings.Contains(err.Error(), "--output is required") {
			t.Errorf("expected '--output is required' error, got: %v", err)
		}
	})

	t.Run("invalid command", func(t *testing.T) {
		resetTestState()

		var buf bytes.Buffer
		rootCmd.SetOut(&buf)
		rootCmd.SetErr(&buf)

		rootCmd.SetArgs([]string{"invalidcommand"})
		err := rootCmd.Execute()

		if err == nil {
			t.Fatal("expected error for invalid command")
		}
	})
}

// verifyContainerContents checks that a container ZIP has the expected structure.
func verifyContainerContents(t *testing.T, containerPath string) {
	t.Helper()

	zr, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	// Check required metadata files
	requiredFiles := []string{
		"manifest.json",
		"chain-of-custody.json",
		"hashes.json",
	}

	for _, name := range requiredFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("container missing required file: %s", name)
		}
	}

	// Verify manifest.json
	if f, ok := files["manifest.json"]; ok {
		data, err := readZipFileHelper(f)
		if err != nil {
			t.Errorf("failed to read manifest.json: %v", err)
		} else {
			var manifest container.Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Errorf("failed to parse manifest.json: %v", err)
			} else {
				if manifest.CollectionID == "" {
					t.Error("manifest missing collection ID")
				}
				t.Logf("Manifest: version=%s, plugins=%d", manifest.Version, len(manifest.Plugins))
			}
		}
	}

	// Count artifacts and records
	artifactCount := 0
	recordCount := 0
	logCount := 0

	for name := range files {
		if strings.HasPrefix(name, "artifacts/") {
			artifactCount++
		}
		if strings.HasPrefix(name, "records/") {
			recordCount++
		}
		if strings.HasPrefix(name, "logs/") {
			logCount++
		}
	}

	t.Logf("Container contents: %d artifacts, %d record files, %d log files",
		artifactCount, recordCount, logCount)
}

// readZipFileHelper reads the contents of a file in a ZIP archive.
func readZipFileHelper(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}
