//go:build ignore

// generate_fixtures.go generates Safari test fixtures for unit testing.
// Run with: go run generate_fixtures.go
package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"howett.net/plist"
	_ "modernc.org/sqlite"
)

// CoreDataEpochUnix is the Unix timestamp of 2001-01-01 00:00:00 UTC.
const CoreDataEpochUnix = 978307200

// timeToCoreData converts a time.Time to Core Data timestamp.
func timeToCoreData(t time.Time) float64 {
	return float64(t.Unix() - CoreDataEpochUnix)
}

func main() {
	// Get the directory containing this source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintf(os.Stderr, "Could not determine source file location\n")
		os.Exit(1)
	}
	dir := filepath.Dir(filename)

	fmt.Println("Generating Safari test fixtures...")

	// Generate History.db
	if err := generateHistoryDB(filepath.Join(dir, "History.db")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating History.db: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created History.db")

	// Generate History_empty.db
	if err := generateEmptyHistoryDB(filepath.Join(dir, "History_empty.db")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating History_empty.db: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created History_empty.db")

	// Generate History_unicode.db
	if err := generateUnicodeHistoryDB(filepath.Join(dir, "History_unicode.db")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating History_unicode.db: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created History_unicode.db")

	// Generate History_large.db
	if err := generateLargeHistoryDB(filepath.Join(dir, "History_large.db")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating History_large.db: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created History_large.db")

	// Generate Downloads.plist
	if err := generateDownloadsPlist(filepath.Join(dir, "Downloads.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Downloads.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Downloads.plist")

	// Generate Downloads_empty.plist
	if err := generateEmptyDownloadsPlist(filepath.Join(dir, "Downloads_empty.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Downloads_empty.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Downloads_empty.plist")

	// Generate Bookmarks.plist
	if err := generateBookmarksPlist(filepath.Join(dir, "Bookmarks.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Bookmarks.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Bookmarks.plist")

	// Generate Bookmarks_empty.plist
	if err := generateEmptyBookmarksPlist(filepath.Join(dir, "Bookmarks_empty.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Bookmarks_empty.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Bookmarks_empty.plist")

	// Generate Cookies.binarycookies
	if err := generateCookiesBinary(filepath.Join(dir, "Cookies.binarycookies")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Cookies.binarycookies: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Cookies.binarycookies")

	// Generate Cookies_empty.binarycookies
	if err := generateEmptyCookiesBinary(filepath.Join(dir, "Cookies_empty.binarycookies")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Cookies_empty.binarycookies: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Cookies_empty.binarycookies")

	// Generate Extensions.plist
	if err := generateExtensionsPlist(filepath.Join(dir, "Extensions.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating Extensions.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created Extensions.plist")

	// Generate LastSession.plist
	if err := generateLastSessionPlist(filepath.Join(dir, "LastSession.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating LastSession.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created LastSession.plist")

	// Generate LocalStorage databases
	if err := generateLocalStorage(filepath.Join(dir, "LocalStorage")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating LocalStorage: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created LocalStorage databases")

	// Generate TopSites.plist
	if err := generateTopSitesPlist(filepath.Join(dir, "TopSites.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating TopSites.plist: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Created TopSites.plist")

	fmt.Println("All fixtures generated successfully!")
}

func generateHistoryDB(path string) error {
	os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create schema matching Safari History.db
	schema := `
		CREATE TABLE history_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			domain_expansion TEXT,
			visit_count INTEGER NOT NULL DEFAULT 0,
			daily_visit_counts BLOB,
			weekly_visit_counts BLOB,
			autocomplete_triggers BLOB,
			should_recompute_derived_visit_counts INTEGER NOT NULL DEFAULT 1,
			visit_count_score INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE history_visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_item INTEGER NOT NULL REFERENCES history_items(id) ON DELETE CASCADE,
			visit_time REAL NOT NULL,
			title TEXT,
			load_successful INTEGER NOT NULL DEFAULT 1,
			http_non_get INTEGER NOT NULL DEFAULT 0,
			synthesized INTEGER NOT NULL DEFAULT 0,
			redirect_source INTEGER REFERENCES history_visits(id) ON DELETE CASCADE,
			redirect_destination INTEGER REFERENCES history_visits(id) ON DELETE CASCADE,
			origin INTEGER NOT NULL DEFAULT 0,
			generation INTEGER NOT NULL DEFAULT 0,
			attributes INTEGER NOT NULL DEFAULT 0,
			score INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE history_client_version (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		CREATE TABLE history_tombstones (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL,
			generation INTEGER NOT NULL,
			created REAL NOT NULL
		);

		CREATE INDEX history_visits_history_item ON history_visits(history_item);
		CREATE INDEX history_visits_visit_time ON history_visits(visit_time);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	// Insert client version
	if _, err := db.Exec(`INSERT INTO history_client_version (key, value) VALUES ('LastMajorSyncVersion', '1')`); err != nil {
		return fmt.Errorf("inserting client version: %w", err)
	}

	// Sample history data - visits to various sites
	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	historyItems := []struct {
		url        string
		domain     string
		visitCount int
	}{
		{"https://www.google.com/search?q=golang+tutorial", "google.com", 5},
		{"https://github.com/anthropics/claude-code", "github.com", 3},
		{"https://stackoverflow.com/questions/12345/go-error-handling", "stackoverflow.com", 2},
		{"https://docs.github.com/en/rest", "github.com", 1},
		{"https://news.ycombinator.com/", "ycombinator.com", 10},
		{"https://www.reddit.com/r/golang/", "reddit.com", 4},
		{"https://pkg.go.dev/database/sql", "go.dev", 2},
		{"https://en.wikipedia.org/wiki/Go_(programming_language)", "wikipedia.org", 1},
	}

	for i, item := range historyItems {
		_, err := db.Exec(`
			INSERT INTO history_items (url, domain_expansion, visit_count)
			VALUES (?, ?, ?)`,
			item.url, item.domain, item.visitCount)
		if err != nil {
			return fmt.Errorf("inserting history item %d: %w", i, err)
		}
	}

	// Insert visits with timestamps
	visits := []struct {
		historyItem    int
		title          string
		minutesFromNow int
		loadSuccess    bool
		redirectSource *int
	}{
		{1, "golang tutorial - Google Search", 0, true, nil},
		{1, "golang tutorial - Google Search", 60, true, nil},
		{1, "golang tutorial - Google Search", 120, true, nil},
		{2, "anthropics/claude-code: Claude Code CLI", 30, true, nil},
		{2, "anthropics/claude-code: Claude Code CLI", 180, true, nil},
		{3, "go - Error handling in Go - Stack Overflow", 45, true, nil},
		{4, "GitHub REST API documentation", 90, true, nil},
		{5, "Hacker News", 15, true, nil},
		{5, "Hacker News", 75, true, nil},
		{5, "Hacker News", 135, true, nil},
		{6, "r/golang - The Go Programming Language", 50, true, nil},
		{7, "database/sql package - Go Packages", 110, true, nil},
		{8, "Go (programming language) - Wikipedia", 150, true, nil},
		{1, "", 200, false, nil}, // Failed load
	}

	for i, v := range visits {
		visitTime := timeToCoreData(baseTime.Add(time.Duration(v.minutesFromNow) * time.Minute))
		loadSuccess := 1
		if !v.loadSuccess {
			loadSuccess = 0
		}

		var redirectSrc interface{} = nil
		if v.redirectSource != nil {
			redirectSrc = *v.redirectSource
		}

		_, err := db.Exec(`
			INSERT INTO history_visits (history_item, visit_time, title, load_successful, redirect_source)
			VALUES (?, ?, ?, ?, ?)`,
			v.historyItem, visitTime, v.title, loadSuccess, redirectSrc)
		if err != nil {
			return fmt.Errorf("inserting visit %d: %w", i, err)
		}
	}

	return nil
}

func generateEmptyHistoryDB(path string) error {
	os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create schema but don't insert any data
	schema := `
		CREATE TABLE history_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			domain_expansion TEXT,
			visit_count INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE history_visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_item INTEGER NOT NULL REFERENCES history_items(id) ON DELETE CASCADE,
			visit_time REAL NOT NULL,
			title TEXT,
			load_successful INTEGER NOT NULL DEFAULT 1
		);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	return nil
}

func generateUnicodeHistoryDB(path string) error {
	os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	schema := `
		CREATE TABLE history_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			domain_expansion TEXT,
			visit_count INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE history_visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_item INTEGER NOT NULL REFERENCES history_items(id) ON DELETE CASCADE,
			visit_time REAL NOT NULL,
			title TEXT,
			load_successful INTEGER NOT NULL DEFAULT 1
		);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	// Unicode URLs and titles
	items := []struct {
		url   string
		title string
	}{
		{"https://ja.wikipedia.org/wiki/Go_(プログラミング言語)", "Go (プログラミング言語) - Wikipedia"},
		{"https://zh.wikipedia.org/wiki/Go", "Go语言 - 维基百科"},
		{"https://ru.wikipedia.org/wiki/Go", "Go (язык программирования) — Википедия"},
		{"https://example.com/emoji/🎉🎊🥳", "Party Time 🎉"},
		{"https://example.com/arabic/مرحبا", "مرحبا بكم"},
		{"https://example.com/hebrew/שלום", "ברוכים הבאים"},
	}

	for i, item := range items {
		if _, err := db.Exec(`INSERT INTO history_items (url, visit_count) VALUES (?, 1)`, item.url); err != nil {
			return fmt.Errorf("inserting item %d: %w", i, err)
		}

		visitTime := timeToCoreData(baseTime.Add(time.Duration(i*10) * time.Minute))
		if _, err := db.Exec(`INSERT INTO history_visits (history_item, visit_time, title, load_successful) VALUES (?, ?, ?, 1)`,
			i+1, visitTime, item.title); err != nil {
			return fmt.Errorf("inserting visit %d: %w", i, err)
		}
	}

	return nil
}

func generateLargeHistoryDB(path string) error {
	os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	schema := `
		CREATE TABLE history_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL UNIQUE,
			domain_expansion TEXT,
			visit_count INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE history_visits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_item INTEGER NOT NULL REFERENCES history_items(id) ON DELETE CASCADE,
			visit_time REAL NOT NULL,
			title TEXT,
			load_successful INTEGER NOT NULL DEFAULT 1
		);

		CREATE INDEX history_visits_history_item ON history_visits(history_item);
		CREATE INDEX history_visits_visit_time ON history_visits(visit_time);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Insert 1000 items (more than typical test but still manageable)
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	insertItem, err := tx.Prepare(`INSERT INTO history_items (url, domain_expansion, visit_count) VALUES (?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer insertItem.Close()

	insertVisit, err := tx.Prepare(`INSERT INTO history_visits (history_item, visit_time, title, load_successful) VALUES (?, ?, ?, 1)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer insertVisit.Close()

	for i := 0; i < 1000; i++ {
		url := fmt.Sprintf("https://example%d.com/page/%d", i%100, i)
		domain := fmt.Sprintf("example%d.com", i%100)

		if _, err := insertItem.Exec(url, domain, 1); err != nil {
			tx.Rollback()
			return fmt.Errorf("inserting item %d: %w", i, err)
		}

		visitTime := timeToCoreData(baseTime.Add(time.Duration(i) * time.Minute))
		title := fmt.Sprintf("Page %d - Example Site", i)
		if _, err := insertVisit.Exec(i+1, visitTime, title); err != nil {
			tx.Rollback()
			return fmt.Errorf("inserting visit %d: %w", i, err)
		}
	}

	return tx.Commit()
}

func generateDownloadsPlist(path string) error {
	baseTime := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	downloads := []map[string]interface{}{
		{
			"DownloadEntryURL":                "https://github.com/anthropics/claude-code/releases/download/v1.0.0/claude-code-v1.0.0.zip",
			"DownloadEntryPath":               "/Users/testuser/Downloads/claude-code-v1.0.0.zip",
			"DownloadEntryDateAddedKey":       baseTime,
			"DownloadEntryDateFinishedKey":    baseTime.Add(30 * time.Second),
			"DownloadEntryProgressTotalToLoad": int64(5242880),
			"DownloadEntryProgressBytesSoFar": int64(5242880),
			"DownloadEntryIdentifier":         "A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
			"DownloadEntryRemoveWhenDoneKey":  false,
		},
		{
			"DownloadEntryURL":                "https://golang.org/dl/go1.21.5.darwin-arm64.pkg",
			"DownloadEntryPath":               "/Users/testuser/Downloads/go1.21.5.darwin-arm64.pkg",
			"DownloadEntryDateAddedKey":       baseTime.Add(-24 * time.Hour),
			"DownloadEntryDateFinishedKey":    baseTime.Add(-24*time.Hour + 2*time.Minute),
			"DownloadEntryProgressTotalToLoad": int64(73000000),
			"DownloadEntryProgressBytesSoFar": int64(73000000),
			"DownloadEntryIdentifier":         "B2C3D4E5-F6A7-8901-BCDE-F12345678901",
			"DownloadEntryRemoveWhenDoneKey":  true,
		},
		{
			// Incomplete download
			"DownloadEntryURL":                "https://example.com/large-file.iso",
			"DownloadEntryPath":               "/Users/testuser/Downloads/large-file.iso",
			"DownloadEntryDateAddedKey":       baseTime.Add(-1 * time.Hour),
			"DownloadEntryProgressTotalToLoad": int64(4700000000),
			"DownloadEntryProgressBytesSoFar": int64(1500000000),
			"DownloadEntryIdentifier":         "C3D4E5F6-A7B8-9012-CDEF-123456789012",
			"DownloadEntryRemoveWhenDoneKey":  false,
		},
	}

	return writePlist(path, downloads)
}

func generateEmptyDownloadsPlist(path string) error {
	return writePlist(path, []map[string]interface{}{})
}

func generateBookmarksPlist(path string) error {
	bookmarks := map[string]interface{}{
		"WebBookmarkUUID":        "ROOT-UUID-0000-0000-000000000000",
		"WebBookmarkType":        "WebBookmarkTypeList",
		"Title":                  "",
		"Children": []map[string]interface{}{
			// Bookmarks Bar
			{
				"WebBookmarkUUID":        "BAR-UUID-0001-0000-000000000000",
				"WebBookmarkType":        "WebBookmarkTypeList",
				"Title":                  "BookmarksBar",
				"Children": []map[string]interface{}{
					{
						"WebBookmarkUUID": "BM-UUID-0001-0001-000000000000",
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://github.com",
						"URIDictionary": map[string]string{
							"title": "GitHub",
						},
					},
					{
						// Nested folder
						"WebBookmarkUUID": "FOLDER-0001-0001-000000000000",
						"WebBookmarkType": "WebBookmarkTypeList",
						"Title":           "Work",
						"Children": []map[string]interface{}{
							{
								"WebBookmarkUUID": "BM-UUID-0002-0001-000000000000",
								"WebBookmarkType": "WebBookmarkTypeLeaf",
								"URLString":       "https://slack.com",
								"URIDictionary": map[string]string{
									"title": "Slack",
								},
							},
							{
								"WebBookmarkUUID": "BM-UUID-0002-0002-000000000000",
								"WebBookmarkType": "WebBookmarkTypeLeaf",
								"URLString":       "https://notion.so",
								"URIDictionary": map[string]string{
									"title": "Notion",
								},
							},
							{
								// Deeper nested folder
								"WebBookmarkUUID": "FOLDER-0002-0001-000000000000",
								"WebBookmarkType": "WebBookmarkTypeList",
								"Title":           "Research",
								"Children": []map[string]interface{}{
									{
										"WebBookmarkUUID": "BM-UUID-0003-0001-000000000000",
										"WebBookmarkType": "WebBookmarkTypeLeaf",
										"URLString":       "https://arxiv.org",
										"URIDictionary": map[string]string{
											"title": "arXiv.org",
										},
									},
								},
							},
						},
					},
				},
			},
			// Bookmarks Menu
			{
				"WebBookmarkUUID":        "MENU-UUID-0001-0000-000000000000",
				"WebBookmarkType":        "WebBookmarkTypeList",
				"Title":                  "BookmarksMenu",
				"Children": []map[string]interface{}{
					{
						"WebBookmarkUUID": "BM-UUID-0004-0001-000000000000",
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://news.ycombinator.com",
						"URIDictionary": map[string]string{
							"title": "Hacker News",
						},
					},
				},
			},
			// Reading List
			{
				"WebBookmarkUUID":        "RL-UUID-0001-0000-000000000000",
				"WebBookmarkType":        "WebBookmarkTypeList",
				"Title":                  "com.apple.ReadingList",
				"Children": []map[string]interface{}{
					{
						"WebBookmarkUUID": "RL-ITEM-0001-0001-000000000000",
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://blog.golang.org/go1.21",
						"URIDictionary": map[string]string{
							"title": "Go 1.21 Release Notes",
						},
						"ReadingList": map[string]interface{}{
							"DateAdded":       time.Date(2024, 1, 10, 14, 0, 0, 0, time.UTC),
							"DateLastFetched": time.Date(2024, 1, 10, 14, 0, 5, 0, time.UTC),
							"PreviewText":     "Go 1.21 includes new language features, performance improvements, and more...",
						},
					},
					{
						"WebBookmarkUUID": "RL-ITEM-0001-0002-000000000000",
						"WebBookmarkType": "WebBookmarkTypeLeaf",
						"URLString":       "https://anthropic.com/research/claude",
						"URIDictionary": map[string]string{
							"title": "Claude - Anthropic",
						},
						"ReadingList": map[string]interface{}{
							"DateAdded":       time.Date(2024, 1, 12, 9, 30, 0, 0, time.UTC),
							"DateLastFetched": time.Date(2024, 1, 12, 9, 30, 10, 0, time.UTC),
							"PreviewText":     "Claude is an AI assistant made by Anthropic to be helpful, harmless, and honest.",
						},
					},
				},
			},
		},
	}

	return writePlist(path, bookmarks)
}

func generateEmptyBookmarksPlist(path string) error {
	bookmarks := map[string]interface{}{
		"WebBookmarkUUID": "ROOT-UUID-0000-0000-000000000000",
		"WebBookmarkType": "WebBookmarkTypeList",
		"Title":           "",
		"Children":        []map[string]interface{}{},
	}
	return writePlist(path, bookmarks)
}

// generateCookiesBinary creates a Safari binary cookies file.
// Format reference: https://github.com/as0ler/BinaryCookieReader
func generateCookiesBinary(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Magic header "cook"
	if _, err := f.Write([]byte("cook")); err != nil {
		return err
	}

	// Sample cookies
	cookies := []struct {
		domain   string
		name     string
		path     string
		value    string
		flags    uint32
		expiry   float64
		creation float64
	}{
		{".github.com", "_gh_sess", "/", "session_token_example_12345", 0x0005, timeToCoreData(time.Now().Add(24 * time.Hour)), timeToCoreData(time.Now())},
		{".google.com", "NID", "/", "nid_cookie_value_67890", 0x0001, timeToCoreData(time.Now().Add(180 * 24 * time.Hour)), timeToCoreData(time.Now())},
		{"stackoverflow.com", "prov", "/", "prov_value_abcdef", 0x0000, timeToCoreData(time.Now().Add(365 * 24 * time.Hour)), timeToCoreData(time.Now())},
	}

	// Build pages (one page with all cookies for simplicity)
	var pageData []byte

	// Page header
	pageData = append(pageData, 0x00, 0x00, 0x01, 0x00) // Page header magic

	// Number of cookies in page (little-endian)
	numCookies := uint32(len(cookies))
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, numCookies)
	pageData = append(pageData, buf...)

	// Cookie offsets (we'll calculate these after building cookie data)
	cookieOffsetStart := len(pageData)
	for range cookies {
		pageData = append(pageData, 0, 0, 0, 0) // Placeholder for offset
	}

	// Page end marker
	pageData = append(pageData, 0x00, 0x00, 0x00, 0x00)

	// Build cookie records
	cookieOffsets := make([]uint32, len(cookies))
	for i, c := range cookies {
		cookieOffsets[i] = uint32(len(pageData))

		// Cookie record start
		cookieStart := len(pageData)

		// Size placeholder (4 bytes) - will fill in after
		sizeOffset := len(pageData)
		pageData = append(pageData, 0, 0, 0, 0)

		// Flags (4 bytes, little-endian)
		binary.LittleEndian.PutUint32(buf, c.flags)
		pageData = append(pageData, buf...)

		// URL/Domain offset (4 bytes) - relative to cookie start
		urlOffset := uint32(48) // Fixed offset after header
		binary.LittleEndian.PutUint32(buf, urlOffset)
		pageData = append(pageData, buf...)

		// Name offset (4 bytes)
		nameOffset := urlOffset + uint32(len(c.domain)+1)
		binary.LittleEndian.PutUint32(buf, nameOffset)
		pageData = append(pageData, buf...)

		// Path offset (4 bytes)
		pathOffset := nameOffset + uint32(len(c.name)+1)
		binary.LittleEndian.PutUint32(buf, pathOffset)
		pageData = append(pageData, buf...)

		// Value offset (4 bytes)
		valueOffset := pathOffset + uint32(len(c.path)+1)
		binary.LittleEndian.PutUint32(buf, valueOffset)
		pageData = append(pageData, buf...)

		// End of header marker (8 bytes)
		pageData = append(pageData, 0, 0, 0, 0, 0, 0, 0, 0)

		// Expiry time (8 bytes, little-endian float64)
		expiryBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(expiryBuf, math.Float64bits(c.expiry))
		pageData = append(pageData, expiryBuf...)

		// Creation time (8 bytes, little-endian float64)
		creationBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(creationBuf, math.Float64bits(c.creation))
		pageData = append(pageData, creationBuf...)

		// Domain (null-terminated)
		pageData = append(pageData, []byte(c.domain)...)
		pageData = append(pageData, 0)

		// Name (null-terminated)
		pageData = append(pageData, []byte(c.name)...)
		pageData = append(pageData, 0)

		// Path (null-terminated)
		pageData = append(pageData, []byte(c.path)...)
		pageData = append(pageData, 0)

		// Value (null-terminated)
		pageData = append(pageData, []byte(c.value)...)
		pageData = append(pageData, 0)

		// Update size
		cookieSize := uint32(len(pageData) - cookieStart)
		binary.LittleEndian.PutUint32(pageData[sizeOffset:], cookieSize)
	}

	// Update cookie offsets
	for i, offset := range cookieOffsets {
		binary.LittleEndian.PutUint32(pageData[cookieOffsetStart+i*4:], offset)
	}

	// Write number of pages (1, big-endian)
	numPages := uint32(1)
	binary.BigEndian.PutUint32(buf, numPages)
	if _, err := f.Write(buf); err != nil {
		return err
	}

	// Write page size (big-endian)
	pageSize := uint32(len(pageData))
	binary.BigEndian.PutUint32(buf, pageSize)
	if _, err := f.Write(buf); err != nil {
		return err
	}

	// Write page data
	if _, err := f.Write(pageData); err != nil {
		return err
	}

	// Write checksum (simplified - just zeros)
	checksum := make([]byte, 8)
	if _, err := f.Write(checksum); err != nil {
		return err
	}

	return nil
}

func generateEmptyCookiesBinary(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Magic header "cook"
	if _, err := f.Write([]byte("cook")); err != nil {
		return err
	}

	// Number of pages (0)
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, 0)
	if _, err := f.Write(buf); err != nil {
		return err
	}

	// Checksum
	checksum := make([]byte, 8)
	if _, err := f.Write(checksum); err != nil {
		return err
	}

	return nil
}

func generateExtensionsPlist(path string) error {
	extensions := map[string]interface{}{
		"Installed Extensions": []map[string]interface{}{
			{
				"Archive File Name":   "1Password.safariextz",
				"Bundle Directory Name": "com.agilebits.onepassword-safari",
				"Enabled":             true,
			},
			{
				"Archive File Name":   "AdBlock.safariextz",
				"Bundle Directory Name": "com.betafish.adblockforsafari",
				"Enabled":             true,
			},
			{
				"Archive File Name":   "Example.safariextz",
				"Bundle Directory Name": "com.example.disabled-ext",
				"Enabled":             false,
			},
		},
	}

	return writePlist(path, extensions)
}

func generateLastSessionPlist(path string) error {
	session := map[string]interface{}{
		"SessionVersion": 1,
		"SessionWindows": []map[string]interface{}{
			{
				"WindowUUID":       "WIN-UUID-0001-0000-000000000000",
				"IsPrivate":        false,
				"SelectedTabIndex": 1,
				"TabStates": []map[string]interface{}{
					{
						"TabUUID":           "TAB-UUID-0001-0001-000000000000",
						"TabURL":            "https://github.com",
						"TabTitle":          "GitHub",
						"LastVisitTime":     time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
						"isAppInitiated":    true,
						"SessionStateData":  nil,
					},
					{
						"TabUUID":           "TAB-UUID-0001-0002-000000000000",
						"TabURL":            "https://news.ycombinator.com",
						"TabTitle":          "Hacker News",
						"LastVisitTime":     time.Date(2024, 1, 15, 10, 35, 0, 0, time.UTC),
						"isAppInitiated":    false,
						"SessionStateData":  nil,
					},
					{
						"TabUUID":           "TAB-UUID-0001-0003-000000000000",
						"TabURL":            "https://stackoverflow.com/questions",
						"TabTitle":          "Stack Overflow",
						"LastVisitTime":     time.Date(2024, 1, 15, 10, 40, 0, 0, time.UTC),
						"isAppInitiated":    false,
						"SessionStateData":  nil,
					},
				},
			},
			{
				"WindowUUID":       "WIN-UUID-0002-0000-000000000000",
				"IsPrivate":        false,
				"SelectedTabIndex": 0,
				"TabStates": []map[string]interface{}{
					{
						"TabUUID":        "TAB-UUID-0002-0001-000000000000",
						"TabURL":         "https://pkg.go.dev",
						"TabTitle":       "Go Packages",
						"LastVisitTime":  time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC),
						"isAppInitiated": true,
					},
				},
			},
		},
	}

	return writePlist(path, session)
}

func generateLocalStorage(dir string) error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Generate sample local storage databases
	origins := []struct {
		filename string
		data     map[string]string
	}{
		{
			"https_github.com_0.localstorage",
			map[string]string{
				"user.settings":   `{"theme":"dark","notifications":true}`,
				"user.lastVisit":  "2024-01-15T10:30:00Z",
				"sidebar.collapsed": "false",
			},
		},
		{
			"https_example.com_0.localstorage",
			map[string]string{
				"preferences": `{"language":"en","timezone":"UTC"}`,
				"authToken":   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.example",
			},
		},
	}

	for _, origin := range origins {
		dbPath := filepath.Join(dir, origin.filename)
		if err := generateLocalStorageDB(dbPath, origin.data); err != nil {
			return fmt.Errorf("generating %s: %w", origin.filename, err)
		}
	}

	return nil
}

func generateLocalStorageDB(path string, data map[string]string) error {
	os.Remove(path)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	// Local storage schema
	schema := `
		CREATE TABLE ItemTable (
			key TEXT UNIQUE ON CONFLICT REPLACE,
			value BLOB NOT NULL ON CONFLICT FAIL
		);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	for k, v := range data {
		if _, err := db.Exec(`INSERT INTO ItemTable (key, value) VALUES (?, ?)`, k, v); err != nil {
			return fmt.Errorf("inserting %s: %w", k, err)
		}
	}

	return nil
}

func generateTopSitesPlist(path string) error {
	topSites := map[string]interface{}{
		"TopSites": []map[string]interface{}{
			{
				"TopSiteTitle": "GitHub",
				"TopSiteURLString": "https://github.com",
				"TopSiteIsBuiltIn": false,
			},
			{
				"TopSiteTitle": "Google",
				"TopSiteURLString": "https://www.google.com",
				"TopSiteIsBuiltIn": true,
			},
			{
				"TopSiteTitle": "Stack Overflow",
				"TopSiteURLString": "https://stackoverflow.com",
				"TopSiteIsBuiltIn": false,
			},
			{
				"TopSiteTitle": "Hacker News",
				"TopSiteURLString": "https://news.ycombinator.com",
				"TopSiteIsBuiltIn": false,
			},
		},
		"DisplayedSitesLastModified": time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}

	return writePlist(path, topSites)
}

func writePlist(path string, data interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := plist.NewBinaryEncoder(f)
	return encoder.Encode(data)
}
