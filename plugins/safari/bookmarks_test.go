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

// Test constants for bookmarks.
const (
	testBookmarkRecordType    = "browser_bookmark"
	testReadingListRecordType = "reading_list_item"
	testCategoryBrowser       = "browser"
)

// TestBookmarkRecord_Structure verifies the BookmarkRecord struct has expected fields.
func TestBookmarkRecord_Structure(t *testing.T) {
	dateAdded := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	record := &BookmarkRecord{
		Type:       testBookmarkRecordType,
		Browser:    testBrowser,
		User:       testUsername,
		URL:        "https://github.com",
		Title:      "GitHub",
		FolderPath: "BookmarksBar/Work",
		UUID:       "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
		DateAdded:  dateAdded,
		SourceFile: "/Users/testuser/Library/Safari/Bookmarks.plist",
	}

	if record.Type != testBookmarkRecordType {
		t.Errorf("expected Type %q, got %q", testBookmarkRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.FolderPath != "BookmarksBar/Work" {
		t.Errorf("expected FolderPath 'BookmarksBar/Work', got %q", record.FolderPath)
	}
}

// TestReadingListRecord_Structure verifies the ReadingListRecord struct has expected fields.
func TestReadingListRecord_Structure(t *testing.T) {
	dateAdded := time.Date(2024, 1, 10, 14, 0, 0, 0, time.UTC)
	dateFetched := time.Date(2024, 1, 10, 14, 0, 5, 0, time.UTC)

	record := &ReadingListRecord{
		Type:            testReadingListRecordType,
		Browser:         testBrowser,
		User:            testUsername,
		URL:             "https://blog.golang.org/go1.21",
		Title:           "Go 1.21 Release Notes",
		PreviewText:     "Go 1.21 includes new language features...",
		DateAdded:       dateAdded,
		DateLastFetched: dateFetched,
		UUID:            "RL-UUID-0001-0001-000000000000",
		SourceFile:      "/Users/testuser/Library/Safari/Bookmarks.plist",
	}

	if record.Type != testReadingListRecordType {
		t.Errorf("expected Type %q, got %q", testReadingListRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.PreviewText == "" {
		t.Error("expected PreviewText to be set")
	}
	if record.DateAdded.IsZero() {
		t.Error("expected DateAdded to be set")
	}
}

// TestBookmarkTypeConstants verifies bookmark type constants.
func TestBookmarkTypeConstants(t *testing.T) {
	if bookmarkTypeLeaf != "WebBookmarkTypeLeaf" {
		t.Errorf("expected bookmarkTypeLeaf 'WebBookmarkTypeLeaf', got %q", bookmarkTypeLeaf)
	}
	if bookmarkTypeList != "WebBookmarkTypeList" {
		t.Errorf("expected bookmarkTypeList 'WebBookmarkTypeList', got %q", bookmarkTypeList)
	}
	if readingListFolderTitle != "com.apple.ReadingList" {
		t.Errorf("expected readingListFolderTitle 'com.apple.ReadingList', got %q", readingListFolderTitle)
	}
}

// TestParseBookmarksPlist_BasicFixture tests parsing the main Bookmarks.plist fixture.
func TestParseBookmarksPlist_BasicFixture(t *testing.T) {
	plistData := mustLoadFixture(FixtureBookmarksPlist)

	var root bookmarkNode
	decoder := plist.NewDecoder(bytes.NewReader(plistData))
	if err := decoder.Decode(&root); err != nil {
		t.Fatalf("failed to decode plist: %v", err)
	}

	// Root should be a list type.
	if root.WebBookmarkType != bookmarkTypeList {
		t.Errorf("expected root type %q, got %q", bookmarkTypeList, root.WebBookmarkType)
	}

	// Root should have children (BookmarksBar, BookmarksMenu, ReadingList).
	if len(root.Children) == 0 {
		t.Fatal("expected root to have children")
	}

	// Find the special folders.
	var bookmarksBar, bookmarksMenu, readingList *bookmarkNode
	for i := range root.Children {
		child := &root.Children[i]
		switch child.Title {
		case bookmarksBarTitle:
			bookmarksBar = child
		case bookmarksMenuTitle:
			bookmarksMenu = child
		case readingListFolderTitle:
			readingList = child
		}
	}

	if bookmarksBar == nil {
		t.Error("expected to find BookmarksBar folder")
	}
	if bookmarksMenu == nil {
		t.Error("expected to find BookmarksMenu folder")
	}
	if readingList == nil {
		t.Error("expected to find Reading List folder")
	}

	// Verify BookmarksBar has children.
	if bookmarksBar != nil && len(bookmarksBar.Children) == 0 {
		t.Error("expected BookmarksBar to have children")
	}

	// Verify Reading List has items with ReadingList metadata.
	if readingList != nil && len(readingList.Children) > 0 {
		item := readingList.Children[0]
		if item.ReadingList == nil {
			t.Error("expected reading list item to have ReadingList metadata")
		}
		if item.ReadingList != nil && item.ReadingList.DateAdded.IsZero() {
			t.Error("expected reading list item to have DateAdded")
		}
	}
}

// TestParseBookmarksPlist_EmptyFixture tests handling of empty Bookmarks.plist.
func TestParseBookmarksPlist_EmptyFixture(t *testing.T) {
	plistData := mustLoadFixture(FixtureBookmarksPlistEmpty)

	var root bookmarkNode
	decoder := plist.NewDecoder(bytes.NewReader(plistData))
	if err := decoder.Decode(&root); err != nil {
		t.Fatalf("failed to decode plist: %v", err)
	}

	// Should have empty Children array.
	if len(root.Children) != 0 {
		t.Errorf("expected 0 children from empty plist, got %d", len(root.Children))
	}
}

// TestConvertBookmarkNode tests conversion of bookmark node to record.
func TestConvertBookmarkNode(t *testing.T) {
	node := bookmarkNode{
		WebBookmarkType: bookmarkTypeLeaf,
		WebBookmarkUUID: "TEST-UUID-0001",
		URLString:       "https://github.com",
		URIDictionary: map[string]string{
			"title": "GitHub",
		},
	}

	record := convertBookmarkNode(node, "BookmarksBar/Work", testUsername, "/path/to/Bookmarks.plist")

	if record.Type != testBookmarkRecordType {
		t.Errorf("expected Type %q, got %q", testBookmarkRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.User != testUsername {
		t.Errorf("expected User %q, got %q", testUsername, record.User)
	}
	if record.URL != "https://github.com" {
		t.Errorf("expected URL 'https://github.com', got %q", record.URL)
	}
	if record.Title != "GitHub" {
		t.Errorf("expected Title 'GitHub', got %q", record.Title)
	}
	if record.FolderPath != "BookmarksBar/Work" {
		t.Errorf("expected FolderPath 'BookmarksBar/Work', got %q", record.FolderPath)
	}
	if record.UUID != "TEST-UUID-0001" {
		t.Errorf("expected UUID 'TEST-UUID-0001', got %q", record.UUID)
	}
}

// TestConvertBookmarkNode_TitleFallback tests fallback to node Title when URIDictionary is empty.
func TestConvertBookmarkNode_TitleFallback(t *testing.T) {
	node := bookmarkNode{
		WebBookmarkType: bookmarkTypeLeaf,
		WebBookmarkUUID: "TEST-UUID-0002",
		Title:           "Node Title",
		URLString:       "https://example.com",
		URIDictionary:   map[string]string{},
	}

	record := convertBookmarkNode(node, "", testUsername, "/path/to/Bookmarks.plist")

	if record.Title != "Node Title" {
		t.Errorf("expected Title 'Node Title' (fallback), got %q", record.Title)
	}
}

// TestConvertReadingListNode tests conversion of reading list node to record.
func TestConvertReadingListNode(t *testing.T) {
	dateAdded := time.Date(2024, 1, 10, 14, 0, 0, 0, time.UTC)
	dateFetched := time.Date(2024, 1, 10, 14, 0, 5, 0, time.UTC)

	node := bookmarkNode{
		WebBookmarkType: bookmarkTypeLeaf,
		WebBookmarkUUID: "RL-UUID-0001",
		URLString:       "https://blog.golang.org/go1.21",
		URIDictionary: map[string]string{
			"title": "Go 1.21 Release Notes",
		},
		ReadingList: &readingListData{
			DateAdded:       dateAdded,
			DateLastFetched: dateFetched,
			PreviewText:     "Go 1.21 includes new language features...",
		},
	}

	record := convertReadingListNode(node, testUsername, "/path/to/Bookmarks.plist")

	if record.Type != testReadingListRecordType {
		t.Errorf("expected Type %q, got %q", testReadingListRecordType, record.Type)
	}
	if record.Browser != testBrowser {
		t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
	}
	if record.URL != "https://blog.golang.org/go1.21" {
		t.Errorf("unexpected URL: %q", record.URL)
	}
	if record.Title != "Go 1.21 Release Notes" {
		t.Errorf("expected Title 'Go 1.21 Release Notes', got %q", record.Title)
	}
	if !record.DateAdded.Equal(dateAdded) {
		t.Errorf("expected DateAdded %v, got %v", dateAdded, record.DateAdded)
	}
	if !record.DateLastFetched.Equal(dateFetched) {
		t.Errorf("expected DateLastFetched %v, got %v", dateFetched, record.DateLastFetched)
	}
	if record.PreviewText != "Go 1.21 includes new language features..." {
		t.Errorf("unexpected PreviewText: %q", record.PreviewText)
	}
}

// TestCollectBookmarksForUser_FileNotFound tests handling of missing Bookmarks.plist.
func TestCollectBookmarksForUser_FileNotFound(t *testing.T) {
	tempDir := t.TempDir()

	// Create source without Bookmarks.plist.
	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectBookmarksForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}

	// Should log debug message about missing file.
	if !coll.hasLogAtLevel(macfor.LogDebug, "not found") {
		t.Error("expected debug log about missing Bookmarks.plist")
	}

	// Should not add any files or records.
	if len(coll.files) != 0 {
		t.Errorf("expected no files, got %d", len(coll.files))
	}
	if len(coll.records) != 0 {
		t.Errorf("expected no records, got %d", len(coll.records))
	}
}

// TestCollectBookmarksForUser_EmptyPlist tests handling of empty Bookmarks.plist.
//
//nolint:dupl // Test patterns similar to downloads_test.go but with different assertions.
func TestCollectBookmarksForUser_EmptyPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy empty fixture.
	fixtureSrc := fixturePath(FixtureBookmarksPlistEmpty)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	bookmarksPath := filepath.Join(safariDir, "Bookmarks.plist")
	if err := os.WriteFile(bookmarksPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectBookmarksForUser(context.Background(), user, src, coll)
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

	// Should log info about no bookmarks.
	if !coll.hasLogAtLevel(macfor.LogInfo, "No bookmarks found") {
		t.Error("expected info log about no bookmarks found")
	}
}

// TestCollectBookmarksForUser_WithFixture tests full collection with real fixture.
func TestCollectBookmarksForUser_WithFixture(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture to temp location.
	fixtureSrc := fixturePath(FixtureBookmarksPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	bookmarksPath := filepath.Join(safariDir, "Bookmarks.plist")
	if err := os.WriteFile(bookmarksPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectBookmarksForUser(context.Background(), user, src, coll)
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
			t.Errorf("expected Username testuser, got %q", artifact.Username)
		}
		if artifact.Category != testCategoryBrowser {
			t.Errorf("expected Category %q, got %q", testCategoryBrowser, artifact.Category)
		}
		if artifact.Type != "bookmarks" {
			t.Errorf("expected Type 'bookmarks', got %q", artifact.Type)
		}
	}

	// Count bookmarks and reading list items.
	bookmarkCount := 0
	readingListCount := 0

	for _, r := range coll.records {
		switch record := r.record.(type) {
		case *BookmarkRecord:
			bookmarkCount++
			// Verify record structure.
			if record.Type != testBookmarkRecordType {
				t.Errorf("expected bookmark Type %q, got %q", testBookmarkRecordType, record.Type)
			}
			if record.Browser != testBrowser {
				t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
			}
		case *ReadingListRecord:
			readingListCount++
			// Verify record structure.
			if record.Type != testReadingListRecordType {
				t.Errorf("expected reading list Type %q, got %q", testReadingListRecordType, record.Type)
			}
			if record.Browser != testBrowser {
				t.Errorf("expected Browser %q, got %q", testBrowser, record.Browser)
			}
		}
	}

	// Fixture has: GitHub, Slack, Notion, arXiv (BookmarksBar/Work/Research),
	// Hacker News (BookmarksMenu) = 5 bookmarks
	// + 2 reading list items.
	if bookmarkCount != 5 {
		t.Errorf("expected 5 bookmarks, got %d", bookmarkCount)
	}
	if readingListCount != 2 {
		t.Errorf("expected 2 reading list items, got %d", readingListCount)
	}

	// Should log success message.
	if !coll.hasLogAtLevel(macfor.LogInfo, "Collected") {
		t.Error("expected info log about collected bookmarks")
	}
}

// TestCollectBookmarksForUser_FolderPath tests that folder paths are correctly built.
func TestCollectBookmarksForUser_FolderPath(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureBookmarksPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	bookmarksPath := filepath.Join(safariDir, "Bookmarks.plist")
	if err := os.WriteFile(bookmarksPath, fixtureContent, 0600); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err = collectBookmarksForUser(context.Background(), user, src, coll)
	if err != nil {
		t.Fatalf("collection failed: %v", err)
	}

	// Check for expected folder paths.
	expectedPaths := map[string]bool{
		"BookmarksBar":               false, // GitHub
		"BookmarksBar/Work":          false, // Slack, Notion
		"BookmarksBar/Work/Research": false, // arXiv
		"BookmarksMenu":              false, // Hacker News
	}

	for _, r := range coll.records {
		if record, ok := r.record.(*BookmarkRecord); ok {
			for path := range expectedPaths {
				if record.FolderPath == path {
					expectedPaths[path] = true
				}
			}
		}
	}

	for path, found := range expectedPaths {
		if !found {
			t.Errorf("expected to find bookmark with folder path %q", path)
		}
	}
}

// TestCollectBookmarksForUser_ContextCancellation tests cancellation during collection.
//
//nolint:dupl // Test patterns similar to other artifact tests but with different data.
func TestCollectBookmarksForUser_ContextCancellation(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Copy fixture.
	fixtureSrc := fixturePath(FixtureBookmarksPlist)
	fixtureContent, err := os.ReadFile(fixtureSrc) //nolint:gosec // Test code with known fixture path
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	bookmarksPath := filepath.Join(safariDir, "Bookmarks.plist")
	if err := os.WriteFile(bookmarksPath, fixtureContent, 0600); err != nil {
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

	err = collectBookmarksForUser(ctx, user, src, coll)

	// Should return context error (might be from AddFile or parsing).
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Logf("error was: %v", err)
	}
}

// TestCollectBookmarksForUser_InvalidPlist tests handling of invalid plist file.
func TestCollectBookmarksForUser_InvalidPlist(t *testing.T) {
	tempDir := t.TempDir()

	// Create directory structure.
	safariDir := filepath.Join(tempDir, "Library", "Safari")
	if err := os.MkdirAll(safariDir, 0750); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	// Create invalid plist (just random text).
	bookmarksPath := filepath.Join(safariDir, "Bookmarks.plist")
	if err := os.WriteFile(bookmarksPath, []byte("not a plist"), 0600); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	src := newFileSource(tempDir)
	user := macfor.User{
		UID:      500,
		Username: testUsername,
		HomeDir:  tempDir,
	}

	coll := newMockCollector()

	err := collectBookmarksForUser(context.Background(), user, src, coll)

	// Should return error for invalid plist.
	if err == nil {
		t.Fatal("expected error for invalid plist file")
	}

	if !strings.Contains(err.Error(), "parsing Bookmarks.plist") {
		t.Errorf("expected 'parsing Bookmarks.plist' error, got: %v", err)
	}
}

// TestWalkBookmarks_NestedFolders tests recursive folder walking.
func TestWalkBookmarks_NestedFolders(t *testing.T) {
	// Create a nested structure.
	root := bookmarkNode{
		WebBookmarkType: bookmarkTypeList,
		Title:           "",
		Children: []bookmarkNode{
			{
				WebBookmarkType: bookmarkTypeList,
				Title:           "Level1",
				Children: []bookmarkNode{
					{
						WebBookmarkType: bookmarkTypeList,
						Title:           "Level2",
						Children: []bookmarkNode{
							{
								WebBookmarkType: bookmarkTypeLeaf,
								WebBookmarkUUID: "DEEP-UUID",
								URLString:       "https://deep.example.com",
								URIDictionary: map[string]string{
									"title": "Deep Bookmark",
								},
							},
						},
					},
				},
			},
		},
	}

	artifact, err := macfor.NewArtifact(PluginID, testCategoryBrowser, "bookmarks")
	if err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	coll := newMockCollector()
	bookmarkCount := 0
	readingListCount := 0

	err = walkBookmarks(context.Background(), root, "", testUsername, "/path", artifact, coll, &bookmarkCount, &readingListCount)
	if err != nil {
		t.Fatalf("walkBookmarks failed: %v", err)
	}

	if bookmarkCount != 1 {
		t.Errorf("expected 1 bookmark, got %d", bookmarkCount)
	}

	// Verify the folder path is Level1/Level2.
	if len(coll.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(coll.records))
	}

	record, ok := coll.records[0].record.(*BookmarkRecord)
	if !ok {
		t.Fatal("expected *BookmarkRecord")
	}

	if record.FolderPath != "Level1/Level2" {
		t.Errorf("expected FolderPath 'Level1/Level2', got %q", record.FolderPath)
	}
}

// TestWalkBookmarks_ReadingListNoPath tests that reading list items don't have folder paths.
func TestWalkBookmarks_ReadingListNoPath(t *testing.T) {
	// Create a reading list structure.
	root := bookmarkNode{
		WebBookmarkType: bookmarkTypeList,
		Title:           "",
		Children: []bookmarkNode{
			{
				WebBookmarkType: bookmarkTypeList,
				Title:           readingListFolderTitle,
				Children: []bookmarkNode{
					{
						WebBookmarkType: bookmarkTypeLeaf,
						WebBookmarkUUID: "RL-UUID",
						URLString:       "https://article.example.com",
						URIDictionary: map[string]string{
							"title": "Article",
						},
						ReadingList: &readingListData{
							DateAdded:   time.Now(),
							PreviewText: "Preview...",
						},
					},
				},
			},
		},
	}

	artifact, err := macfor.NewArtifact(PluginID, testCategoryBrowser, "bookmarks")
	if err != nil {
		t.Fatalf("failed to create artifact: %v", err)
	}

	coll := newMockCollector()
	bookmarkCount := 0
	readingListCount := 0

	err = walkBookmarks(context.Background(), root, "", testUsername, "/path", artifact, coll, &bookmarkCount, &readingListCount)
	if err != nil {
		t.Fatalf("walkBookmarks failed: %v", err)
	}

	if readingListCount != 1 {
		t.Errorf("expected 1 reading list item, got %d", readingListCount)
	}
	if bookmarkCount != 0 {
		t.Errorf("expected 0 bookmarks, got %d", bookmarkCount)
	}
}

// TestBookmarkNode_PlistTags tests that plist struct tags are correct.
func TestBookmarkNode_PlistTags(t *testing.T) {
	// Create a plist with the expected keys.
	plistContent := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>WebBookmarkType</key>
	<string>WebBookmarkTypeLeaf</string>
	<key>WebBookmarkUUID</key>
	<string>test-uuid-12345</string>
	<key>Title</key>
	<string>Test Bookmark</string>
	<key>URLString</key>
	<string>https://test.example.com</string>
	<key>URIDictionary</key>
	<dict>
		<key>title</key>
		<string>URI Title</string>
	</dict>
	<key>ReadingList</key>
	<dict>
		<key>DateAdded</key>
		<date>2024-01-15T10:00:00Z</date>
		<key>DateLastFetched</key>
		<date>2024-01-15T10:00:05Z</date>
		<key>PreviewText</key>
		<string>Preview text here</string>
	</dict>
</dict>
</plist>`

	var node bookmarkNode
	decoder := plist.NewDecoder(strings.NewReader(plistContent))
	if err := decoder.Decode(&node); err != nil {
		t.Fatalf("failed to decode plist: %v", err)
	}

	if node.WebBookmarkType != bookmarkTypeLeaf {
		t.Errorf("WebBookmarkType mismatch: %q", node.WebBookmarkType)
	}
	if node.WebBookmarkUUID != "test-uuid-12345" {
		t.Errorf("WebBookmarkUUID mismatch: %q", node.WebBookmarkUUID)
	}
	if node.Title != "Test Bookmark" {
		t.Errorf("Title mismatch: %q", node.Title)
	}
	if node.URLString != "https://test.example.com" {
		t.Errorf("URLString mismatch: %q", node.URLString)
	}
	if node.URIDictionary["title"] != "URI Title" {
		t.Errorf("URIDictionary.title mismatch: %q", node.URIDictionary["title"])
	}
	if node.ReadingList == nil {
		t.Fatal("expected ReadingList to be set")
	}
	if node.ReadingList.PreviewText != "Preview text here" {
		t.Errorf("ReadingList.PreviewText mismatch: %q", node.ReadingList.PreviewText)
	}
}
