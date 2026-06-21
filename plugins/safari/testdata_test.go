package safari

import (
	"context"
	"database/sql"
	"testing"

	"howett.net/plist"
	_ "modernc.org/sqlite"
)

func TestFixtureExists(t *testing.T) {
	fixtures := []string{
		FixtureHistoryDB,
		FixtureHistoryDBEmpty,
		FixtureHistoryDBUnicode,
		FixtureHistoryDBLarge,
		FixtureDownloadsPlist,
		FixtureDownloadsPlistEmpty,
		FixtureBookmarksPlist,
		FixtureBookmarksPlistEmpty,
		FixtureCookiesBinary,
		FixtureCookiesBinaryEmpty,
		FixtureExtensionsPlist,
		FixtureLastSessionPlist,
		FixtureTopSitesPlist,
		FixtureLocalStorageGitHub,
		FixtureLocalStorageExample,
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			if !fixtureExists(fixture) {
				t.Errorf("fixture %q does not exist", fixture)
			}
		})
	}
}

func TestLoadFixture(t *testing.T) {
	content, err := loadFixture(FixtureHistoryDB)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}
	if len(content) == 0 {
		t.Error("fixture content is empty")
	}
	// SQLite databases start with "SQLite format 3\x00"
	if string(content[:16]) != "SQLite format 3\x00" {
		t.Error("fixture does not appear to be a SQLite database")
	}
}

func TestMustLoadFixture(t *testing.T) {
	content := mustLoadFixture(FixtureHistoryDB)
	if len(content) == 0 {
		t.Error("mustLoadFixture returned empty content")
	}
}

func TestFixturePath(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)
	if path == "" {
		t.Error("fixturePath returned empty string")
	}
}

func TestListFixtures(t *testing.T) {
	fixtures, err := listFixtures()
	if err != nil {
		t.Fatalf("failed to list fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Error("no fixtures found")
	}

	// Should have at least our main fixtures
	expectedMinCount := 10
	if len(fixtures) < expectedMinCount {
		t.Errorf("expected at least %d fixtures, got %d", expectedMinCount, len(fixtures))
	}
}

func TestListLocalStorageFixtures(t *testing.T) {
	fixtures, err := listLocalStorageFixtures()
	if err != nil {
		t.Fatalf("failed to list local storage fixtures: %v", err)
	}

	expectedCount := 2
	if len(fixtures) != expectedCount {
		t.Errorf("expected %d local storage fixtures, got %d", expectedCount, len(fixtures))
	}
}

func TestHistoryDBFixture(t *testing.T) {
	ctx := context.Background()
	path := fixturePath(FixtureHistoryDB)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify schema
	var tableName string
	err = db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='history_items'").Scan(&tableName)
	if err != nil {
		t.Errorf("history_items table not found: %v", err)
	}

	err = db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='history_visits'").Scan(&tableName)
	if err != nil {
		t.Errorf("history_visits table not found: %v", err)
	}

	// Verify data
	var itemCount, visitCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_items").Scan(&itemCount); err != nil {
		t.Fatalf("failed to count history items: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_visits").Scan(&visitCount); err != nil {
		t.Fatalf("failed to count history visits: %v", err)
	}

	if itemCount != 8 {
		t.Errorf("expected 8 history items, got %d", itemCount)
	}
	if visitCount != 14 {
		t.Errorf("expected 14 history visits, got %d", visitCount)
	}

	// Verify a specific URL exists
	var url string
	err = db.QueryRowContext(ctx, "SELECT url FROM history_items WHERE url LIKE '%github.com%'").Scan(&url)
	if err != nil {
		t.Errorf("expected github.com URL not found: %v", err)
	}
}

func TestHistoryDBEmptyFixture(t *testing.T) {
	ctx := context.Background()
	path := fixturePath(FixtureHistoryDBEmpty)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var itemCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_items").Scan(&itemCount); err != nil {
		t.Fatalf("failed to count history items: %v", err)
	}

	if itemCount != 0 {
		t.Errorf("expected 0 history items in empty fixture, got %d", itemCount)
	}
}

func TestHistoryDBUnicodeFixture(t *testing.T) {
	ctx := context.Background()
	path := fixturePath(FixtureHistoryDBUnicode)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var itemCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_items").Scan(&itemCount); err != nil {
		t.Fatalf("failed to count history items: %v", err)
	}

	if itemCount != 6 {
		t.Errorf("expected 6 history items in unicode fixture, got %d", itemCount)
	}

	// Verify Japanese URL exists
	var url string
	err = db.QueryRowContext(ctx, "SELECT url FROM history_items WHERE url LIKE '%ja.wikipedia%'").Scan(&url)
	if err != nil {
		t.Errorf("expected Japanese Wikipedia URL not found: %v", err)
	}

	// Verify Japanese title exists
	var title string
	err = db.QueryRowContext(ctx, "SELECT title FROM history_visits WHERE title LIKE '%プログラミング%'").Scan(&title)
	if err != nil {
		t.Errorf("expected Japanese title not found: %v", err)
	}
}

func TestHistoryDBLargeFixture(t *testing.T) {
	ctx := context.Background()
	path := fixturePath(FixtureHistoryDBLarge)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	var itemCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_items").Scan(&itemCount); err != nil {
		t.Fatalf("failed to count history items: %v", err)
	}

	if itemCount != 1000 {
		t.Errorf("expected 1000 history items in large fixture, got %d", itemCount)
	}
}

func TestDownloadsPlistFixture(t *testing.T) {
	content, err := loadFixture(FixtureDownloadsPlist)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	var downloads []map[string]interface{}
	_, err = plist.Unmarshal(content, &downloads)
	if err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	if len(downloads) != 3 {
		t.Errorf("expected 3 downloads, got %d", len(downloads))
	}

	// Verify first download has expected fields
	if downloads[0]["DownloadEntryURL"] == nil {
		t.Error("download missing DownloadEntryURL")
	}
	if downloads[0]["DownloadEntryPath"] == nil {
		t.Error("download missing DownloadEntryPath")
	}
}

func TestBookmarksPlistFixture(t *testing.T) {
	content, err := loadFixture(FixtureBookmarksPlist)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	var bookmarks map[string]interface{}
	_, err = plist.Unmarshal(content, &bookmarks)
	if err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	if bookmarks["WebBookmarkType"] != "WebBookmarkTypeList" {
		t.Errorf("expected WebBookmarkType 'WebBookmarkTypeList', got %v", bookmarks["WebBookmarkType"])
	}

	children, ok := bookmarks["Children"].([]interface{})
	if !ok {
		t.Fatal("expected Children to be an array")
	}

	// Should have BookmarksBar, BookmarksMenu, and ReadingList
	if len(children) < 3 {
		t.Errorf("expected at least 3 children (bar, menu, reading list), got %d", len(children))
	}
}

func TestCookiesBinaryFixture(t *testing.T) {
	content, err := loadFixture(FixtureCookiesBinary)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	// Verify magic header "cook"
	if len(content) < 4 {
		t.Fatal("cookies file too short")
	}
	if string(content[:4]) != "cook" {
		t.Errorf("expected magic 'cook', got %q", string(content[:4]))
	}
}

func TestExtensionsPlistFixture(t *testing.T) {
	content, err := loadFixture(FixtureExtensionsPlist)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	var extensions map[string]interface{}
	_, err = plist.Unmarshal(content, &extensions)
	if err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	installedExts, ok := extensions["Installed Extensions"].([]interface{})
	if !ok {
		t.Fatal("expected 'Installed Extensions' to be an array")
	}

	if len(installedExts) != 3 {
		t.Errorf("expected 3 extensions, got %d", len(installedExts))
	}
}

func TestLastSessionPlistFixture(t *testing.T) {
	content, err := loadFixture(FixtureLastSessionPlist)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	var session map[string]interface{}
	_, err = plist.Unmarshal(content, &session)
	if err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	windows, ok := session["SessionWindows"].([]interface{})
	if !ok {
		t.Fatal("expected SessionWindows to be an array")
	}

	if len(windows) != 2 {
		t.Errorf("expected 2 windows, got %d", len(windows))
	}
}

func TestLocalStorageFixture(t *testing.T) {
	ctx := context.Background()
	path := fixturePath(FixtureLocalStorageGitHub)
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify schema
	var tableName string
	err = db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='ItemTable'").Scan(&tableName)
	if err != nil {
		t.Errorf("ItemTable not found: %v", err)
	}

	// Verify data
	var itemCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ItemTable").Scan(&itemCount); err != nil {
		t.Fatalf("failed to count items: %v", err)
	}

	if itemCount != 3 {
		t.Errorf("expected 3 items in github local storage, got %d", itemCount)
	}
}

func TestTopSitesPlistFixture(t *testing.T) {
	content, err := loadFixture(FixtureTopSitesPlist)
	if err != nil {
		t.Fatalf("failed to load fixture: %v", err)
	}

	var topSites map[string]interface{}
	_, err = plist.Unmarshal(content, &topSites)
	if err != nil {
		t.Fatalf("failed to parse plist: %v", err)
	}

	sites, ok := topSites["TopSites"].([]interface{})
	if !ok {
		t.Fatal("expected TopSites to be an array")
	}

	if len(sites) != 4 {
		t.Errorf("expected 4 top sites, got %d", len(sites))
	}
}
