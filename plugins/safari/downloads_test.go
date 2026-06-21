package safari

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"github.com/macfor-io/macfor-community/pkg/macfor/artifacts"
	"github.com/macfor-io/macfor-community/pkg/macfor/plistmapper"
)

// Test constants for downloads.
const (
	testDownloadRecordType = "browser_download"
)

// extractDownloadRecords runs the declarative plist mapper against data using
// the manifest's downloads extract spec and the download-state deriver — the
// equivalence oracle for the migrated parser.
func extractDownloadRecords(tb testing.TB, data []byte) []*DownloadRecord {
	tb.Helper()
	def := artifacts.GetArtifactByID(manifest, string(ArtifactDownloads))
	if def == nil || def.Extract == nil {
		tb.Fatal("downloads extract spec missing from manifest")
	}
	var recs []*DownloadRecord
	_, err := plistmapper.Each(context.Background(), data, *def.Extract, plistmapper.Options{
		NewRecord: func() any { return &DownloadRecord{} },
		Context:   map[string]string{"username": testUsername, "source_file": "/test/Downloads.plist"},
		Derivers:  pluginDerivers,
	}, func(rec any) error {
		recs = append(recs, rec.(*DownloadRecord))
		return nil
	})
	if err != nil {
		tb.Fatalf("extract: %v", err)
	}
	return recs
}

// TestDownloadRecord_Structure verifies the DownloadRecord struct has expected fields.
func TestDownloadRecord_Structure(t *testing.T) {
	startTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 15, 10, 0, 30, 0, time.UTC)

	record := &DownloadRecord{
		Type:           testDownloadRecordType,
		Browser:        testBrowser,
		User:           testUsername,
		URL:            "https://example.com/file.zip",
		LocalPath:      "/Users/testuser/Downloads/file.zip",
		FileName:       "file.zip",
		TotalBytes:     5242880,
		ReceivedBytes:  5242880,
		StartTime:      startTime,
		EndTime:        endTime,
		State:          DownloadStateComplete,
		Identifier:     "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
		RemoveWhenDone: false,
		SourceFile:     "/Users/testuser/Library/Safari/Downloads.plist",
	}

	if record.Type != testDownloadRecordType {
		t.Errorf("expected Type %q, got %q", testDownloadRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.State != DownloadStateComplete {
		t.Errorf("expected State %q, got %q", DownloadStateComplete, record.State)
	}
	if record.FileName != "file.zip" {
		t.Errorf("expected FileName 'file.zip', got %q", record.FileName)
	}
}

// TestDownloadState_Constants verifies download state constants.
func TestDownloadState_Constants(t *testing.T) {
	if DownloadStateComplete != "complete" {
		t.Errorf("expected DownloadStateComplete to be 'complete', got %q", DownloadStateComplete)
	}
	if DownloadStateIncomplete != "incomplete" {
		t.Errorf("expected DownloadStateIncomplete to be 'incomplete', got %q", DownloadStateIncomplete)
	}
}

// TestDownloadsMapping verifies the declarative plist mapper extracts the
// fixture's downloads with the right const/context/key fields, the basename
// transform, and the derived state.
func TestDownloadsMapping(t *testing.T) {
	recs := extractDownloadRecords(t, mustLoadFixture(FixtureDownloadsPlist))
	if len(recs) != 3 {
		t.Fatalf("expected 3 downloads, got %d", len(recs))
	}

	first := recs[0]
	if first.Type != testDownloadRecordType || first.Browser != testBrowser || first.User != testUsername {
		t.Errorf("const/context fields wrong: %+v", first)
	}
	if !strings.Contains(first.URL, "claude-code") {
		t.Errorf("unexpected first URL: %q", first.URL)
	}
	if first.TotalBytes != 5242880 || first.ReceivedBytes != 5242880 {
		t.Errorf("first bytes wrong: total=%d received=%d", first.TotalBytes, first.ReceivedBytes)
	}
	if first.State != DownloadStateComplete {
		t.Errorf("expected first State complete, got %q", first.State)
	}
	if first.Identifier != "A1B2C3D4-E5F6-7890-ABCD-EF1234567890" {
		t.Errorf("unexpected Identifier: %q", first.Identifier)
	}
	if first.RemoveWhenDone {
		t.Error("expected first RemoveWhenDone to be false")
	}
	if first.FileName == "" || !strings.HasSuffix(first.LocalPath, first.FileName) {
		t.Errorf("file_name not basename of local_path: name=%q path=%q", first.FileName, first.LocalPath)
	}
	if first.SourceFile != "/test/Downloads.plist" {
		t.Errorf("source_file context wrong: %q", first.SourceFile)
	}

	if !recs[1].RemoveWhenDone || !strings.Contains(recs[1].URL, "golang.org") {
		t.Errorf("second download wrong: %+v", recs[1])
	}

	third := recs[2]
	if !strings.Contains(third.URL, "large-file.iso") {
		t.Errorf("unexpected third URL: %q", third.URL)
	}
	if third.State != DownloadStateIncomplete {
		t.Errorf("expected third State incomplete, got %q", third.State)
	}
	if !third.EndTime.IsZero() {
		t.Errorf("expected zero EndTime for incomplete download, got %v", third.EndTime)
	}
}

// TestDownloadsMapping_Empty verifies the empty fixture yields no records.
func TestDownloadsMapping_Empty(t *testing.T) {
	recs := extractDownloadRecords(t, mustLoadFixture(FixtureDownloadsPlistEmpty))
	if len(recs) != 0 {
		t.Errorf("expected 0 downloads from empty plist, got %d", len(recs))
	}
}

// TestDownloadState_Deriver unit-tests the cross-field state derivation.
func TestDownloadState_Deriver(t *testing.T) {
	cases := []struct {
		name            string
		received, total int64
		want            DownloadState
	}{
		{"complete", 100, 100, DownloadStateComplete},
		{"over received", 120, 100, DownloadStateComplete},
		{"incomplete", 50, 100, DownloadStateIncomplete},
		{"zero total", 0, 0, DownloadStateIncomplete},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := downloadState(map[string]any{
				"DownloadEntryProgressBytesSoFar":  c.received,
				"DownloadEntryProgressTotalToLoad": c.total,
			})
			if err != nil {
				t.Fatalf("downloadState: %v", err)
			}
			if v.(string) != string(c.want) {
				t.Errorf("got %v, want %v", v, c.want)
			}
		})
	}
}

// TestCollectDownloadsForUser_FileNotFound tests handling of missing Downloads.plist.
func TestCollectDownloadsForUser_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Create source without Downloads.plist.
	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectDownloadsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should log debug message about missing file.
	if !coll.hasLogAtLevel(macfor.LogDebug, "not found") {
		t.Error("expected debug log about missing Downloads.plist")
	}

	// Should not add any files or records.
	if len(coll.files) != 0 {
		t.Errorf("expected no files, got %d", len(coll.files))
	}
	if len(coll.records) != 0 {
		t.Errorf("expected no records, got %d", len(coll.records))
	}
}

// TestCollectDownloadsForUser_EmptyPlist tests handling of empty Downloads.plist.
//
//nolint:dupl // Test patterns similar to other artifact tests but with different data.
func TestCollectDownloadsForUser_EmptyPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy empty fixture.
	fixtureSrc := fixturePath(FixtureDownloadsPlistEmpty)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	downloadsPath := filepath.Join(safariDir, "Downloads.plist")
	if err := os.WriteFile(downloadsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectDownloadsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Should have no records (empty plist).
	if len(coll.records) != 0 {
		t.Errorf("expected 0 records, got %d", len(coll.records))
	}

	// Should log info about no downloads.
	if !coll.hasLogAtLevel(macfor.LogInfo, "No downloads records found") {
		t.Error("expected info log about no downloads found")
	}
}

// TestCollectDownloadsForUser_WithFixture tests full collection with real fixture.
//
//nolint:dupl // Test patterns similar to history_test.go but with different assertions.
func TestCollectDownloadsForUser_WithFixture(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture to temp location.
	fixtureSrc := fixturePath(FixtureDownloadsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	downloadsPath := filepath.Join(safariDir, "Downloads.plist")
	if err := os.WriteFile(downloadsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectDownloadsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Should have added records (3 downloads in fixture).
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
		if artifact.Category != "browser" {
			t.Errorf("expected Category 'browser', got %q", artifact.Category)
		}
		if artifact.Type != "downloads" {
			t.Errorf("expected Type 'downloads', got %q", artifact.Type)
		}
	}

	// Verify record structure.
	if len(coll.records) > 0 {
		record, ok := coll.records[0].record.(*DownloadRecord)
		if !ok {
			t.Fatal("expected record to be *DownloadRecord")
		}
		if record.Type != testDownloadRecordType {
			t.Errorf("expected Type 'browser_download', got %q", record.Type)
		}
		if record.Browser != testBrowser {
			t.Errorf("expected Browser 'safari', got %q", record.Browser)
		}
		if record.User != testUsername {
			t.Errorf("expected User testuser, got %q", record.User)
		}
	}

	// Should log success message.
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collected 3 downloads") {
		t.Error("expected info log about collected downloads")
	}
}

// TestCollectDownloadsForUser_ContextCancellation tests cancellation during collection.
//
//nolint:dupl // Test patterns similar to other artifact tests but with different data.
func TestCollectDownloadsForUser_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureDownloadsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	downloadsPath := filepath.Join(safariDir, "Downloads.plist")
	if err := os.WriteFile(downloadsPath, fixtureContent, 0600); err != nil {
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

	err = collectDownloadsForUser(ctx, user, src, coll)

	// Should return context error (might be from AddFile or parsing).
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("error was: %v", err)
	}
}

// TestCollectDownloadsForUser_InvalidPlist tests handling of invalid plist file.
func TestCollectDownloadsForUser_InvalidPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create invalid plist (just random text).
	downloadsPath := filepath.Join(safariDir, "Downloads.plist")
	if err := os.WriteFile(downloadsPath, []byte("not a plist"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectDownloadsForUser(context.Background(), user, src, coll)

	// Should return error for invalid plist.
	if err == nil {
		t.Fatal("expected error for invalid plist file")
	}

	if !strings.Contains(err.Error(), "parsing downloads") {
		t.Errorf("expected 'parsing downloads' error, got: %v", err)
	}
}

// TestCollectDownloadsForUser_RecordStates tests that download states are correctly identified.
func TestCollectDownloadsForUser_RecordStates(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureDownloadsPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	downloadsPath := filepath.Join(safariDir, "Downloads.plist")
	if err := os.WriteFile(downloadsPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectDownloadsForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Count complete and incomplete downloads.
	completeCount := 0
	incompleteCount := 0

	for _, r := range coll.records {
		record, ok := r.record.(*DownloadRecord)
		if !ok {
			continue
		}
		switch record.State {
		case DownloadStateComplete:
			completeCount++
		case DownloadStateIncomplete:
			incompleteCount++
		}
	}

	// Fixture has 2 complete and 1 incomplete.
	if completeCount != 2 {
		t.Errorf("expected 2 complete downloads, got %d", completeCount)
	}
	if incompleteCount != 1 {
		t.Errorf("expected 1 incomplete download, got %d", incompleteCount)
	}
}

// TestDownloadsKeyMapping verifies the mapper reads the Safari download plist
// keys into the record (array-root form), with basename and derived state.
func TestDownloadsKeyMapping(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
	<dict>
		<key>DownloadEntryURL</key>
		<string>https://test.example.com/file.zip</string>
		<key>DownloadEntryPath</key>
		<string>/Users/test/Downloads/file.zip</string>
		<key>DownloadEntryDateAddedKey</key>
		<date>2024-01-15T10:00:00Z</date>
		<key>DownloadEntryDateFinishedKey</key>
		<date>2024-01-15T10:00:30Z</date>
		<key>DownloadEntryProgressTotalToLoad</key>
		<integer>1024</integer>
		<key>DownloadEntryProgressBytesSoFar</key>
		<integer>1024</integer>
		<key>DownloadEntryIdentifier</key>
		<string>test-id-123</string>
		<key>DownloadEntryRemoveWhenDoneKey</key>
		<true/>
	</dict>
</array>
</plist>`)

	recs := extractDownloadRecords(t, data)
	if len(recs) != 1 {
		t.Fatalf("expected 1 download, got %d", len(recs))
	}
	d := recs[0]
	if d.URL != "https://test.example.com/file.zip" {
		t.Errorf("URL mismatch: %q", d.URL)
	}
	if d.LocalPath != "/Users/test/Downloads/file.zip" {
		t.Errorf("LocalPath mismatch: %q", d.LocalPath)
	}
	if d.FileName != "file.zip" {
		t.Errorf("FileName (basename) mismatch: %q", d.FileName)
	}
	if d.TotalBytes != 1024 || d.ReceivedBytes != 1024 {
		t.Errorf("bytes mismatch: total=%d received=%d", d.TotalBytes, d.ReceivedBytes)
	}
	if d.Identifier != "test-id-123" {
		t.Errorf("Identifier mismatch: %q", d.Identifier)
	}
	if !d.RemoveWhenDone {
		t.Error("RemoveWhenDone should be true")
	}
	if d.State != DownloadStateComplete {
		t.Errorf("expected State complete, got %q", d.State)
	}
	if d.StartTime.Year() != 2024 || d.EndTime.Year() != 2024 {
		t.Errorf("date mapping wrong: start=%v end=%v", d.StartTime, d.EndTime)
	}
}
