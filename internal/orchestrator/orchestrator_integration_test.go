package orchestrator

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/live"

	// Import shell plugin to ensure it's registered.
	_ "github.com/macfor-io/macfor-community/plugins/shell"
)

const (
	darwinOS        = "darwin"
	shellHistoryID  = "shell.history"
	shellCategory   = "shell"
	testToolVersion = "1.0.0-test"
)

// Integration tests that verify the orchestrator works with real components.
// These tests use the live source and real plugins.

func TestIntegration_OrchestratorWithLiveSource(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	t.Run("dry run detection", func(t *testing.T) {
		o, err := New(Config{
			Source: src,
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		result, err := o.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.DetectionOnly {
			t.Error("expected DetectionOnly to be true")
		}
		if !result.Success {
			t.Error("expected Success to be true")
		}

		// Should have at least the shellHistoryID plugin registered
		if len(result.PluginResults) == 0 {
			t.Error("expected at least one plugin result")
		}

		// Find the shellHistoryID plugin result
		var shellResult *PluginResult
		for i, pr := range result.PluginResults {
			if pr.ID == shellHistoryID {
				shellResult = &result.PluginResults[i]
				break
			}
		}

		if shellResult == nil {
			t.Fatal("shell.history plugin not found in results")
		}

		// On a real macOS system, shell.history should be applicable (unless no history files exist)
		t.Logf("shell.history applicable: %v, reason: %s", shellResult.Applicable, shellResult.Reason)
	})

	t.Run("dry run with plugin filter", func(t *testing.T) {
		o, err := New(Config{
			Source:    src,
			PluginIDs: []string{shellHistoryID},
			DryRun:    true,
		})
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		result, err := o.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.PluginResults) != 1 {
			t.Errorf("expected 1 plugin result, got %d", len(result.PluginResults))
		}
		if result.PluginResults[0].ID != shellHistoryID {
			t.Errorf("expected %s, got %s", shellHistoryID, result.PluginResults[0].ID)
		}
	})

	t.Run("dry run with category filter", func(t *testing.T) {
		o, err := New(Config{
			Source:   src,
			Category: "shell",
			DryRun:   true,
		})
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		result, err := o.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should find shell.history plugin
		found := false
		for _, pr := range result.PluginResults {
			if pr.ID == shellHistoryID {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected to find shell.history in shell category")
		}
	})
}

func TestIntegration_OrchestratorWithContainer(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	// Create temp directory for output
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "evidence.zip")

	// Create container
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	t.Run("full collection with shell plugin", func(t *testing.T) {
		o, err := New(Config{
			Source:      src,
			Container:   cont,
			PluginIDs:   []string{shellHistoryID},
			ToolVersion: testToolVersion,
		})
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		// Track progress
		var progressUpdates []Progress
		o.SetProgressCallback(func(p Progress) {
			progressUpdates = append(progressUpdates, p)
		})

		result, err := o.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Close container
		if err := cont.Close(); err != nil {
			t.Fatalf("failed to close container: %v", err)
		}

		// Verify result
		if result.DetectionOnly {
			t.Error("expected DetectionOnly to be false")
		}

		// Log collection statistics
		t.Logf("Collection result: files=%d, records=%d, errors=%d",
			result.TotalFiles, result.TotalRecords, result.TotalErrors)

		// Verify progress was tracked
		if len(progressUpdates) == 0 {
			t.Error("expected progress updates")
		}

		// Verify we saw all expected phases
		seenPhases := make(map[ProgressPhase]bool)
		for _, p := range progressUpdates {
			seenPhases[p.Phase] = true
		}

		expectedPhases := []ProgressPhase{PhaseInit, PhaseDetection}
		for _, phase := range expectedPhases {
			if !seenPhases[phase] {
				t.Errorf("missing expected phase: %s", phase)
			}
		}

		// Verify container file exists
		if _, err := os.Stat(outputPath); os.IsNotExist(err) {
			t.Error("container file was not created")
		}

		// Check if collection phase ran (only if plugin was applicable)
		for _, pr := range result.PluginResults {
			if pr.ID == shellHistoryID && pr.Applicable {
				if !seenPhases[PhaseCollection] {
					t.Error("missing collection phase for applicable plugin")
				}
				if !seenPhases[PhaseFinalize] {
					t.Error("missing finalize phase")
				}
				break
			}
		}
	})
}

//nolint:gocyclo // Integration test with many assertions and subtests.
func TestIntegration_ContainerContents(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "evidence.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	o, err := New(Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{shellHistoryID},
		ToolVersion: testToolVersion,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Open the ZIP to verify contents
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	// Build map of files in container
	files := make(map[string]*zip.File)
	for _, f := range zr.File {
		files[f.Name] = f
	}

	t.Run("manifest.json exists and is valid", func(t *testing.T) {
		f, ok := files["manifest.json"]
		if !ok {
			t.Fatal("manifest.json not found in container")
		}

		data, err := readZipFile(f)
		if err != nil {
			t.Fatalf("failed to read manifest.json: %v", err)
		}

		var manifest container.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to parse manifest.json: %v", err)
		}

		if manifest.Version == "" {
			t.Error("manifest version is empty")
		}
		if manifest.CollectionID == "" {
			t.Error("manifest collection ID is empty")
		}
		if manifest.ToolVersion != testToolVersion {
			t.Errorf("expected tool version '%s', got '%s'", testToolVersion, manifest.ToolVersion)
		}

		t.Logf("Manifest: version=%s, plugins=%d, files=%d, records=%d",
			manifest.Version, manifest.Statistics.PluginsExecuted,
			manifest.Statistics.TotalFiles, manifest.Statistics.TotalRecords)
	})

	t.Run("chain-of-custody.json exists and is valid", func(t *testing.T) {
		f, ok := files["chain-of-custody.json"]
		if !ok {
			t.Fatal("chain-of-custody.json not found in container")
		}

		data, err := readZipFile(f)
		if err != nil {
			t.Fatalf("failed to read chain-of-custody.json: %v", err)
		}

		var custody container.ChainOfCustodyLog
		if err := json.Unmarshal(data, &custody); err != nil {
			t.Fatalf("failed to parse chain-of-custody.json: %v", err)
		}

		if custody.CollectionID == "" {
			t.Error("custody log collection ID is empty")
		}
		if len(custody.Entries) == 0 {
			t.Error("custody log has no entries")
		}

		// Should have at least collection started and completed entries
		var hasStarted, hasCompleted bool
		for _, entry := range custody.Entries {
			if entry.Action == container.CustodyActionCollectionStarted {
				hasStarted = true
			}
			if entry.Action == container.CustodyActionCollectionCompleted {
				hasCompleted = true
			}
		}

		if !hasStarted {
			t.Error("missing collection started custody entry")
		}
		if !hasCompleted {
			t.Error("missing collection completed custody entry")
		}

		t.Logf("Chain of custody: %d entries", len(custody.Entries))
	})

	t.Run("hashes.json exists and is valid", func(t *testing.T) {
		f, ok := files["hashes.json"]
		if !ok {
			t.Fatal("hashes.json not found in container")
		}

		data, err := readZipFile(f)
		if err != nil {
			t.Fatalf("failed to read hashes.json: %v", err)
		}

		var hashes container.HashManifest
		if err := json.Unmarshal(data, &hashes); err != nil {
			t.Fatalf("failed to parse hashes.json: %v", err)
		}

		if hashes.Algorithm != "sha256" {
			t.Errorf("expected algorithm 'sha256', got '%s'", hashes.Algorithm)
		}

		// Should have hashes for all files added
		t.Logf("Hash manifest: %d files", len(hashes.Files))
	})

	t.Run("artifacts are collected if applicable", func(t *testing.T) {
		// Check if shell.history was applicable
		var shellApplicable bool
		for _, pr := range result.PluginResults {
			if pr.ID == shellHistoryID && pr.Applicable {
				shellApplicable = true
				break
			}
		}

		if !shellApplicable {
			t.Skip("shell.history not applicable on this system")
		}

		// Look for artifact files
		artifactCount := 0
		for name := range files {
			if strings.HasPrefix(name, "artifacts/") {
				artifactCount++
				t.Logf("Found artifact: %s", name)
			}
		}

		if artifactCount == 0 {
			t.Log("Warning: no artifacts found (user may have no shell history)")
		}
	})

	t.Run("records are created if applicable", func(t *testing.T) {
		// Look for record files
		recordCount := 0
		for name := range files {
			if strings.HasPrefix(name, "records/") {
				recordCount++
				t.Logf("Found records: %s", name)
			}
		}

		// Records are optional - only created if parsing succeeds
		t.Logf("Found %d record files", recordCount)
	})

	t.Run("logs are created", func(t *testing.T) {
		// Look for log files
		logCount := 0
		for name := range files {
			if strings.HasPrefix(name, "logs/") {
				logCount++
				t.Logf("Found log: %s", name)
			}
		}

		t.Logf("Found %d log files", logCount)
	})
}

func TestIntegration_OrchestratorCancellation(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	t.Run("cancellation during detection", func(t *testing.T) {
		o, err := New(Config{
			Source: src,
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("failed to create orchestrator: %v", err)
		}

		// Create an already-cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := o.Run(ctx)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got: %v", err)
		}
		if result != nil && !result.Cancelled {
			t.Error("expected Cancelled to be true")
		}
	})
}

func TestIntegration_ResultHelpers(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	o, err := New(Config{
		Source: src,
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("ApplicablePlugins helper", func(t *testing.T) {
		applicable := result.ApplicablePlugins()
		t.Logf("ApplicablePlugins: %d", applicable)

		// Count manually to verify
		manualCount := 0
		for _, pr := range result.PluginResults {
			if pr.Applicable {
				manualCount++
			}
		}

		if applicable != manualCount {
			t.Errorf("ApplicablePlugins() = %d, manual count = %d", applicable, manualCount)
		}
	})

	t.Run("CollectedPlugins helper", func(t *testing.T) {
		collected := result.CollectedPlugins()

		// In dry-run mode, no plugins should be collected
		if collected != 0 {
			t.Errorf("expected 0 collected plugins in dry-run mode, got %d", collected)
		}
	})
}

// readZipFile reads the contents of a file in a ZIP archive.
func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// TestIntegration_MultiplePluginExecution tests running multiple plugins together.
func TestIntegration_MultiplePluginExecution(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	// Clear registry and register mock plugins alongside shell
	originalPlugins := macfor.All()
	macfor.ClearRegistry()

	// Re-register original plugins
	for _, p := range originalPlugins {
		macfor.Register(p)
	}

	// Add a mock plugin that always succeeds
	macfor.Register(&mockPlugin{
		info: macfor.PluginInfo{
			ID:         "test.always.applicable",
			Name:       "Always Applicable Test",
			Version:    "1.0.0",
			Categories: []string{"test"},
		},
		detectResult: macfor.DetectionResult{
			Applicable: true,
			Reason:     "test plugin always applicable",
		},
	})

	defer macfor.ClearRegistry()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "evidence.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	o, err := New(Config{
		Source:      src,
		Container:   cont,
		ToolVersion: testToolVersion,
	})
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Should have run multiple plugins
	if len(result.PluginResults) < 2 {
		t.Errorf("expected at least 2 plugin results, got %d", len(result.PluginResults))
	}

	// Verify both plugins are in results
	foundShell := false
	foundTest := false
	for _, pr := range result.PluginResults {
		if pr.ID == shellHistoryID {
			foundShell = true
		}
		if pr.ID == "test.always.applicable" {
			foundTest = true
			if !pr.Applicable {
				t.Error("test plugin should be applicable")
			}
		}
	}

	if !foundShell {
		t.Error("shell.history not found in results")
	}
	if !foundTest {
		t.Error("test.always.applicable not found in results")
	}

	t.Logf("Ran %d plugins", len(result.PluginResults))
}
