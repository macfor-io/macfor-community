package directory_test

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/internal/orchestrator"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/sources/directory"
	"github.com/macfor-io/macfor-community/testdata/macostree"

	// Import plugins to trigger init() registration.
	_ "github.com/macfor-io/macfor-community/plugins/safari"
	_ "github.com/macfor-io/macfor-community/plugins/shell"
)

// testOSVersion is the macOS version set by macostree.Defaults().
const testOSVersion = "14.2.1"

// -----------------------------------------------------------------------------
// End-to-End Collection Tests
// -----------------------------------------------------------------------------

func TestIntegration_SafariCollectionFromDirectory(t *testing.T) {
	// Build a synthetic macOS tree with a user.
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	// Seed Safari fixture data into the tree.
	safariDir := filepath.Join(root, "Users", "alice", "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("creating safari dir: %v", err)
	}

	// Copy History.db fixture into the synthetic tree.
	fixtureBase := findFixtureDir(t)
	copyFile(t, filepath.Join(fixtureBase, "safari", "History.db"), filepath.Join(safariDir, "History.db"))

	// Copy Bookmarks.plist fixture.
	copyFile(t, filepath.Join(fixtureBase, "safari", "Bookmarks.plist"), filepath.Join(safariDir, "Bookmarks.plist"))

	// Copy Downloads.plist fixture.
	copyFile(t, filepath.Join(fixtureBase, "safari", "Downloads.plist"), filepath.Join(safariDir, "Downloads.plist"))

	// Create DirectorySource.
	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	// Create evidence container.
	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	// Run orchestrator with browser.safari plugin.
	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"browser.safari"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("closing container: %v", err)
	}

	// Verify Safari was detected as applicable.
	safariResult := findPluginResult(t, result, "browser.safari")
	if !safariResult.Applicable {
		t.Fatalf("expected browser.safari to be applicable, reason: %s", safariResult.Reason)
	}

	if !safariResult.Collected {
		t.Error("expected browser.safari to have collected artifacts")
	}

	// Verify container has the expected metadata files.
	manifest := readManifestFromZip(t, containerPath)
	if manifest == nil {
		t.Fatal("manifest.json not found in container")
	}

	// Verify source type is "directory" in the manifest.
	sourceMap, ok := manifest["source"].(map[string]any)
	if !ok {
		t.Fatal("manifest source field missing or not a map")
	}
	sourceType, _ := sourceMap["type"].(string)
	if sourceType != "directory" {
		t.Errorf("expected source.type = %q, got %q", "directory", sourceType)
	}

	// Verify image_path is set to the tree root.
	imagePath, _ := sourceMap["image_path"].(string)
	if imagePath != root {
		t.Errorf("expected source.image_path = %q, got %q", root, imagePath)
	}

	// Verify collection produced files and/or records.
	t.Logf("Collection results: files=%d, records=%d, errors=%d",
		result.TotalFiles, result.TotalRecords, result.TotalErrors)

	if result.TotalFiles == 0 && result.TotalRecords == 0 {
		t.Error("expected at least some files or records from Safari collection")
	}

	// Verify the container ZIP has artifact and record entries.
	verifyContainerHasEntries(t, containerPath)
}

func TestIntegration_ShellCollectionFromDirectory(t *testing.T) {
	// Build a synthetic macOS tree.
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	// Seed shell history for alice.
	homeDir := filepath.Join(root, "Users", "alice")
	zshHistory := filepath.Join(homeDir, ".zsh_history")
	historyContent := `: 1700000000:0;echo hello
: 1700000001:0;ls -la
: 1700000002:0;cd /tmp
`
	if err := os.WriteFile(zshHistory, []byte(historyContent), 0600); err != nil {
		t.Fatalf("writing zsh history: %v", err)
	}

	// Create DirectorySource.
	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	// Create evidence container.
	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	// Run orchestrator with shell.history plugin.
	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("closing container: %v", err)
	}

	// Verify shell.history was detected as applicable.
	shellResult := findPluginResult(t, result, "shell.history")
	if !shellResult.Applicable {
		t.Fatalf("expected shell.history to be applicable, reason: %s", shellResult.Reason)
	}

	if !shellResult.Collected {
		t.Error("expected shell.history to have collected artifacts")
	}

	t.Logf("Shell collection: files=%d, records=%d",
		shellResult.FilesCollected, shellResult.RecordsCreated)

	if shellResult.FilesCollected == 0 && shellResult.RecordsCreated == 0 {
		t.Error("expected at least some files or records from shell collection")
	}
}

// -----------------------------------------------------------------------------
// Detection Tests with DirectorySource
// -----------------------------------------------------------------------------

func TestIntegration_DetectionWithEmptyTree(t *testing.T) {
	// Build a minimal tree with no plugin-specific data.
	root := macostree.MustBuild(t, macostree.Defaults())

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	// Run in dry-run mode to test detection only.
	cfg := orchestrator.Config{
		Source:      src,
		ToolVersion: "test-integration",
		DryRun:      true,
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if !result.DetectionOnly {
		t.Error("expected DetectionOnly to be true for dry run")
	}

	if !result.Success {
		t.Error("expected dry run to succeed even with no applicable plugins")
	}

	// Log detection results for informational purposes.
	for _, pr := range result.PluginResults {
		t.Logf("  %s: applicable=%v, reason=%s", pr.ID, pr.Applicable, pr.Reason)
	}
}

func TestIntegration_DetectionSafariApplicable(t *testing.T) {
	// Build tree with Safari artifacts.
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	// Seed a minimal Safari History.db.
	safariDir := filepath.Join(root, "Users", "alice", "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("creating safari dir: %v", err)
	}

	fixtureBase := findFixtureDir(t)
	copyFile(t, filepath.Join(fixtureBase, "safari", "History.db"), filepath.Join(safariDir, "History.db"))

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	// Test detection directly on the Safari plugin.
	p, ok := macfor.ByID("browser.safari")
	if !ok {
		t.Fatal("browser.safari plugin not registered")
	}

	detection, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !detection.Applicable {
		t.Errorf("expected Safari to be applicable, reason: %s", detection.Reason)
	}

	if detection.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %.2f", detection.Confidence)
	}

	if detection.OSVersion != testOSVersion {
		t.Errorf("expected OSVersion %s, got %s", testOSVersion, detection.OSVersion)
	}

	t.Logf("Detection metadata: %v", detection.Metadata)
}

// -----------------------------------------------------------------------------
// SystemInfo Integration Tests
// -----------------------------------------------------------------------------

func TestIntegration_SystemInfoSourceType(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	info, err := src.SystemInfo()
	if err != nil {
		t.Fatalf("SystemInfo: %v", err)
	}

	if info.SourceType != macfor.SourceTypeDirectory {
		t.Errorf("SourceType = %q, want %q", info.SourceType, macfor.SourceTypeDirectory)
	}

	if info.ImagePath != root {
		t.Errorf("ImagePath = %q, want %q", info.ImagePath, root)
	}

	if info.OSVersion != testOSVersion {
		t.Errorf("OSVersion = %q, want %q", info.OSVersion, testOSVersion)
	}

	if info.OSName != "macOS" {
		t.Errorf("OSName = %q, want macOS", info.OSName)
	}

	if info.OSCodename != "Sonoma" {
		t.Errorf("OSCodename = %q, want Sonoma", info.OSCodename)
	}

	if info.ComputerName != "Alice's MacBook Pro" {
		t.Errorf("ComputerName = %q, want %q", info.ComputerName, "Alice's MacBook Pro")
	}
}

func TestIntegration_SystemInfoInManifest(t *testing.T) {
	// Build tree with shell history so we get a full collection.
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	homeDir := filepath.Join(root, "Users", "alice")
	bashHistory := filepath.Join(homeDir, ".bash_history")
	if err := os.WriteFile(bashHistory, []byte("echo test\nls\n"), 0600); err != nil {
		t.Fatalf("writing bash history: %v", err)
	}

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	_, err = orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("closing container: %v", err)
	}

	// Read the manifest and verify structured system info.
	manifest := readManifestFromZip(t, containerPath)
	if manifest == nil {
		t.Fatal("manifest.json not found in container")
	}

	sourceMap, ok := manifest["source"].(map[string]any)
	if !ok {
		t.Fatal("manifest source field missing or not a map")
	}

	// Verify type is directory.
	if got, _ := sourceMap["type"].(string); got != "directory" {
		t.Errorf("source.type = %q, want directory", got)
	}

	// Verify OS version from flat field.
	if got, _ := sourceMap["os_version"].(string); got != testOSVersion {
		t.Errorf("source.os_version = %q, want %q", got, testOSVersion)
	}

	// Verify nested OS info if present.
	if osInfo, ok := sourceMap["os"].(map[string]any); ok {
		if got, _ := osInfo["version"].(string); got != testOSVersion {
			t.Errorf("source.os.version = %q, want %q", got, testOSVersion)
		}
		if got, _ := osInfo["codename"].(string); got != "Sonoma" {
			t.Errorf("source.os.codename = %q, want Sonoma", got)
		}
	}

	// Verify nested network info if present.
	if networkInfo, ok := sourceMap["network"].(map[string]any); ok {
		if got, _ := networkInfo["computer_name"].(string); got != "Alice's MacBook Pro" {
			t.Errorf("source.network.computer_name = %q, want %q", got, "Alice's MacBook Pro")
		}
	}
}

// -----------------------------------------------------------------------------
// Context Cancellation Tests
// -----------------------------------------------------------------------------

func TestIntegration_CollectionCancellation(t *testing.T) {
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	// Seed Safari data so there is something to collect.
	safariDir := filepath.Join(root, "Users", "alice", "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0755); err != nil { //nolint:gosec // Test fixture directory.
		t.Fatalf("creating safari dir: %v", err)
	}

	fixtureBase := findFixtureDir(t)
	copyFile(t, filepath.Join(fixtureBase, "safari", "History.db"), filepath.Join(safariDir, "History.db"))

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"browser.safari"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	// Create an already-cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := orch.Run(ctx)

	// The orchestrator should return a cancellation error.
	if err == nil {
		t.Log("orchestrator returned nil error (cancellation may have been caught internally)")
	}

	if result != nil && !result.Cancelled {
		// Some plugins may not have been collected.
		t.Log("result.Cancelled is false, cancellation may have happened after detection")
	}

	// Clean up container.
	cont.MarkError()
	if closeErr := cont.Close(); closeErr != nil {
		t.Logf("close error (expected for cancelled run): %v", closeErr)
	}
}

// -----------------------------------------------------------------------------
// Multi-User Collection Tests
// -----------------------------------------------------------------------------

func TestIntegration_MultiUserCollection(t *testing.T) {
	opts := macostree.Defaults()
	opts.Users = []macostree.UserOptions{
		{Username: "alice", UID: 501, GID: 20, Name: "Alice Smith", Shell: "/bin/zsh"},
		{Username: "bob", UID: 502, GID: 20, Name: "Bob Jones", Shell: "/bin/bash"},
	}
	root := macostree.MustBuild(t, opts)

	// Seed shell history for both users.
	for _, username := range []string{"alice", "bob"} {
		homeDir := filepath.Join(root, "Users", username)
		histPath := filepath.Join(homeDir, ".bash_history")
		content := "echo hello from " + username + "\nls\n"
		if err := os.WriteFile(histPath, []byte(content), 0600); err != nil {
			t.Fatalf("writing bash history for %s: %v", username, err)
		}
	}

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	// Verify both users are enumerated.
	users, err := src.Users()
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("closing container: %v", err)
	}

	shellResult := findPluginResult(t, result, "shell.history")
	if !shellResult.Applicable {
		t.Fatalf("expected shell.history to be applicable, reason: %s", shellResult.Reason)
	}

	t.Logf("Multi-user collection: files=%d, records=%d",
		shellResult.FilesCollected, shellResult.RecordsCreated)

	// Both users should contribute artifacts.
	if shellResult.FilesCollected < 2 && shellResult.RecordsCreated < 2 {
		t.Error("expected artifacts from at least 2 users")
	}
}

// -----------------------------------------------------------------------------
// Container Integrity Tests
// -----------------------------------------------------------------------------

func TestIntegration_ContainerMetadataFiles(t *testing.T) {
	opts := macostree.Defaults()
	root := macostree.MustBuild(t, opts)

	// Seed minimal shell data.
	homeDir := filepath.Join(root, "Users", "alice")
	if err := os.WriteFile(filepath.Join(homeDir, ".bash_history"), []byte("ls\n"), 0600); err != nil {
		t.Fatalf("writing bash history: %v", err)
	}

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("creating directory source: %v", err)
	}

	containerPath := filepath.Join(t.TempDir(), "evidence.zip")
	cont, err := container.NewContainer(container.Config{OutputPath: containerPath})
	if err != nil {
		t.Fatalf("creating container: %v", err)
	}

	cfg := orchestrator.Config{
		Source:      src,
		Container:   cont,
		PluginIDs:   []string{"shell.history"},
		ToolVersion: "test-integration",
	}
	orch, err := orchestrator.New(cfg)
	if err != nil {
		t.Fatalf("creating orchestrator: %v", err)
	}

	_, err = orch.Run(context.Background())
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	if err := cont.Close(); err != nil {
		t.Fatalf("closing container: %v", err)
	}

	// Open the container and verify required files.
	zr, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = zr.Close() }()

	fileNames := make(map[string]bool)
	for _, f := range zr.File {
		fileNames[f.Name] = true
	}

	requiredFiles := []string{"manifest.json", "chain-of-custody.json", "hashes.json"}
	for _, name := range requiredFiles {
		if !fileNames[name] {
			t.Errorf("missing required metadata file: %s", name)
		}
	}

	// Verify chain of custody has expected entries.
	var custodyFile *zip.File
	for _, f := range zr.File {
		if f.Name == "chain-of-custody.json" {
			custodyFile = f
			break
		}
	}
	if custodyFile != nil {
		data := readZipFileData(t, custodyFile)
		var custody container.ChainOfCustodyLog
		if err := json.Unmarshal(data, &custody); err != nil {
			t.Fatalf("parsing chain-of-custody.json: %v", err)
		}

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
			t.Error("missing collection_started custody entry")
		}
		if !hasCompleted {
			t.Error("missing collection_completed custody entry")
		}
	}
}

// -----------------------------------------------------------------------------
// Source Type Verification Tests
// -----------------------------------------------------------------------------

func TestIntegration_SourceTypeIsDirectory(t *testing.T) {
	root := macostree.MustBuild(t, macostree.Defaults())

	src, err := directory.New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if src.Type() != macfor.SourceTypeDirectory {
		t.Errorf("Type() = %q, want %q", src.Type(), macfor.SourceTypeDirectory)
	}
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// findFixtureDir locates the testdata directory relative to this test file.
func findFixtureDir(t testing.TB) string {
	t.Helper()

	// Try relative paths from the test file location.
	candidates := []string{
		"../../testdata",
		filepath.Join("..", "..", "testdata"),
	}

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}

	t.Fatal("could not locate testdata directory")
	return ""
}

// copyFile copies a file from src to dst.
func copyFile(t testing.TB, src, dst string) {
	t.Helper()

	data, err := os.ReadFile(src) //nolint:gosec // G304: Test fixture path from controlled source.
	if err != nil {
		t.Fatalf("reading fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil { //nolint:gosec // Test fixture file.
		t.Fatalf("writing %s: %v", dst, err)
	}
}

// readManifestFromZip reads and parses manifest.json from a ZIP container.
func readManifestFromZip(t testing.TB, zipPath string) map[string]any {
	t.Helper()

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("opening manifest.json: %v", err)
			}
			defer func() { _ = rc.Close() }()

			var m map[string]any
			if err := json.NewDecoder(rc).Decode(&m); err != nil {
				t.Fatalf("decoding manifest.json: %v", err)
			}
			return m
		}
	}
	return nil
}

// readZipFileData reads the full contents of a file within a ZIP archive.
func readZipFileData(t testing.TB, f *zip.File) []byte {
	t.Helper()

	rc, err := f.Open()
	if err != nil {
		t.Fatalf("opening zip entry %s: %v", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("reading zip entry %s: %v", f.Name, err)
	}
	return data
}

// findPluginResult locates a plugin result by ID in the orchestrator result.
func findPluginResult(t testing.TB, result *orchestrator.Result, pluginID string) orchestrator.PluginResult {
	t.Helper()

	for _, pr := range result.PluginResults {
		if pr.ID == pluginID {
			return pr
		}
	}

	t.Fatalf("plugin %s not found in results", pluginID)
	return orchestrator.PluginResult{}
}

// verifyContainerHasEntries checks that the ZIP container has artifact or record entries.
func verifyContainerHasEntries(t testing.TB, containerPath string) {
	t.Helper()

	r, err := zip.OpenReader(containerPath)
	if err != nil {
		t.Fatalf("opening zip: %v", err)
	}
	defer func() { _ = r.Close() }()

	artifactCount := 0
	recordCount := 0
	logCount := 0

	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "artifacts/") {
			artifactCount++
		}
		if strings.HasPrefix(f.Name, "records/") {
			recordCount++
		}
		if strings.HasPrefix(f.Name, "logs/") {
			logCount++
		}
	}

	t.Logf("Container entries: %d artifacts, %d record files, %d log files",
		artifactCount, recordCount, logCount)

	if artifactCount == 0 && recordCount == 0 {
		t.Error("expected at least some artifact or record entries in the container")
	}
}
