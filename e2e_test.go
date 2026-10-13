package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/internal/orchestrator"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/sources/live"
	"github.com/macfor-io/macfor-community/testdata/macostree"

	// Import shell plugin to ensure it's registered.
	_ "github.com/macfor-io/macfor-community/plugins/shell"
)

const darwinOS = "darwin"

// End-to-end integration tests that verify the complete macfor workflow.
// These tests run the entire collection pipeline from live source to evidence container.

func TestE2E_FullCollectionWorkflow(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "e2e-evidence.zip")

	// Phase 1: Create source
	t.Log("Phase 1: Creating source from a synthetic macOS tree")
	src := fixtureSource(t)

	// Verify source provides system information
	osVersion, err := src.OSVersion()
	if err != nil {
		t.Fatalf("failed to get OS version: %v", err)
	}
	t.Logf("  OS Version: %s", osVersion)

	hostname, err := src.Hostname()
	if err != nil {
		t.Fatalf("failed to get hostname: %v", err)
	}
	t.Logf("  Hostname: %s", hostname)

	// Phase 2: Create container
	t.Log("Phase 2: Creating evidence container")
	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Phase 3: Configure orchestrator
	t.Log("Phase 3: Configuring orchestrator")
	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// Track progress
	var phases []orchestrator.ProgressPhase
	orch.SetProgressCallback(func(p orchestrator.Progress) {
		phases = append(phases, p.Phase)
		t.Logf("  Progress: %s - %s", p.Phase, p.Message)
	})

	// Phase 4: Run orchestration
	t.Log("Phase 4: Running orchestration")
	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestration failed: %v", err)
	}

	t.Logf("  Result: success=%v, files=%d, records=%d, errors=%d",
		result.Success, result.TotalFiles, result.TotalRecords, result.TotalErrors)
	t.Logf("  Duration: %s", result.Duration)
	t.Logf("  Applicable plugins: %d", result.ApplicablePlugins())
	t.Logf("  Collected plugins: %d", result.CollectedPlugins())

	if result.CollectedPlugins() == 0 {
		t.Fatal("expected shell.history to collect from the fixture tree")
	}

	// Phase 5: Close container
	t.Log("Phase 5: Closing container")
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Phase 6: Verify container contents
	t.Log("Phase 6: Verifying container contents")
	verifyE2EContainer(t, outputPath, result)

	// Verify all expected phases occurred
	expectedPhases := []orchestrator.ProgressPhase{
		orchestrator.PhaseInit,
		orchestrator.PhaseDetection,
	}

	for _, expected := range expectedPhases {
		found := false
		for _, phase := range phases {
			if phase == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected phase: %s", expected)
		}
	}

	// If collection occurred, verify collection and finalize phases
	if result.CollectedPlugins() > 0 {
		for _, expected := range []orchestrator.ProgressPhase{
			orchestrator.PhaseCollection,
			orchestrator.PhaseFinalize,
		} {
			found := false
			for _, phase := range phases {
				if phase == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing expected phase: %s", expected)
			}
		}
	}

	t.Log("End-to-end test completed successfully")
}

func TestE2E_DryRunWorkflow(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	// Create live source
	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	// Configure orchestrator for dry run
	config := orchestrator.Config{
		Source:      src,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
		DryRun:      true,
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// Run dry run
	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	// Verify dry run behavior
	if !result.DetectionOnly {
		t.Error("expected DetectionOnly to be true for dry run")
	}

	if !result.Success {
		t.Error("expected Success to be true for dry run")
	}

	if result.CollectedPlugins() != 0 {
		t.Error("expected 0 collected plugins for dry run")
	}

	// Should have detection results for shell.history
	found := false
	for _, pr := range result.PluginResults {
		if pr.ID == "shell.history" {
			found = true
			t.Logf("shell.history: applicable=%v, reason=%s", pr.Applicable, pr.Reason)
			break
		}
	}

	if !found {
		t.Error("shell.history not found in results")
	}
}

func TestE2E_MultiPluginCollection(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "multi-plugin.zip")

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Run with all registered plugins
	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestration failed: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Log results for all plugins
	t.Logf("Ran %d plugins:", len(result.PluginResults))
	for _, pr := range result.PluginResults {
		t.Logf("  %s: applicable=%v, collected=%v, files=%d, records=%d",
			pr.ID, pr.Applicable, pr.Collected, pr.FilesCollected, pr.RecordsCreated)
	}
}

func TestE2E_HashIntegrity(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "hash-integrity.zip")

	src := fixtureSource(t)

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestration failed: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	if result.TotalFiles == 0 {
		t.Fatal("expected files from the fixture tree")
	}

	// Verify hash integrity
	verifyHashIntegrity(t, outputPath)
}

func TestE2E_ChainOfCustody(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "custody.zip")

	src := fixtureSource(t)

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	_, err = orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestration failed: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Verify chain of custody
	verifyCustodyLog(t, outputPath)
}

func TestE2E_ManifestContents(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "manifest.zip")

	src := fixtureSource(t)

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestration failed: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Verify manifest contents
	verifyManifest(t, outputPath, result)
}

func TestE2E_RecoveryFromCancellation(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("end-to-end test only runs on macOS")
	}

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "cancelled.zip")

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	config := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "1.0.0-e2e-test",
	}

	orch, err := orchestrator.New(config)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := orch.Run(ctx)

	// Should return cancelled error
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	if result != nil && !result.Cancelled {
		t.Error("expected result.Cancelled to be true")
	}

	// Container should be marked with error
	cont.MarkError()
	if err := cont.Close(); err != nil {
		t.Logf("close error (expected): %v", err)
	}
}

// Helper functions for verification

func verifyE2EContainer(t *testing.T, containerPath string, result *orchestrator.Result) {
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

	// Verify required metadata files
	requiredFiles := []string{"manifest.json", "chain-of-custody.json", "hashes.json"}
	for _, name := range requiredFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("missing required file: %s", name)
		}
	}

	// Verify manifest
	if f, ok := files["manifest.json"]; ok {
		data, err := readZipFile(f)
		if err != nil {
			t.Errorf("failed to read manifest.json: %v", err)
		} else {
			var manifest container.Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Errorf("failed to parse manifest.json: %v", err)
			} else if manifest.ToolVersion != "1.0.0-e2e-test" {
				t.Errorf("expected tool version '1.0.0-e2e-test', got '%s'", manifest.ToolVersion)
			}
		}
	}

	// Count artifacts if collection occurred
	if result.CollectedPlugins() > 0 {
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

		t.Logf("Container contents: %d total files, %d artifacts, %d record files, %d log files",
			len(files), artifactCount, recordCount, logCount)
	}
}

func verifyHashIntegrity(t *testing.T, containerPath string) {
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

	// Read hash manifest
	hashFile, ok := files["hashes.json"]
	if !ok {
		t.Fatal("hashes.json not found")
	}

	hashData, err := readZipFile(hashFile)
	if err != nil {
		t.Fatalf("failed to read hashes.json: %v", err)
	}

	var hashManifest container.HashManifest
	if err := json.Unmarshal(hashData, &hashManifest); err != nil {
		t.Fatalf("failed to parse hashes.json: %v", err)
	}

	// Verify each file's hash
	verifiedCount := 0
	for name, expectedHash := range hashManifest.Files {
		f, ok := files[name]
		if !ok {
			t.Errorf("file in hash manifest but not in container: %s", name)
			continue
		}

		data, err := readZipFile(f)
		if err != nil {
			t.Errorf("failed to read %s: %v", name, err)
			continue
		}

		actualHash := sha256Hash(data)
		if actualHash != expectedHash {
			t.Errorf("hash mismatch for %s: expected %s, got %s", name, expectedHash, actualHash)
		} else {
			verifiedCount++
		}
	}

	t.Logf("Verified %d file hashes", verifiedCount)
}

func verifyCustodyLog(t *testing.T, containerPath string) {
	t.Helper()

	zr, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	var custodyFile *zip.File
	for _, f := range zr.File {
		if f.Name == "chain-of-custody.json" {
			custodyFile = f
			break
		}
	}

	if custodyFile == nil {
		t.Fatal("chain-of-custody.json not found")
	}

	data, err := readZipFile(custodyFile)
	if err != nil {
		t.Fatalf("failed to read chain-of-custody.json: %v", err)
	}

	var custody container.ChainOfCustodyLog
	if err := json.Unmarshal(data, &custody); err != nil {
		t.Fatalf("failed to parse chain-of-custody.json: %v", err)
	}

	// Verify required entries
	hasStarted := false
	hasCompleted := false

	for _, entry := range custody.Entries {
		if entry.Action == container.CustodyActionCollectionStarted {
			hasStarted = true
		}
		if entry.Action == container.CustodyActionCollectionCompleted {
			hasCompleted = true
		}
	}

	if !hasStarted {
		t.Error("missing collection started entry")
	}
	if !hasCompleted {
		t.Error("missing collection completed entry")
	}

	t.Logf("Chain of custody: %d entries", len(custody.Entries))
}

func verifyManifest(t *testing.T, containerPath string, result *orchestrator.Result) {
	t.Helper()

	zr, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}

	if manifestFile == nil {
		t.Fatal("manifest.json not found")
	}

	data, err := readZipFile(manifestFile)
	if err != nil {
		t.Fatalf("failed to read manifest.json: %v", err)
	}

	var manifest container.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("failed to parse manifest.json: %v", err)
	}

	// Verify manifest contents
	if manifest.Version == "" {
		t.Error("manifest version is empty")
	}
	if manifest.CollectionID == "" {
		t.Error("manifest collection ID is empty")
	}
	if manifest.ToolVersion != "1.0.0-e2e-test" {
		t.Errorf("expected tool version '1.0.0-e2e-test', got '%s'", manifest.ToolVersion)
	}

	// Verify statistics match result
	if manifest.Statistics.TotalFiles != result.TotalFiles {
		t.Errorf("manifest files %d != result files %d",
			manifest.Statistics.TotalFiles, result.TotalFiles)
	}
	if manifest.Statistics.TotalRecords != result.TotalRecords {
		t.Errorf("manifest records %d != result records %d",
			manifest.Statistics.TotalRecords, result.TotalRecords)
	}

	t.Logf("Manifest: version=%s, plugins=%d, files=%d, records=%d",
		manifest.Version, len(manifest.Plugins),
		manifest.Statistics.TotalFiles, manifest.Statistics.TotalRecords)
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

// sha256Hash computes the SHA-256 hash of data.
func sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// fixtureSource returns a directory source over a synthetic macOS tree whose
// user has zsh history, so collection tests produce a full evidence
// container on any machine, including CI runners without shell history.
func fixtureSource(t *testing.T) macfor.Source {
	t.Helper()
	opts := macostree.Defaults()
	opts.Users[0].ZshHistory = ": 1700000000:0;echo hello\n: 1700000001:0;ls -la\n: 1700000002:0;cd /tmp\n"
	src, err := directory.New(macostree.MustBuild(t, opts))
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}
	return src
}
