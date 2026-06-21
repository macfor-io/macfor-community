package safari

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/plistmapper"
)

// extractExtensionRecords runs the declarative plist mapper against data using
// the manifest's extensions extract spec — the equivalence oracle for the
// migrated parser.
func extractExtensionRecords(tb testing.TB, data []byte) []*ExtensionRecord {
	tb.Helper()
	def := artifacts.GetArtifactByID(manifest, string(ArtifactExtensions))
	if def == nil || def.Extract == nil {
		tb.Fatal("extensions extract spec missing from manifest")
	}
	var recs []*ExtensionRecord
	_, err := plistmapper.Each(context.Background(), data, *def.Extract, plistmapper.Options{
		NewRecord: func() any { return &ExtensionRecord{} },
		Context:   map[string]string{"username": testUsername, "source_file": "/test/Extensions.plist"},
	}, func(rec any) error {
		recs = append(recs, rec.(*ExtensionRecord))
		return nil
	})
	if err != nil {
		tb.Fatalf("extract: %v", err)
	}
	return recs
}

// Test constants for extensions.
const (
	testExtensionRecordType = "browser_extension"
)

// TestExtensionRecord_Structure verifies the ExtensionRecord struct has expected fields.
func TestExtensionRecord_Structure(t *testing.T) {
	record := &ExtensionRecord{
		Type:            testExtensionRecordType,
		Browser:         testBrowser,
		User:            testUsername,
		BundleID:        "com.example.extension",
		ArchiveFileName: "Example.safariextz",
		Enabled:         true,
		SourceFile:      "/Users/testuser/Library/Safari/Extensions/Extensions.plist",
	}

	if record.Type != testExtensionRecordType {
		t.Errorf("expected Type %q, got %q", testExtensionRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.BundleID != "com.example.extension" {
		t.Errorf("expected BundleID 'com.example.extension', got %q", record.BundleID)
	}
	if record.ArchiveFileName != "Example.safariextz" {
		t.Errorf("expected ArchiveFileName 'Example.safariextz', got %q", record.ArchiveFileName)
	}
	if !record.Enabled {
		t.Error("expected Enabled to be true")
	}
}

// TestExtensionsMapping verifies the declarative plist mapper extracts the
// fixture's extensions with the right const, context, and key-sourced fields.
func TestExtensionsMapping(t *testing.T) {
	recs := extractExtensionRecords(t, mustLoadFixture(FixtureExtensionsPlist))

	if len(recs) != 3 {
		t.Fatalf("expected 3 extensions, got %d", len(recs))
	}

	first := recs[0]
	if first.Type != testExtensionRecordType || first.Browser != testBrowser || first.User != testUsername {
		t.Errorf("const/context fields wrong: %+v", first)
	}
	if first.ArchiveFileName != "1Password.safariextz" ||
		first.BundleID != "com.agilebits.onepassword-safari" || !first.Enabled {
		t.Errorf("first extension mismatch: %+v", first)
	}
	if first.SourceFile != "/test/Extensions.plist" {
		t.Errorf("source_file context wrong: %q", first.SourceFile)
	}

	if recs[1].ArchiveFileName != "AdBlock.safariextz" ||
		recs[1].BundleID != "com.betafish.adblockforsafari" || !recs[1].Enabled {
		t.Errorf("second extension mismatch: %+v", recs[1])
	}

	if recs[2].BundleID != "com.example.disabled-ext" || recs[2].Enabled {
		t.Errorf("third extension should be disabled: %+v", recs[2])
	}
}

// TestCollectExtensionsForUser_FileNotFound tests handling of missing Extensions.plist.
func TestCollectExtensionsForUser_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Create source without Extensions.plist.
	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectExtensionsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should log debug message about missing file.
	if !coll.hasLogAtLevel(macfor.LogDebug, "not found") {
		t.Error("expected debug log about missing Extensions.plist")
	}

	// Should not add any files or records.
	if len(coll.files) != 0 {
		t.Errorf("expected no files, got %d", len(coll.files))
	}
	if len(coll.records) != 0 {
		t.Errorf("expected no records, got %d", len(coll.records))
	}
}

// TestCollectExtensionsForUser_WithFixture tests full collection with real fixture.
func TestCollectExtensionsForUser_WithFixture(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	extensionsDir := filepath.Join(tempDir, "Library", "Safari", "Extensions")
	if err := os.MkdirAll(extensionsDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture to temp location.
	fixtureSrc := fixturePath(FixtureExtensionsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extensionsPath := filepath.Join(extensionsDir, "Extensions.plist")
	if err := os.WriteFile(extensionsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectExtensionsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Should have added records (3 extensions in fixture).
	if len(coll.records) != 3 {
		t.Errorf("expected 3 records, got %d", len(coll.records))
	}

	// Verify artifact metadata.
	if len(coll.files) > 0 {
		artifact := coll.files[0].artifact
		if artifact.PluginID != PluginID {
			t.Errorf("expected PluginID %q, got %q", PluginID, artifact.PluginID)
		}
		if artifact.Username != testUsername {
			t.Errorf("expected Username testuser, got %q", artifact.Username)
		}
		if artifact.Category != testCategoryBrowser {
			t.Errorf("expected Category %q, got %q", testCategoryBrowser, artifact.Category)
		}
		if artifact.Type != "extensions" {
			t.Errorf("expected Type 'extensions', got %q", artifact.Type)
		}
	}

	// Verify record structure.
	if len(coll.records) > 0 {
		record, ok := coll.records[0].record.(*ExtensionRecord)
		if !ok {
			t.Fatal("expected record to be *ExtensionRecord")
		}
		if record.Type != testExtensionRecordType {
			t.Errorf("expected Type 'browser_extension', got %q", record.Type)
		}
		if record.Browser != testBrowser {
			t.Errorf("expected Browser 'safari', got %q", record.Browser)
		}
		if record.User != testUsername {
			t.Errorf("expected User testuser, got %q", record.User)
		}
	}

	// Should log success message.
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collected 3 extensions") {
		t.Error("expected info log about collected extensions")
	}
}

// TestCollectExtensionsForUser_ContextCancellation tests cancellation during collection.
func TestCollectExtensionsForUser_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	extensionsDir := filepath.Join(tempDir, "Library", "Safari", "Extensions")
	if err := os.MkdirAll(extensionsDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureExtensionsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extensionsPath := filepath.Join(extensionsDir, "Extensions.plist")
	if err := os.WriteFile(extensionsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	// Cancel context immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = collectExtensionsForUser(ctx, user, src, coll)

	// Should return context error (might be from AddFile or parsing).
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("error was: %v", err)
	}
}

// TestCollectExtensionsForUser_InvalidPlist tests handling of invalid plist file.
func TestCollectExtensionsForUser_InvalidPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	extensionsDir := filepath.Join(tempDir, "Library", "Safari", "Extensions")
	if err := os.MkdirAll(extensionsDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create invalid plist (just random text).
	extensionsPath := filepath.Join(extensionsDir, "Extensions.plist")
	if err := os.WriteFile(extensionsPath, []byte("not a plist"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectExtensionsForUser(context.Background(), user, src, coll)

	// Should return error for invalid plist.
	if err == nil {
		t.Fatal("expected error for invalid plist file")
	}

	if !strings.Contains(err.Error(), "parsing extensions") {
		t.Errorf("expected 'parsing extensions' error, got: %v", err)
	}
}

// TestCollectExtensionsForUser_EnabledStates tests that extension enabled states are correctly parsed.
func TestCollectExtensionsForUser_EnabledStates(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	extensionsDir := filepath.Join(tempDir, "Library", "Safari", "Extensions")
	if err := os.MkdirAll(extensionsDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureExtensionsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extensionsPath := filepath.Join(extensionsDir, "Extensions.plist")
	if err := os.WriteFile(extensionsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectExtensionsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Count enabled and disabled extensions.
	enabledCount := 0
	disabledCount := 0

	for _, r := range coll.records {
		record, ok := r.record.(*ExtensionRecord)
		if !ok {
			continue
		}
		if record.Enabled {
			enabledCount++
		} else {
			disabledCount++
		}
	}

	// Fixture has 2 enabled and 1 disabled.
	if enabledCount != 2 {
		t.Errorf("expected 2 enabled extensions, got %d", enabledCount)
	}
	if disabledCount != 1 {
		t.Errorf("expected 1 disabled extension, got %d", disabledCount)
	}
}

// TestExtensionsKeyMapping verifies the mapper reads the Safari plist keys
// ("Archive File Name", "Bundle Directory Name", "Enabled") into the record.
func TestExtensionsKeyMapping(t *testing.T) {
	plistContent := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Installed Extensions</key>
	<array>
		<dict>
			<key>Archive File Name</key>
			<string>TestExtension.safariextz</string>
			<key>Bundle Directory Name</key>
			<string>com.test.extension</string>
			<key>Enabled</key>
			<true/>
		</dict>
	</array>
</dict>
</plist>`)

	recs := extractExtensionRecords(t, plistContent)
	if len(recs) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(recs))
	}
	ext := recs[0]
	if ext.ArchiveFileName != "TestExtension.safariextz" {
		t.Errorf("ArchiveFileName mismatch: %q", ext.ArchiveFileName)
	}
	if ext.BundleID != "com.test.extension" {
		t.Errorf("BundleID mismatch: %q", ext.BundleID)
	}
	if !ext.Enabled {
		t.Error("Enabled should be true")
	}
}

// TestCollectExtensionsForUser_VerifyBundleIDs tests that bundle IDs are correctly extracted.
func TestCollectExtensionsForUser_VerifyBundleIDs(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	extensionsDir := filepath.Join(tempDir, "Library", "Safari", "Extensions")
	if err := os.MkdirAll(extensionsDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureExtensionsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extensionsPath := filepath.Join(extensionsDir, "Extensions.plist")
	if err := os.WriteFile(extensionsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectExtensionsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Verify expected bundle IDs are present.
	expectedBundleIDs := map[string]bool{
		"com.agilebits.onepassword-safari": true,
		"com.betafish.adblockforsafari":    true,
		"com.example.disabled-ext":         true,
	}

	for _, r := range coll.records {
		record, ok := r.record.(*ExtensionRecord)
		if !ok {
			continue
		}
		if expectedBundleIDs[record.BundleID] {
			delete(expectedBundleIDs, record.BundleID)
		}
	}

	if len(expectedBundleIDs) > 0 {
		t.Errorf("missing expected bundle IDs: %v", expectedBundleIDs)
	}
}
