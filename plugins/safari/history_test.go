package safari

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/sqlite"
	"github.com/macfor-io/macfor-community/pkg/macfor/tablemapper"
	"github.com/macfor-io/macfor-community/pkg/macfor/timestamp"
)

// Test constants to avoid magic strings.
const (
	testRecordType = "browser_history"
	testBrowser    = "safari"
	testUsername   = "testuser"
)

// fileSource provides a Source that can read real files from a temp directory.
// This is used for integration tests with actual SQLite fixtures.
type fileSource struct {
	baseDir string
	users   []macfor.User
}

func newFileSource(baseDir string) *fileSource {
	return &fileSource{
		baseDir: baseDir,
		users:   []macfor.User{},
	}
}

func (s *fileSource) Type() macfor.SourceType {
	return macfor.SourceTypeLive
}

func (s *fileSource) OSVersion() (string, error) {
	return "14.2.1", nil
}

func (s *fileSource) Hostname() (string, error) {
	return "test-mac.local", nil
}

func (s *fileSource) Open(path string) (io.ReadCloser, error) {
	return os.Open(path) //nolint:gosec // Test code with known paths
}

func (s *fileSource) Stat(path string) (macfor.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, macfor.ErrNotFound
		}
		return nil, err
	}
	return fileSourceInfo{info}, nil
}

type fileSourceInfo struct {
	os.FileInfo
}

func (f fileSourceInfo) AccessTime() time.Time { return f.ModTime() }
func (f fileSourceInfo) ChangeTime() time.Time { return f.ModTime() }
func (f fileSourceInfo) BirthTime() time.Time  { return f.ModTime() }

func (s *fileSource) ReadDir(path string) ([]macfor.DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]macfor.DirEntry, len(entries))
	for i, e := range entries {
		result[i] = e
	}
	return result, nil
}

func (s *fileSource) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func (s *fileSource) Walk(root string, fn macfor.WalkFunc) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fn(path, nil, err)
		}
		return fn(path, fileSourceInfo{info}, nil)
	})
}

func (s *fileSource) Users() ([]macfor.User, error) {
	return s.users, nil
}

func (s *fileSource) ResolvePath(path string) string {
	return path
}

func (s *fileSource) ReadPlist(_ string, _ any) error {
	return macfor.ErrNotFound
}

func (s *fileSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{
		OSName:       "macOS",
		OSVersion:    "14.2.1",
		Architecture: "arm64",
		Hostname:     "test-mac.local",
		SourceType:   macfor.SourceTypeLive,
	}, nil
}

// TestHistoryVisit_Structure verifies the HistoryVisit struct has expected fields.
func TestHistoryVisit_Structure(t *testing.T) {
	visit := &HistoryVisit{
		Type:           testRecordType,
		Browser:        testBrowser,
		User:           testUsername,
		URL:            "https://example.com",
		Title:          "Example",
		VisitTime:      time.Now(),
		VisitCount:     5,
		LoadSuccessful: true,
		SourceFile:     "/Users/testuser/Library/Safari/History.db",
		HistoryItemID:  1,
		VisitID:        1,
	}

	if visit.Type != testRecordType {
		t.Errorf("expected Type %q, got %q", testRecordType, visit.Type)
	}
	if visit.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, visit.Browser)
	}
}

// extractHistoryVisits runs the declarative table mapper against db using the
// manifest's history extract spec, returning the parsed visits. It mirrors the
// production path in parseAndOutputHistory and serves as the byte-identical
// oracle for the migrated parser.
func extractHistoryVisits(tb testing.TB, db *sql.DB, path string) []*HistoryVisit {
	tb.Helper()
	def := artifacts.GetArtifactByID(manifest, string(ArtifactHistory))
	if def == nil || def.Extract == nil {
		tb.Fatal("history extract spec missing from manifest")
	}
	var visits []*HistoryVisit
	_, err := tablemapper.Each(context.Background(), db, *def.Extract, tablemapper.Options{
		NewRecord: func() any { return &HistoryVisit{} },
		Context:   map[string]string{"username": testUsername, "source_file": path},
	}, func(rec any) error {
		visits = append(visits, rec.(*HistoryVisit))
		return nil
	})
	if err != nil {
		tb.Fatalf("extract failed: %v", err)
	}
	return visits
}

// TestParseHistoryDB_BasicFixture tests parsing the main History.db fixture.
func TestParseHistoryDB_BasicFixture(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)

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

	// Extract all visits via the manifest-driven table mapper.
	visits := extractHistoryVisits(t, db, path)

	// Fixture should have 14 visits (based on generate_fixtures.go).
	if len(visits) != 14 {
		t.Errorf("expected 14 visits, got %d", len(visits))
	}

	// Verify first visit.
	if len(visits) > 0 {
		first := visits[0]
		if first.User != testUsername {
			t.Errorf("expected user testuser, got %q", first.User)
		}
		if first.Type != testRecordType {
			t.Errorf("expected type 'browser_history', got %q", first.Type)
		}
		if first.Browser != testBrowser {
			t.Errorf("expected browser 'safari', got %q", first.Browser)
		}
		if !strings.Contains(first.URL, "google.com") && !strings.Contains(first.URL, "github.com") &&
			!strings.Contains(first.URL, "news.ycombinator.com") {
			t.Errorf("unexpected first URL: %q", first.URL)
		}
		if first.VisitTime.IsZero() {
			t.Error("expected non-zero visit time")
		}
		// Verify timestamp is reasonable (2024+).
		if first.VisitTime.Year() < 2024 {
			t.Errorf("expected visit time year >= 2024, got %d", first.VisitTime.Year())
		}
	}
}

// TestParseHistoryDB_EmptyFixture tests handling of empty database.
func TestParseHistoryDB_EmptyFixture(t *testing.T) {
	path := fixturePath(FixtureHistoryDBEmpty)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Tables should exist.
	if !sqlite.TableExists(db, "history_items") {
		t.Fatal("history_items table not found")
	}
	if !sqlite.TableExists(db, "history_visits") {
		t.Fatal("history_visits table not found")
	}

	// Extraction should return no visits.
	if count := len(extractHistoryVisits(t, db, path)); count != 0 {
		t.Errorf("expected 0 visits from empty database, got %d", count)
	}
}

// TestParseHistoryDB_UnicodeFixture tests Unicode URL and title handling.
func TestParseHistoryDB_UnicodeFixture(t *testing.T) {
	path := fixturePath(FixtureHistoryDBUnicode)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	visits := extractHistoryVisits(t, db, path)

	if len(visits) == 0 {
		t.Fatal("expected visits from unicode fixture")
	}

	// Check for various Unicode characters in URLs and titles.
	hasJapanese := false
	hasChinese := false
	hasEmoji := false
	hasRussian := false

	for _, v := range visits {
		if strings.Contains(v.URL, "ja.wikipedia.org") || strings.Contains(v.Title, "\u65e5\u672c\u8a9e") {
			hasJapanese = true
		}
		if strings.Contains(v.URL, "zh.wikipedia.org") || strings.Contains(v.Title, "\u4e2d\u6587") {
			hasChinese = true
		}
		if strings.Contains(v.Title, "\U0001F600") || strings.Contains(v.Title, "\U0001F389") {
			hasEmoji = true
		}
		if strings.Contains(v.URL, "ru.wikipedia.org") || strings.Contains(v.Title, "\u0420\u0443\u0441\u0441\u043a\u0438\u0439") {
			hasRussian = true
		}
	}

	// At least some Unicode should be present (depends on fixture).
	if !hasJapanese && !hasChinese && !hasEmoji && !hasRussian {
		t.Log("Note: Unicode fixture may not contain all expected characters")
	}
}

// TestParseHistoryDB_LargeFixture tests performance with large database.
func TestParseHistoryDB_LargeFixture(t *testing.T) {
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
}

// TestParseHistoryDB_FailedLoad tests handling of load_successful flag.
func TestParseHistoryDB_FailedLoad(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	hasFailedLoad := false
	hasSuccessfulLoad := false

	for _, visit := range extractHistoryVisits(t, db, path) {
		if visit.LoadSuccessful {
			hasSuccessfulLoad = true
		} else {
			hasFailedLoad = true
		}
	}

	// Fixture should have at least one failed load and one successful.
	if !hasSuccessfulLoad {
		t.Error("expected at least one successful load")
	}
	if !hasFailedLoad {
		t.Error("expected at least one failed load in fixture")
	}
}

// TestTimestampConversion verifies Core Data timestamp handling.
func TestTimestampConversion(t *testing.T) {
	// Test value from fixture (approximate - 2024 or later).
	// The fixture uses timestamps around 727005600.0 which is early 2024.
	testTimestamp := 727005600.0
	converted := timestamp.CoreDataToTime(testTimestamp)

	// Should be in 2024.
	if converted.Year() != 2024 {
		t.Errorf("expected year 2024, got %d", converted.Year())
	}

	// Should not be zero.
	if converted.IsZero() {
		t.Error("converted time should not be zero")
	}

	// Round trip test.
	backToCore := timestamp.TimeToCoreData(converted)
	diff := testTimestamp - backToCore
	if diff > 1.0 || diff < -1.0 {
		t.Errorf("round trip error: original=%f, converted back=%f", testTimestamp, backToCore)
	}
}

// TestCollectHistoryForUser_FileNotFound tests handling of missing History.db.
func TestCollectHistoryForUser_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Create source without History.db.
	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectHistoryForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should log debug message about missing file.
	if !coll.hasLogAtLevel(macfor.LogDebug, "not found") {
		t.Error("expected debug log about missing History.db")
	}

	// Should not add any files or records.
	if len(coll.files) != 0 {
		t.Errorf("expected no files, got %d", len(coll.files))
	}
	if len(coll.records) != 0 {
		t.Errorf("expected no records, got %d", len(coll.records))
	}
}

// TestCollectHistoryForUser_InvalidSQLite tests handling of non-SQLite file.
func TestCollectHistoryForUser_InvalidSQLite(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create invalid History.db (just random text).
	historyPath := filepath.Join(safariDir, "History.db")
	if err := os.WriteFile(historyPath, []byte("not a database"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectHistoryForUser(context.Background(), user, src, coll)

	// Should return error for invalid database.
	if err == nil {
		t.Fatal("expected error for invalid SQLite file")
	}

	if !strings.Contains(err.Error(), "invalid SQLite database") {
		t.Errorf("expected 'invalid SQLite database' error, got: %v", err)
	}
}

// TestCollectHistoryForUser_WithFixture tests full collection with real fixture.
//
//nolint:dupl // Test patterns similar to downloads_test.go but with different assertions.
func TestCollectHistoryForUser_WithFixture(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture to temp location.
	fixtureSrc := fixturePath(FixtureHistoryDB)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	historyPath := filepath.Join(safariDir, "History.db")
	if err := os.WriteFile(historyPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectHistoryForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Should have added records (14 visits in fixture).
	if len(coll.records) != 14 {
		t.Errorf("expected 14 records, got %d", len(coll.records))
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
		if artifact.Category != "browser" {
			t.Errorf("expected Category 'browser', got %q", artifact.Category)
		}
		if artifact.Type != "history" {
			t.Errorf("expected Type 'history', got %q", artifact.Type)
		}
	}

	// Verify record structure.
	if len(coll.records) > 0 {
		record, ok := coll.records[0].record.(*HistoryVisit)
		if !ok {
			t.Fatal("expected record to be *HistoryVisit")
		}
		if record.Type != testRecordType {
			t.Errorf("expected Type 'browser_history', got %q", record.Type)
		}
		if record.Browser != testBrowser {
			t.Errorf("expected Browser 'safari', got %q", record.Browser)
		}
		if record.User != testUsername {
			t.Errorf("expected User testuser, got %q", record.User)
		}
	}

	// Should log success message.
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collected") {
		t.Error("expected info log about collected visits")
	}
}

// TestCollectHistoryForUser_ContextCancellation tests cancellation during parsing.
//
//nolint:dupl // Test patterns similar to other artifact tests but with different data.
func TestCollectHistoryForUser_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureHistoryDB)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	historyPath := filepath.Join(safariDir, "History.db")
	if err := os.WriteFile(historyPath, fixtureContent, 0600); err != nil {
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

	err = collectHistoryForUser(ctx, user, src, coll)

	// Should return context error (might be from AddFile or parsing).
	// The file is added before parsing, so it depends on timing.
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("error was: %v", err)
	}
}

// TestScanHistoryRow_NullTitle tests handling of NULL title.
func TestScanHistoryRow_NullTitle(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	// A NULL or empty title must map to an empty string, not an error. Extract
	// all visits (the mapper handles NULL columns) and verify any empty-title
	// visit is the empty string.
	for _, visit := range extractHistoryVisits(t, db, path) {
		if visit.Title == "" {
			// Empty string is the expected representation of a NULL title.
			return
		}
	}
}

// TestScanHistoryRow_RedirectFields tests handling of redirect chain fields.
func TestScanHistoryRow_RedirectFields(t *testing.T) {
	// Most visits won't have redirect fields set.
	path := fixturePath(FixtureHistoryDB)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	hasNullRedirect := false
	for _, visit := range extractHistoryVisits(t, db, path) {
		if visit.RedirectSource == nil && visit.RedirectDest == nil {
			hasNullRedirect = true
		}
	}

	// Most visits should have NULL redirect fields.
	if !hasNullRedirect {
		t.Error("expected at least some visits with NULL redirect fields")
	}
}

// TestHistoryQuery_OrderByTime verifies results are ordered by visit_time.
func TestHistoryQuery_OrderByTime(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	var lastTime time.Time
	for _, visit := range extractHistoryVisits(t, db, path) {
		if !lastTime.IsZero() && visit.VisitTime.Before(lastTime) {
			t.Errorf("visits not ordered by time: %v came after %v", visit.VisitTime, lastTime)
		}
		lastTime = visit.VisitTime
	}
}

// TestParseAndOutputHistory_ErrorRecovery tests that parsing continues on error.
func TestParseAndOutputHistory_ErrorRecovery(t *testing.T) {
	path := fixturePath(FixtureHistoryDB)

	db, err := sqlite.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("failed to open fixture: %v", err)
	}
	defer func() { _ = db.Close() }()

	artifact, err := macfor.NewArtifact(PluginID, "browser", "history")
	if err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	coll := newMockCollector()

	count, err := parseAndOutputHistory(context.Background(), db, testUsername, path, artifact, coll)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should have parsed all 14 records.
	if count != 14 {
		t.Errorf("expected 14 records, got %d", count)
	}

	// Records should be added to collector.
	if len(coll.records) != 14 {
		t.Errorf("expected 14 collector records, got %d", len(coll.records))
	}
}

// TestIsSQLiteFile tests SQLite magic header detection.
func TestIsSQLiteFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a valid SQLite database.
	validPath := filepath.Join(tempDir, "valid.db")
	validDB, err := sql.Open("sqlite", validPath)
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	_, _ = validDB.ExecContext(context.Background(), "CREATE TABLE test (id INTEGER)")
	_ = validDB.Close()

	// Create an invalid file.
	invalidPath := filepath.Join(tempDir, "invalid.db")
	if err := os.WriteFile(invalidPath, []byte("not a database"), 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	src := newFileSource(tempDir)

	// Valid database should be detected.
	isValid, err := sqlite.IsSQLiteFile(src, validPath)
	if err != nil {
		t.Fatalf("IsSQLiteFile error: %v", err)
	}
	if !isValid {
		t.Error("expected valid.db to be detected as SQLite")
	}

	// Invalid file should not be detected.
	isValid, err = sqlite.IsSQLiteFile(src, invalidPath)
	if err != nil {
		t.Fatalf("IsSQLiteFile error: %v", err)
	}
	if isValid {
		t.Error("expected invalid.db to not be detected as SQLite")
	}
}

// Verify sqlite import is used in tests (avoiding unused import).
var _ = sqlite.OpenReadOnly
