package safari

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	"howett.net/plist"
)

// Test constants for session.
const (
	testSessionWindowRecordType = "browser_session_window"
	testSessionTabRecordType    = "browser_session_tab"
)

// TestSessionWindowRecord_Structure verifies the SessionWindowRecord struct has expected fields.
func TestSessionWindowRecord_Structure(t *testing.T) {
	record := &SessionWindowRecord{
		Type:             testSessionWindowRecordType,
		Browser:          testBrowser,
		User:             testUsername,
		WindowIndex:      0,
		WindowUUID:       "WIN-UUID-0001-0000-000000000000",
		IsPrivate:        false,
		SelectedTabIndex: 1,
		TabCount:         3,
		SourceFile:       "/Users/testuser/Library/Safari/LastSession.plist",
	}

	if record.Type != testSessionWindowRecordType {
		t.Errorf("expected Type %q, got %q", testSessionWindowRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.WindowIndex != 0 {
		t.Errorf("expected WindowIndex 0, got %d", record.WindowIndex)
	}
	if record.TabCount != 3 {
		t.Errorf("expected TabCount 3, got %d", record.TabCount)
	}
}

// TestSessionTabRecord_Structure verifies the SessionTabRecord struct has expected fields.
func TestSessionTabRecord_Structure(t *testing.T) {
	lastVisit := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	record := &SessionTabRecord{
		Type:           testSessionTabRecordType,
		Browser:        testBrowser,
		User:           testUsername,
		WindowIndex:    0,
		TabIndex:       1,
		TabUUID:        "TAB-UUID-0001-0002-000000000000",
		URL:            "https://github.com",
		Title:          "GitHub",
		LastVisitTime:  lastVisit,
		IsAppInitiated: true,
		SourceFile:     "/Users/testuser/Library/Safari/LastSession.plist",
	}

	if record.Type != testSessionTabRecordType {
		t.Errorf("expected Type %q, got %q", testSessionTabRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.WindowIndex != 0 {
		t.Errorf("expected WindowIndex 0, got %d", record.WindowIndex)
	}
	if record.TabIndex != 1 {
		t.Errorf("expected TabIndex 1, got %d", record.TabIndex)
	}
	if record.URL != "https://github.com" {
		t.Errorf("expected URL 'https://github.com', got %q", record.URL)
	}
}

// TestParseLastSessionPlist_BasicFixture tests parsing the LastSession.plist fixture.
func TestParseLastSessionPlist_BasicFixture(t *testing.T) {
	plistData := mustLoadFixture(FixtureLastSessionPlist)

	var session sessionData
	decoder := plist.NewDecoder(bytes.NewReader(plistData))
	if err := decoder.Decode(&session); err != nil {
		t.Fatalf("failed to decode plist: %v", err)
	}

	// Fixture should have 2 windows.
	if len(session.SessionWindows) != 2 {
		t.Errorf("expected 2 windows, got %d", len(session.SessionWindows))
	}

	// Verify first window (3 tabs: GitHub, Hacker News, Stack Overflow).
	if len(session.SessionWindows) > 0 {
		first := session.SessionWindows[0]
		if first.WindowUUID == "" {
			t.Error("expected WindowUUID to be set")
		}
		if first.IsPrivate {
			t.Error("expected first window to not be private")
		}
		if first.SelectedTabIndex != 1 {
			t.Errorf("expected SelectedTabIndex 1, got %d", first.SelectedTabIndex)
		}
		if len(first.TabStates) != 3 {
			t.Errorf("expected 3 tabs in first window, got %d", len(first.TabStates))
		}

		// Verify first tab in first window.
		if len(first.TabStates) > 0 {
			tab := first.TabStates[0]
			if tab.TabURL != "https://github.com" {
				t.Errorf("expected first tab URL 'https://github.com', got %q", tab.TabURL)
			}
			if tab.TabTitle != "GitHub" {
				t.Errorf("expected first tab title 'GitHub', got %q", tab.TabTitle)
			}
		}
	}

	// Verify second window (1 tab: Go Packages).
	if len(session.SessionWindows) > 1 {
		second := session.SessionWindows[1]
		if len(second.TabStates) != 1 {
			t.Errorf("expected 1 tab in second window, got %d", len(second.TabStates))
		}
		if second.SelectedTabIndex != 0 {
			t.Errorf("expected SelectedTabIndex 0, got %d", second.SelectedTabIndex)
		}
	}
}

// TestConvertSessionWindow tests conversion of session window to record.
func TestConvertSessionWindow(t *testing.T) {
	window := sessionWindow{
		WindowUUID:       "TEST-WIN-UUID",
		IsPrivate:        true,
		SelectedTabIndex: 2,
		TabStates: []sessionTab{
			{TabUUID: "TAB-1"},
			{TabUUID: "TAB-2"},
			{TabUUID: "TAB-3"},
		},
	}

	record := convertSessionWindow(window, 0, testUsername, "/path/to/LastSession.plist")

	if record.Type != testSessionWindowRecordType {
		t.Errorf("expected Type %q, got %q", testSessionWindowRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.User != testUsername {
		t.Errorf("expected User %q, got %q", testUsername, record.User)
	}
	if record.WindowIndex != 0 {
		t.Errorf("expected WindowIndex 0, got %d", record.WindowIndex)
	}
	if record.WindowUUID != "TEST-WIN-UUID" {
		t.Errorf("expected WindowUUID 'TEST-WIN-UUID', got %q", record.WindowUUID)
	}
	if !record.IsPrivate {
		t.Error("expected IsPrivate to be true")
	}
	if record.SelectedTabIndex != 2 {
		t.Errorf("expected SelectedTabIndex 2, got %d", record.SelectedTabIndex)
	}
	if record.TabCount != 3 {
		t.Errorf("expected TabCount 3, got %d", record.TabCount)
	}
}

// TestConvertSessionTab tests conversion of session tab to record.
func TestConvertSessionTab(t *testing.T) {
	lastVisit := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tab := sessionTab{
		TabUUID:        "TEST-TAB-UUID",
		TabURL:         "https://example.com",
		TabTitle:       "Example Page",
		LastVisitTime:  lastVisit,
		IsAppInitiated: true,
	}

	record := convertSessionTab(tab, 1, 2, testUsername, "/path/to/LastSession.plist")

	if record.Type != testSessionTabRecordType {
		t.Errorf("expected Type %q, got %q", testSessionTabRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.User != testUsername {
		t.Errorf("expected User %q, got %q", testUsername, record.User)
	}
	if record.WindowIndex != 1 {
		t.Errorf("expected WindowIndex 1, got %d", record.WindowIndex)
	}
	if record.TabIndex != 2 {
		t.Errorf("expected TabIndex 2, got %d", record.TabIndex)
	}
	if record.TabUUID != "TEST-TAB-UUID" {
		t.Errorf("expected TabUUID 'TEST-TAB-UUID', got %q", record.TabUUID)
	}
	if record.URL != "https://example.com" {
		t.Errorf("expected URL 'https://example.com', got %q", record.URL)
	}
	if record.Title != "Example Page" {
		t.Errorf("expected Title 'Example Page', got %q", record.Title)
	}
	if !record.LastVisitTime.Equal(lastVisit) {
		t.Errorf("expected LastVisitTime %v, got %v", lastVisit, record.LastVisitTime)
	}
	if !record.IsAppInitiated {
		t.Error("expected IsAppInitiated to be true")
	}
}

// TestCollectLastSessionForUser_FileNotFound tests handling of missing LastSession.plist.
func TestCollectLastSessionForUser_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Create source without LastSession.plist.
	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectLastSessionForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should log debug message about missing file.
	if !coll.hasLogAtLevel(macfor.LogDebug, "not found") {
		t.Error("expected debug log about missing LastSession.plist")
	}

	// Should not add any files or records.
	if len(coll.files) != 0 {
		t.Errorf("expected no files, got %d", len(coll.files))
	}
	if len(coll.records) != 0 {
		t.Errorf("expected no records, got %d", len(coll.records))
	}
}

// TestCollectLastSessionForUser_WithFixture tests full collection with real fixture.
func TestCollectLastSessionForUser_WithFixture(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture to temp location.
	fixtureSrc := fixturePath(FixtureLastSessionPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	if err := os.WriteFile(sessionPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectLastSessionForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Verify artifact metadata.
	if len(coll.files) > 0 {
		artifact := coll.files[0].artifact
		if artifact.PluginID != PluginID {
			t.Errorf("expected PluginID %q, got %q", PluginID, artifact.PluginID)
		}
		if artifact.Username != testUsername {
			t.Errorf("expected Username %s, got %q", testUsername, artifact.Username)
		}
		if artifact.Category != "browser" {
			t.Errorf("expected Category 'browser', got %q", artifact.Category)
		}
		if artifact.Type != "session" {
			t.Errorf("expected Type 'session', got %q", artifact.Type)
		}
	}

	// Count window and tab records.
	// Fixture has: 2 windows + 4 tabs = 6 records.
	windowCount := 0
	tabCount := 0

	for _, r := range coll.records {
		switch r.record.(type) {
		case *SessionWindowRecord:
			windowCount++
		case *SessionTabRecord:
			tabCount++
		}
	}

	if windowCount != 2 {
		t.Errorf("expected 2 window records, got %d", windowCount)
	}
	if tabCount != 4 {
		t.Errorf("expected 4 tab records, got %d", tabCount)
	}

	// Should log success message.
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collected 2 windows and 4 tabs") {
		t.Error("expected info log about collected windows and tabs")
	}
}

// TestCollectLastSessionForUser_WindowsAndTabs tests that windows and tabs are correctly associated.
func TestCollectLastSessionForUser_WindowsAndTabs(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureLastSessionPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	if err := os.WriteFile(sessionPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectLastSessionForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Verify window 0 has 3 tabs (GitHub, Hacker News, Stack Overflow).
	window0Tabs := 0
	for _, r := range coll.records {
		if tab, ok := r.record.(*SessionTabRecord); ok {
			if tab.WindowIndex == 0 {
				window0Tabs++
			}
		}
	}
	if window0Tabs != 3 {
		t.Errorf("expected 3 tabs in window 0, got %d", window0Tabs)
	}

	// Verify window 1 has 1 tab (Go Packages).
	window1Tabs := 0
	for _, r := range coll.records {
		if tab, ok := r.record.(*SessionTabRecord); ok {
			if tab.WindowIndex == 1 {
				window1Tabs++
			}
		}
	}
	if window1Tabs != 1 {
		t.Errorf("expected 1 tab in window 1, got %d", window1Tabs)
	}

	// Verify tab content matches fixture.
	var foundGitHub, foundGoPackages bool
	for _, r := range coll.records {
		if tab, ok := r.record.(*SessionTabRecord); ok {
			if tab.URL == "https://github.com" && tab.Title == "GitHub" {
				foundGitHub = true
				if tab.WindowIndex != 0 || tab.TabIndex != 0 {
					t.Errorf("GitHub tab has wrong indices: window=%d, tab=%d", tab.WindowIndex, tab.TabIndex)
				}
			}
			if tab.URL == "https://pkg.go.dev" && tab.Title == "Go Packages" {
				foundGoPackages = true
				if tab.WindowIndex != 1 || tab.TabIndex != 0 {
					t.Errorf("Go Packages tab has wrong indices: window=%d, tab=%d", tab.WindowIndex, tab.TabIndex)
				}
			}
		}
	}

	if !foundGitHub {
		t.Error("expected to find GitHub tab")
	}
	if !foundGoPackages {
		t.Error("expected to find Go Packages tab")
	}
}

// TestCollectLastSessionForUser_ContextCancellation tests cancellation during collection.
//
//nolint:dupl // Test patterns similar to other artifact tests but with different data.
func TestCollectLastSessionForUser_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureLastSessionPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	if err := os.WriteFile(sessionPath, fixtureContent, 0600); err != nil {
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

	err = collectLastSessionForUser(ctx, user, src, coll)

	// Should return context error (might be from AddFile or parsing).
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("error was: %v", err)
	}
}

// TestCollectLastSessionForUser_InvalidPlist tests handling of invalid plist file.
func TestCollectLastSessionForUser_InvalidPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create invalid plist (just random text).
	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	if err := os.WriteFile(sessionPath, []byte("not a plist"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectLastSessionForUser(context.Background(), user, src, coll)

	// Should return error for invalid plist.
	if err == nil {
		t.Fatal("expected error for invalid plist file")
	}

	if !strings.Contains(err.Error(), "parsing LastSession.plist") {
		t.Errorf("expected 'parsing LastSession.plist' error, got: %v", err)
	}
}

// TestSessionData_PlistTags tests that plist struct tags are correct.
func TestSessionData_PlistTags(t *testing.T) {
	// Create a plist with the expected keys.
	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>SessionVersion</key>
	<integer>1</integer>
	<key>SessionWindows</key>
	<array>
		<dict>
			<key>WindowUUID</key>
			<string>test-window-uuid-12345</string>
			<key>IsPrivate</key>
			<true/>
			<key>SelectedTabIndex</key>
			<integer>2</integer>
			<key>TabStates</key>
			<array>
				<dict>
					<key>TabUUID</key>
					<string>test-tab-uuid-67890</string>
					<key>TabURL</key>
					<string>https://test.example.com</string>
					<key>TabTitle</key>
					<string>Test Page</string>
					<key>LastVisitTime</key>
					<date>2024-01-15T10:30:00Z</date>
					<key>isAppInitiated</key>
					<true/>
				</dict>
			</array>
		</dict>
	</array>
</dict>
</plist>`

	var session sessionData
	decoder := plist.NewDecoder(strings.NewReader(plistContent))
	if err := decoder.Decode(&session); err != nil {
		t.Fatalf("failed to decode plist: %v", err)
	}

	if session.SessionVersion != 1 {
		t.Errorf("SessionVersion mismatch: %d", session.SessionVersion)
	}
	if len(session.SessionWindows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(session.SessionWindows))
	}

	window := session.SessionWindows[0]
	if window.WindowUUID != "test-window-uuid-12345" {
		t.Errorf("WindowUUID mismatch: %q", window.WindowUUID)
	}
	if !window.IsPrivate {
		t.Error("IsPrivate should be true")
	}
	if window.SelectedTabIndex != 2 {
		t.Errorf("SelectedTabIndex mismatch: %d", window.SelectedTabIndex)
	}
	if len(window.TabStates) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(window.TabStates))
	}

	tab := window.TabStates[0]
	if tab.TabUUID != "test-tab-uuid-67890" {
		t.Errorf("TabUUID mismatch: %q", tab.TabUUID)
	}
	if tab.TabURL != "https://test.example.com" {
		t.Errorf("TabURL mismatch: %q", tab.TabURL)
	}
	if tab.TabTitle != "Test Page" {
		t.Errorf("TabTitle mismatch: %q", tab.TabTitle)
	}
	if !tab.IsAppInitiated {
		t.Error("isAppInitiated should be true")
	}
	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !tab.LastVisitTime.Equal(expectedTime) {
		t.Errorf("LastVisitTime mismatch: %v", tab.LastVisitTime)
	}
}

// TestCollectLastSessionForUser_EmptyWindows tests handling of empty session windows.
func TestCollectLastSessionForUser_EmptyWindows(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create plist with empty windows array.
	emptySession := map[string]interface{}{
		"SessionVersion": 1,
		"SessionWindows": []map[string]interface{}{},
	}

	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	f, err := os.Create(sessionPath) //nolint:gosec // Test code with known path
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	encoder := plist.NewBinaryEncoder(f)
	if err := encoder.Encode(emptySession); err != nil {
		_ = f.Close()
		t.Fatalf("failed to encode plist: %v", err)
	}
	_ = f.Close()

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectLastSessionForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have added the raw file.
	if len(coll.files) != 1 {
		t.Errorf("expected 1 file, got %d", len(coll.files))
	}

	// Should have no records (empty windows).
	if len(coll.records) != 0 {
		t.Errorf("expected 0 records, got %d", len(coll.records))
	}

	// Should log info about no windows.
	if !coll.hasLogAtLevel(macfor.LogInfo, "No session windows found") {
		t.Error("expected info log about no session windows found")
	}
}

// TestCollectLastSessionForUser_WindowWithNoTabs tests handling of window with no tabs.
func TestCollectLastSessionForUser_WindowWithNoTabs(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create plist with window but no tabs.
	session := map[string]interface{}{
		"SessionVersion": 1,
		"SessionWindows": []map[string]interface{}{
			{
				"WindowUUID":       "WIN-UUID-EMPTY",
				"IsPrivate":        false,
				"SelectedTabIndex": 0,
				"TabStates":        []map[string]interface{}{},
			},
		},
	}

	sessionPath := filepath.Join(safariDir, "LastSession.plist")
	f, err := os.Create(sessionPath) //nolint:gosec // Test code with known path
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	encoder := plist.NewBinaryEncoder(f)
	if err := encoder.Encode(session); err != nil {
		_ = f.Close()
		t.Fatalf("failed to encode plist: %v", err)
	}
	_ = f.Close()

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectLastSessionForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Should have 1 window record, 0 tab records.
	windowCount := 0
	tabCount := 0
	for _, r := range coll.records {
		switch record := r.record.(type) {
		case *SessionWindowRecord:
			windowCount++
			if record.TabCount != 0 {
				t.Errorf("expected TabCount 0, got %d", record.TabCount)
			}
		case *SessionTabRecord:
			tabCount++
		}
	}

	if windowCount != 1 {
		t.Errorf("expected 1 window record, got %d", windowCount)
	}
	if tabCount != 0 {
		t.Errorf("expected 0 tab records, got %d", tabCount)
	}
}
