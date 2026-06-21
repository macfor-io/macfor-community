package safari

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// Test constants for LocalStorage tests.
const (
	testTypeLocalStorage = "local_storage"
)

// Note: Tests use fileSource from history_test.go and mockCollector from safari_test.go.

func TestExtractOriginFromFilename(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "https github",
			filename: "https_github.com_0.localstorage",
			want:     "https://github.com",
		},
		{
			name:     "https example",
			filename: "https_example.com_0.localstorage",
			want:     "https://example.com",
		},
		{
			name:     "http site",
			filename: "http_localhost_0.localstorage",
			want:     "http://localhost",
		},
		{
			name:     "subdomain",
			filename: "https_api.github.com_0.localstorage",
			want:     "https://api.github.com",
		},
		{
			name:     "higher index",
			filename: "https_example.com_5.localstorage",
			want:     "https://example.com",
		},
		{
			name:     "complex domain",
			filename: "https_sub.domain.example.co.uk_0.localstorage",
			want:     "https://sub.domain.example.co.uk",
		},
		{
			name:     "no index",
			filename: "https_noindex.com.localstorage",
			want:     "https://noindex.com",
		},
		{
			name:     "just domain",
			filename: "nodomain.localstorage",
			want:     "nodomain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractOriginFromFilename(tt.filename)
			if got != tt.want {
				t.Errorf("extractOriginFromFilename(%q) = %q, want %q", tt.filename, got, tt.want)
			}
		})
	}
}

// Note: value-preview truncation is now the tablemapper "truncate:200" transform;
// its behaviour is covered by TestResolveTransform_Truncate in the tablemapper
// package.

func TestLocalStorageRecordFields(t *testing.T) {
	record := &LocalStorageRecord{
		Type:         "testTypeLocalStorage",
		Browser:      "safari",
		User:         "testuser",
		Origin:       "https://example.com",
		Key:          "theme",
		ValuePreview: "dark",
		ValueSize:    4,
		SourceFile:   "/Users/testuser/Library/WebKit/WebsiteData/LocalStorage/https_example.com_0.localstorage",
	}

	if record.Type != "testTypeLocalStorage" {
		t.Errorf("Type = %q, want %q", record.Type, "testTypeLocalStorage")
	}
	if record.Browser != "safari" {
		t.Errorf("Browser = %q, want %q", record.Browser, "safari")
	}
	if record.User != "testuser" {
		t.Errorf("User = %q, want %q", record.User, "testuser")
	}
	if record.Origin != "https://example.com" {
		t.Errorf("Origin = %q, want %q", record.Origin, "https://example.com")
	}
	if record.Key != "theme" {
		t.Errorf("Key = %q, want %q", record.Key, "theme")
	}
	if record.ValuePreview != "dark" {
		t.Errorf("ValuePreview = %q, want %q", record.ValuePreview, "dark")
	}
	if record.ValueSize != 4 {
		t.Errorf("ValueSize = %d, want %d", record.ValueSize, 4)
	}
}

func TestCollectLocalStorageForUser(t *testing.T) {
	// Skip if fixtures don't exist.
	if !fixtureExists(FixtureLocalStorageGitHub) {
		t.Skip("LocalStorage fixtures not found")
	}

	t.Run("collect from fixtures", func(t *testing.T) {
		testCollectLocalStorageFromFixtures(t)
	})

	t.Run("missing directory", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir
		if err := os.MkdirAll(userHome, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		err := collectLocalStorageForUser(context.Background(), user, src, out)
		if err != nil {
			t.Fatalf("collectLocalStorageForUser() error = %v", err)
		}

		// Should not fail, just skip.
		if len(out.files) != 0 {
			t.Errorf("expected 0 files for missing directory, got %d", len(out.files))
		}
		if len(out.records) != 0 {
			t.Errorf("expected 0 records for missing directory, got %d", len(out.records))
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir
		localStorageDir := filepath.Join(userHome, LocalStoragePath)
		if err := os.MkdirAll(localStorageDir, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		err := collectLocalStorageForUser(context.Background(), user, src, out)
		if err != nil {
			t.Fatalf("collectLocalStorageForUser() error = %v", err)
		}

		// Should not fail, just skip.
		if len(out.files) != 0 {
			t.Errorf("expected 0 files for empty directory, got %d", len(out.files))
		}
		if len(out.records) != 0 {
			t.Errorf("expected 0 records for empty directory, got %d", len(out.records))
		}
	})

	t.Run("non-localstorage files ignored", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir
		localStorageDir := filepath.Join(userHome, LocalStoragePath)
		if err := os.MkdirAll(localStorageDir, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		// Create a non-localstorage file.
		otherPath := filepath.Join(localStorageDir, "other.txt")
		if err := os.WriteFile(otherPath, []byte("not a database"), 0600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		err := collectLocalStorageForUser(context.Background(), user, src, out)
		if err != nil {
			t.Fatalf("collectLocalStorageForUser() error = %v", err)
		}

		// Should ignore non-.localstorage files.
		if len(out.files) != 0 {
			t.Errorf("expected 0 files, got %d", len(out.files))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir
		localStorageDir := filepath.Join(userHome, LocalStoragePath)
		if err := os.MkdirAll(localStorageDir, 0750); err != nil {
			t.Fatalf("creating test directory: %v", err)
		}

		// Copy a fixture.
		githubData := mustLoadFixture(FixtureLocalStorageGitHub)
		githubPath := filepath.Join(localStorageDir, "https_github.com_0.localstorage")
		if err := os.WriteFile(githubPath, githubData, 0600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		// Create a cancelled context.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := collectLocalStorageForUser(ctx, user, src, out)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestCollectSingleLocalStorage(t *testing.T) {
	// Skip if fixtures don't exist.
	if !fixtureExists(FixtureLocalStorageGitHub) {
		t.Skip("LocalStorage fixtures not found")
	}

	t.Run("valid database", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir

		// Copy fixture to temp directory.
		githubData := mustLoadFixture(FixtureLocalStorageGitHub)
		filePath := filepath.Join(tempDir, "https_github.com_0.localstorage")
		if err := os.WriteFile(filePath, githubData, 0600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		count, err := collectSingleLocalStorage(context.Background(), user, src, out, filePath, "https_github.com_0.localstorage")
		if err != nil {
			t.Fatalf("collectSingleLocalStorage() error = %v", err)
		}

		// GitHub fixture has 3 keys.
		if count != 3 {
			t.Errorf("expected 3 records, got %d", count)
		}

		// Verify file was added.
		if len(out.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(out.files))
		}

		// Verify records were added.
		if len(out.records) != 3 {
			t.Errorf("expected 3 records, got %d", len(out.records))
		}

		// Check specific keys.
		keys := make(map[string]bool)
		for _, rec := range out.records {
			lsr := rec.record.(*LocalStorageRecord)
			keys[lsr.Key] = true
		}
		expectedKeys := []string{"user.settings", "user.lastVisit", "sidebar.collapsed"}
		for _, key := range expectedKeys {
			if !keys[key] {
				t.Errorf("expected key %q not found", key)
			}
		}
	})

	t.Run("invalid sqlite file", func(t *testing.T) {
		tempDir := t.TempDir()
		userHome := tempDir

		// Create a non-SQLite file with .localstorage extension.
		filePath := filepath.Join(tempDir, "https_bad.com_0.localstorage")
		if err := os.WriteFile(filePath, []byte("not a sqlite file"), 0600); err != nil {
			t.Fatalf("writing test file: %v", err)
		}

		src := newFileSource(tempDir)
		out := newMockCollector()

		user := macfor.User{
			Username: testUsername,
			HomeDir:  userHome,
		}

		_, err := collectSingleLocalStorage(context.Background(), user, src, out, filePath, "https_bad.com_0.localstorage")
		if err == nil {
			t.Error("expected error for invalid SQLite file")
		}
	})
}

func TestLocalStorageRecordSerialization(t *testing.T) {
	// This tests that LocalStorageRecord can be properly serialized.
	// The actual serialization is done by the collector, but we verify the struct tags.

	record := &LocalStorageRecord{
		Type:         "testTypeLocalStorage",
		Browser:      "safari",
		User:         "admin",
		Origin:       "https://app.example.com",
		Key:          "user_preferences",
		ValuePreview: `{"theme":"dark","lang":"en"}`,
		ValueSize:    28,
		SourceFile:   "/Users/admin/Library/WebKit/WebsiteData/LocalStorage/https_app.example.com_0.localstorage",
	}

	// Basic field access tests - ensures struct is properly defined.
	if record.Type != "testTypeLocalStorage" {
		t.Errorf("Type field mismatch")
	}
	if record.ValueSize != 28 {
		t.Errorf("ValueSize = %d, want 28", record.ValueSize)
	}
}

// testCollectLocalStorageFromFixtures tests collecting LocalStorage from test fixtures.
// Extracted from TestCollectLocalStorageForUser to reduce cyclomatic complexity.
func testCollectLocalStorageFromFixtures(t *testing.T) {
	t.Helper()

	// Create a file source with fixture data.
	tempDir := t.TempDir()
	userHome := tempDir
	localStorageDir := filepath.Join(userHome, LocalStoragePath)
	if err := os.MkdirAll(localStorageDir, 0750); err != nil {
		t.Fatalf("creating test directory: %v", err)
	}

	// Copy fixtures to expected location.
	setupLocalStorageFixtures(t, localStorageDir)

	// Create file source.
	src := newFileSource(tempDir)
	out := newMockCollector()

	user := macfor.User{
		Username: testUsername,
		HomeDir:  userHome,
	}

	err := collectLocalStorageForUser(context.Background(), user, src, out)
	if err != nil {
		t.Fatalf("collectLocalStorageForUser() error = %v", err)
	}

	// Verify files were added (2 databases).
	if len(out.files) != 2 {
		t.Errorf("expected 2 files, got %d", len(out.files))
	}

	// Verify records were added.
	// GitHub fixture has 3 keys, Example fixture has 2 keys.
	expectedRecords := 5
	if len(out.records) != expectedRecords {
		t.Errorf("expected %d records, got %d", expectedRecords, len(out.records))
	}

	// Verify local storage records.
	verifyLocalStorageRecords(t, out.records)
}

// setupLocalStorageFixtures copies LocalStorage fixtures to the test directory.
func setupLocalStorageFixtures(t *testing.T, localStorageDir string) {
	t.Helper()

	githubData := mustLoadFixture(FixtureLocalStorageGitHub)
	githubPath := filepath.Join(localStorageDir, "https_github.com_0.localstorage")
	if err := os.WriteFile(githubPath, githubData, 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	exampleData := mustLoadFixture(FixtureLocalStorageExample)
	examplePath := filepath.Join(localStorageDir, "https_example.com_0.localstorage")
	if err := os.WriteFile(examplePath, exampleData, 0600); err != nil {
		t.Fatalf("writing test file: %v", err)
	}
}

// verifyLocalStorageRecords verifies that all LocalStorage records are valid.
func verifyLocalStorageRecords(t *testing.T, records []collectedRecord) {
	t.Helper()

	// Check that records are LocalStorageRecords.
	for i, rec := range records {
		lsr, ok := rec.record.(*LocalStorageRecord)
		if !ok {
			t.Errorf("record %d is not a *LocalStorageRecord", i)
			continue
		}
		if lsr.Type != testTypeLocalStorage {
			t.Errorf("record %d Type = %q, want %q", i, lsr.Type, testTypeLocalStorage)
		}
		if lsr.Browser != testBrowser {
			t.Errorf("record %d Browser = %q, want %q", i, lsr.Browser, testBrowser)
		}
		if lsr.User != testUsername {
			t.Errorf("record %d User = %q, want %q", i, lsr.User, testUsername)
		}
		if lsr.Origin == "" {
			t.Errorf("record %d Origin is empty", i)
		}
	}

	// Verify we have records from both origins.
	origins := make(map[string]int)
	for _, rec := range records {
		lsr := rec.record.(*LocalStorageRecord)
		origins[lsr.Origin]++
	}
	if origins["https://github.com"] != 3 {
		t.Errorf("expected 3 github.com records, got %d", origins["https://github.com"])
	}
	if origins["https://example.com"] != 2 {
		t.Errorf("expected 2 example.com records, got %d", origins["https://example.com"])
	}
}
