package safari

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/internal/container"
	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/sqlite"
	"github.com/macfor-io/macfor-community/sources/live"
)

const darwinOS = "darwin"

// Integration tests that verify the Safari plugin works with real components.
// These tests use the live source and test fixtures.

func TestIntegration_PluginInfo(t *testing.T) {
	p := &Plugin{}

	info := p.Info()

	if info.ID != PluginID {
		t.Errorf("expected ID %q, got %q", PluginID, info.ID)
	}

	if info.Name != "Safari Browser" {
		t.Errorf("expected name 'Safari Browser', got %q", info.Name)
	}

	if info.Version == "" {
		t.Error("version should not be empty")
	}

	// Check categories.
	expectedCategories := []string{"browser", "user", "web"}
	for _, expected := range expectedCategories {
		found := false
		for _, cat := range info.Categories {
			if cat == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing category: %s", expected)
		}
	}

	// Check platform spec.
	if len(info.Platforms) == 0 {
		t.Error("expected at least one platform spec")
	}
	if info.Platforms[0].OS != "darwin" {
		t.Errorf("expected platform darwin, got %s", info.Platforms[0].OS)
	}
	if info.Platforms[0].MinVersion != "10.12" {
		t.Errorf("expected min version 10.12, got %s", info.Platforms[0].MinVersion)
	}
}

func TestIntegration_PluginRegistration(t *testing.T) {
	// Plugin should be auto-registered via init().
	p, ok := macfor.ByID(PluginID)
	if !ok {
		t.Fatal("browser.safari plugin not registered")
	}

	if p.Info().ID != PluginID {
		t.Errorf("registered plugin ID mismatch: got %s", p.Info().ID)
	}
}

func TestIntegration_DetectWithLiveSource(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	// Log detection result.
	t.Logf("Detection result: applicable=%v, confidence=%.2f, reason=%s",
		result.Applicable, result.Confidence, result.Reason)

	// OS version should be set.
	if result.OSVersion == "" {
		t.Error("OSVersion should be set")
	}

	// Metadata should be populated.
	if result.Metadata == nil {
		t.Error("Metadata should not be nil")
	}

	if result.Applicable {
		// If applicable, should have artifacts detected.
		artifacts, ok := result.Metadata["artifacts_detected"]
		if !ok {
			t.Error("missing artifacts_detected in metadata")
		} else {
			t.Logf("Detected artifacts: %v", artifacts)
		}

		users, ok := result.Metadata["users_with_safari"]
		if !ok {
			t.Error("missing users_with_safari in metadata")
		} else {
			t.Logf("Users with Safari: %v", users)
		}

		if result.Confidence != 1.0 {
			t.Errorf("expected confidence 1.0, got %.2f", result.Confidence)
		}

		// Check Safari version if detected.
		if version, ok := result.Metadata["safari_version"]; ok {
			t.Logf("Safari version: %v", version)
		}
	} else {
		t.Log("Plugin not applicable on this system (no Safari artifacts found)")
	}
}

func TestIntegration_CollectWithLiveSource(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// First check if applicable.
	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Applicable {
		t.Skip("browser.safari not applicable on this system")
	}

	// Create container for collection.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "safari-evidence.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(container.CollectionMeta{
		ToolVersion: "1.0.0-test",
		SourceType:  "live",
	})

	// Create collector.
	coll := container.NewCollector(cont, PluginID)

	// Run collection.
	err = p.Collect(context.Background(), src, coll)

	// Collection may return errors for inaccessible files, but shouldn't fail completely.
	if err != nil {
		t.Logf("Collection returned non-fatal errors: %v", err)
	}

	// Finalize collector.
	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("failed to finalize collector: %v", err)
	}

	cont.AddPluginSummary(summary)

	if err := cont.Finalize(); err != nil {
		t.Fatalf("failed to finalize container: %v", err)
	}
	if err := cont.Close(); err != nil {
		t.Fatalf("failed to close container: %v", err)
	}

	// Log collection statistics.
	t.Logf("Collection summary: files=%d, records=%d, errors=%d",
		summary.FilesCollected, summary.RecordsCreated, summary.ErrorsEncountered)

	// Verify container exists.
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("container was not created")
	}

	// Verify container contents.
	verifyContainerContents(t, outputPath)
}

func TestIntegration_CollectionCancellation(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// Create cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "safari-cancelled.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	coll := container.NewCollector(cont, PluginID)

	// Collection should return context cancelled error.
	err = p.Collect(ctx, src, coll)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	// Cleanup.
	cont.MarkError()
	_ = cont.Close()
}

func TestIntegration_CollectWithHistoryOnlyOption(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{
		Options: &CollectionOptions{
			HistoryOnly: true,
		},
	}

	// First check if applicable.
	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Applicable {
		t.Skip("browser.safari not applicable on this system")
	}

	// Create container for collection.
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "safari-history-only.zip")

	cont, err := container.NewContainer(container.Config{OutputPath: outputPath})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	cont.SetCollectionMetadata(container.CollectionMeta{
		ToolVersion: "1.0.0-test",
		SourceType:  "live",
	})

	coll := container.NewCollector(cont, PluginID)

	// Run collection with HistoryOnly option.
	_ = p.Collect(context.Background(), src, coll)

	summary, err := coll.Finalize()
	if err != nil {
		t.Fatalf("failed to finalize collector: %v", err)
	}

	cont.AddPluginSummary(summary)
	_ = cont.Finalize()
	_ = cont.Close()

	// Verify only history files were collected.
	zr, err := zip.OpenReader(outputPath)
	if err != nil {
		t.Fatalf("failed to open container: %v", err)
	}
	defer func() { _ = zr.Close() }()

	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "artifacts/") {
			// Should only contain history artifacts.
			if strings.Contains(f.Name, "downloads") ||
				strings.Contains(f.Name, "bookmarks") ||
				strings.Contains(f.Name, "cookies") ||
				strings.Contains(f.Name, "extensions") ||
				strings.Contains(f.Name, "localstorage") ||
				strings.Contains(f.Name, "session") {
				t.Errorf("unexpected artifact in HistoryOnly mode: %s", f.Name)
			}
		}
	}
}

// TestIntegration_ParseHistoryFixtures tests parsing History.db fixtures.
func TestIntegration_ParseHistoryFixtures(t *testing.T) {
	fixtures := []struct {
		name          string
		expectedCount int
		description   string
	}{
		{FixtureHistoryDB, 14, "main fixture with 14 visits"},
		{FixtureHistoryDBEmpty, 0, "empty database"},
		{FixtureHistoryDBUnicode, 6, "unicode URLs and titles"},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			path := fixturePath(tt.name)

			db, err := sqlite.OpenReadOnly(path)
			if err != nil {
				t.Fatalf("failed to open fixture: %v", err)
			}
			defer func() { _ = db.Close() }()

			// Verify tables exist.
			if !sqlite.TableExists(db, "history_items") {
				t.Fatal("history_items table not found")
			}
			if !sqlite.TableExists(db, "history_visits") {
				t.Fatal("history_visits table not found")
			}

			// Extract visits via the manifest-driven table mapper.
			visits := extractHistoryVisits(t, db, path)

			if len(visits) != tt.expectedCount {
				t.Errorf("expected %d visits, got %d", tt.expectedCount, len(visits))
			}

			t.Logf("%s: parsed %d visits - %s", tt.name, len(visits), tt.description)
		})
	}
}

// TestIntegration_ParseDownloadsFixtures tests parsing Downloads.plist fixtures.
func TestIntegration_ParseDownloadsFixtures(t *testing.T) {
	t.Run("Downloads.plist", func(t *testing.T) {
		path := fixturePath(FixtureDownloadsPlist)
		content, err := os.ReadFile(path) //nolint:gosec // Test code
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("fixture is empty")
		}

		t.Logf("Downloads.plist fixture size: %d bytes", len(content))
	})

	t.Run("Downloads_empty.plist", func(t *testing.T) {
		path := fixturePath(FixtureDownloadsPlistEmpty)
		content, err := os.ReadFile(path) //nolint:gosec // Test code
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("fixture is empty")
		}

		t.Logf("Downloads_empty.plist fixture size: %d bytes", len(content))
	})
}

// TestIntegration_ParseBookmarksFixtures tests parsing Bookmarks.plist fixtures.
func TestIntegration_ParseBookmarksFixtures(t *testing.T) {
	t.Run("Bookmarks.plist", func(t *testing.T) {
		path := fixturePath(FixtureBookmarksPlist)
		content, err := os.ReadFile(path) //nolint:gosec // Test code
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("fixture is empty")
		}

		t.Logf("Bookmarks.plist fixture size: %d bytes", len(content))
	})
}

// TestIntegration_ParseCookiesFixtures tests parsing Cookies.binarycookies fixtures.
func TestIntegration_ParseCookiesFixtures(t *testing.T) {
	t.Run("Cookies.binarycookies", func(t *testing.T) {
		content, err := loadFixture(FixtureCookiesBinary)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		cookies, err := parseBinaryCookies(content)
		if err != nil {
			t.Fatalf("failed to parse cookies: %v", err)
		}

		// Fixture should have 3 cookies.
		if len(cookies) != 3 {
			t.Errorf("expected 3 cookies, got %d", len(cookies))
		}

		t.Logf("Parsed %d cookies from fixture", len(cookies))

		// Verify cookie structure.
		for i, cookie := range cookies {
			if cookie.Domain == "" {
				t.Errorf("cookie %d: empty domain", i)
			}
			if cookie.Name == "" {
				t.Errorf("cookie %d: empty name", i)
			}
			t.Logf("  Cookie %d: %s on %s (secure=%v, httpOnly=%v)",
				i, cookie.Name, cookie.Domain, cookie.IsSecure(), cookie.IsHTTPOnly())
		}
	})

	t.Run("Cookies_empty.binarycookies", func(t *testing.T) {
		content, err := loadFixture(FixtureCookiesBinaryEmpty)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		cookies, err := parseBinaryCookies(content)
		if err != nil {
			t.Fatalf("failed to parse empty cookies: %v", err)
		}

		if len(cookies) != 0 {
			t.Errorf("expected 0 cookies from empty file, got %d", len(cookies))
		}
	})
}

// TestIntegration_ParseExtensionsFixtures tests parsing Extensions.plist fixtures.
func TestIntegration_ParseExtensionsFixtures(t *testing.T) {
	t.Run("Extensions.plist", func(t *testing.T) {
		path := fixturePath(FixtureExtensionsPlist)
		content, err := os.ReadFile(path) //nolint:gosec // Test code
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("fixture is empty")
		}

		t.Logf("Extensions.plist fixture size: %d bytes", len(content))
	})
}

// TestIntegration_ParseLocalStorageFixtures tests parsing LocalStorage fixtures.
func TestIntegration_ParseLocalStorageFixtures(t *testing.T) {
	fixtures, err := listLocalStorageFixtures()
	if err != nil {
		t.Fatalf("failed to list LocalStorage fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Skip("no LocalStorage fixtures found")
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(testdataDir(), "LocalStorage", name)

			db, err := sqlite.OpenReadOnly(path)
			if err != nil {
				t.Fatalf("failed to open fixture: %v", err)
			}
			defer func() { _ = db.Close() }()

			// Verify ItemTable exists.
			if !sqlite.TableExists(db, "ItemTable") {
				t.Fatal("ItemTable not found")
			}

			// Count items.
			count, err := sqlite.GetTableRowCount(db, "ItemTable")
			if err != nil {
				t.Fatalf("failed to count items: %v", err)
			}

			t.Logf("LocalStorage %s: %d items", name, count)
		})
	}
}

// TestIntegration_ParseLastSessionFixtures tests parsing LastSession.plist fixtures.
func TestIntegration_ParseLastSessionFixtures(t *testing.T) {
	t.Run("LastSession.plist", func(t *testing.T) {
		path := fixturePath(FixtureLastSessionPlist)
		content, err := os.ReadFile(path) //nolint:gosec // Test code
		if err != nil {
			t.Fatalf("failed to read fixture: %v", err)
		}

		if len(content) == 0 {
			t.Fatal("fixture is empty")
		}

		t.Logf("LastSession.plist fixture size: %d bytes", len(content))
	})
}

// TestIntegration_RecordSerialization tests JSON serialization of record types.
func TestIntegration_RecordSerialization(t *testing.T) {
	t.Run("HistoryVisit JSON", func(t *testing.T) {
		visit := &HistoryVisit{
			Type:           "browser_history",
			Browser:        "safari",
			User:           "testuser",
			URL:            "https://example.com",
			Title:          "Example",
			VisitTime:      time.Now(),
			VisitCount:     5,
			LoadSuccessful: true,
			SourceFile:     "/Users/testuser/Library/Safari/History.db",
			HistoryItemID:  1,
			VisitID:        1,
		}

		data, err := json.Marshal(visit)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled HistoryVisit
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Type != visit.Type {
			t.Error("Type mismatch after serialization")
		}
		if unmarshaled.URL != visit.URL {
			t.Error("URL mismatch after serialization")
		}
		if unmarshaled.Browser != "safari" {
			t.Errorf("expected browser 'safari', got %q", unmarshaled.Browser)
		}
	})

	t.Run("DownloadRecord JSON", func(t *testing.T) {
		record := &DownloadRecord{
			Type:           "browser_download",
			Browser:        "safari",
			User:           "testuser",
			URL:            "https://example.com/file.zip",
			LocalPath:      "/Users/testuser/Downloads/file.zip",
			FileName:       "file.zip",
			TotalBytes:     1024,
			ReceivedBytes:  1024,
			StartTime:      time.Now(),
			EndTime:        time.Now(),
			State:          DownloadStateComplete,
			Identifier:     "uuid-123",
			RemoveWhenDone: false,
			SourceFile:     "/Users/testuser/Library/Safari/Downloads.plist",
		}

		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled DownloadRecord
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Type != record.Type {
			t.Error("Type mismatch after serialization")
		}
		if unmarshaled.State != DownloadStateComplete {
			t.Errorf("expected state 'complete', got %q", unmarshaled.State)
		}
	})

	t.Run("CookieRecord JSON", func(t *testing.T) {
		record := &CookieRecord{
			Type:         "browser_cookie",
			Browser:      "safari",
			User:         "testuser",
			Domain:       ".example.com",
			Name:         "session",
			Path:         "/",
			ValuePreview: "[REDACTED - 32 chars]",
			ValueLength:  32,
			CreationTime: time.Now(),
			ExpiryTime:   time.Now().Add(24 * time.Hour),
			Secure:       true,
			HTTPOnly:     true,
			SourceFile:   "/Users/testuser/Library/Cookies/Cookies.binarycookies",
		}

		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var unmarshaled CookieRecord
		if err := json.Unmarshal(data, &unmarshaled); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if unmarshaled.Domain != record.Domain {
			t.Error("Domain mismatch after serialization")
		}
		if !unmarshaled.Secure {
			t.Error("expected Secure=true")
		}
		if !unmarshaled.HTTPOnly {
			t.Error("expected HTTPOnly=true")
		}
	})
}

// TestIntegration_EdgeCases tests edge case handling.
func TestIntegration_EdgeCases(t *testing.T) {
	t.Run("empty history database", func(t *testing.T) {
		path := fixturePath(FixtureHistoryDBEmpty)

		db, err := sqlite.OpenReadOnly(path)
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer func() { _ = db.Close() }()

		if count := len(extractHistoryVisits(t, db, path)); count != 0 {
			t.Errorf("expected 0 records from empty database, got %d", count)
		}
	})

	t.Run("empty cookies file", func(t *testing.T) {
		content, err := loadFixture(FixtureCookiesBinaryEmpty)
		if err != nil {
			t.Fatalf("failed to load fixture: %v", err)
		}

		cookies, err := parseBinaryCookies(content)
		if err != nil {
			t.Fatalf("parse should not fail on empty file: %v", err)
		}

		if len(cookies) != 0 {
			t.Errorf("expected 0 cookies from empty file, got %d", len(cookies))
		}
	})

	t.Run("large history database", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping large fixture test in short mode")
		}

		path := fixturePath(FixtureHistoryDBLarge)

		db, err := sqlite.OpenReadOnly(path)
		if err != nil {
			t.Fatalf("failed to open fixture: %v", err)
		}
		defer func() { _ = db.Close() }()

		start := time.Now()

		count := len(extractHistoryVisits(t, db, path))

		elapsed := time.Since(start)

		// Should handle 1000+ visits reasonably quickly.
		if count < 1000 {
			t.Errorf("expected at least 1000 visits, got %d", count)
		}

		// Should complete in under 5 seconds.
		if elapsed > 5*time.Second {
			t.Errorf("large fixture parsing took too long: %v", elapsed)
		}

		t.Logf("Parsed %d visits in %v", count, elapsed)
	})

	t.Run("invalid cookie magic", func(t *testing.T) {
		invalidData := []byte("notacookiefile")
		_, err := parseBinaryCookies(invalidData)
		if !errors.Is(err, ErrInvalidCookieMagic) {
			t.Errorf("expected ErrInvalidCookieMagic, got: %v", err)
		}
	})
}

// TestIntegration_AllFixturesLoadable verifies all fixtures can be loaded.
func TestIntegration_AllFixturesLoadable(t *testing.T) {
	fixtures, err := listFixtures()
	if err != nil {
		t.Fatalf("failed to list fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Fatal("no fixtures found")
	}

	t.Logf("Found %d fixtures", len(fixtures))

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			content, err := loadFixture(name)
			if err != nil {
				t.Errorf("failed to load fixture %s: %v", name, err)
			}
			if len(content) == 0 {
				t.Logf("warning: fixture %s is empty", name)
			}
		})
	}
}

// TestIntegration_LocalStorageFixturesLoadable verifies LocalStorage fixtures can be loaded.
func TestIntegration_LocalStorageFixturesLoadable(t *testing.T) {
	fixtures, err := listLocalStorageFixtures()
	if err != nil {
		t.Fatalf("failed to list LocalStorage fixtures: %v", err)
	}

	if len(fixtures) == 0 {
		t.Skip("no LocalStorage fixtures found")
	}

	t.Logf("Found %d LocalStorage fixtures", len(fixtures))

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(testdataDir(), "LocalStorage", name)
			content, err := os.ReadFile(path) //nolint:gosec // Test code
			if err != nil {
				t.Errorf("failed to read fixture: %v", err)
			}
			if len(content) == 0 {
				t.Logf("warning: fixture %s is empty", name)
			}
		})
	}
}

// TestIntegration_VersionComparison verifies macOS version comparison.
func TestIntegration_VersionComparison(t *testing.T) {
	testCases := []struct {
		current  string
		minimum  string
		expected bool
	}{
		{"14.2.1", "10.12", true},
		{"14.2", "10.12", true},
		{"10.12", "10.12", true},
		{"10.11", "10.12", false},
		{"10.12.1", "10.12", true},
		{"10.12", "10.12.1", false},
		{"15.0", "14.0", true},
		{"13.0", "14.0", false},
	}

	for _, tc := range testCases {
		t.Run(tc.current+"_vs_"+tc.minimum, func(t *testing.T) {
			result := artifacts.VersionAtLeast(tc.current, tc.minimum)
			if result != tc.expected {
				t.Errorf("artifacts.VersionAtLeast(%q, %q) = %v, expected %v",
					tc.current, tc.minimum, result, tc.expected)
			}
		})
	}
}

// TestIntegration_ArtifactTypes verifies artifact type constants.
func TestIntegration_ArtifactTypes(t *testing.T) {
	artifacts := []ArtifactType{
		ArtifactHistory,
		ArtifactDownloads,
		ArtifactBookmarks,
		ArtifactReadingList,
		ArtifactCookies,
		ArtifactExtensions,
		ArtifactLocalStorage,
		ArtifactLastSession,
		ArtifactTopSites,
	}

	for _, a := range artifacts {
		if a == "" {
			t.Errorf("artifact type should not be empty")
		}
	}

	// Verify collection order includes expected artifacts.
	expectedInOrder := []ArtifactType{
		ArtifactHistory,
		ArtifactDownloads,
		ArtifactBookmarks,
		ArtifactCookies,
		ArtifactExtensions,
		ArtifactLocalStorage,
		ArtifactLastSession,
	}

	for _, expected := range expectedInOrder {
		found := false
		for _, co := range CollectionOrder {
			if co == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("artifact %s not in CollectionOrder", expected)
		}
	}

	// History should be first in collection order.
	if CollectionOrder[0] != ArtifactHistory {
		t.Error("ArtifactHistory should be first in CollectionOrder")
	}
}

// TestIntegration_CollectWithMockCollector tests collection with mock collector.
func TestIntegration_CollectWithMockCollector(t *testing.T) {
	if runtime.GOOS != darwinOS {
		t.Skip("integration test only runs on macOS")
	}

	src, err := live.New()
	if err != nil {
		t.Fatalf("failed to create live source: %v", err)
	}

	p := &Plugin{}

	// Check if applicable first.
	result, err := p.Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}

	if !result.Applicable {
		t.Skip("browser.safari not applicable on this system")
	}

	// Use mock collector.
	coll := newMockCollector()

	_ = p.Collect(context.Background(), src, coll)

	// Log results.
	t.Logf("Collection results: %d files, %d records, %d errors, %d log messages",
		len(coll.files), len(coll.records), len(coll.errors), len(coll.logs))

	// Should have collected something (if applicable).
	if len(coll.files) == 0 && len(coll.records) == 0 && len(coll.errors) == 0 {
		t.Log("No files, records, or errors collected (user may have no Safari data)")
	}

	// Check file artifacts path patterns.
	for _, file := range coll.files {
		if !strings.HasPrefix(file.artifact.Category, "browser") {
			t.Errorf("expected artifact category to start with 'browser', got %q", file.artifact.Category)
		}
	}
}

// Helper functions

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

	// Verify required metadata files.
	requiredFiles := []string{"manifest.json", "chain-of-custody.json", "hashes.json"}
	for _, name := range requiredFiles {
		if _, ok := files[name]; !ok {
			t.Errorf("missing required file: %s", name)
		}
	}

	// Count artifacts and records.
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
