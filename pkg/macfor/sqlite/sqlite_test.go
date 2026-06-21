package sqlite

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// createTestDB creates a test SQLite database with sample data.
// Returns the path to the database and a cleanup function.
func createTestDB(t *testing.T) (string, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "macfor-sqlite-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("creating test database: %v", err)
	}

	// Create test tables and data
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE
		);

		CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INTEGER REFERENCES users(id),
			title TEXT,
			content TEXT,
			created_at REAL
		);

		INSERT INTO users (name, email) VALUES
			('Alice', 'alice@example.com'),
			('Bob', 'bob@example.com'),
			('Charlie', 'charlie@example.com');

		INSERT INTO posts (user_id, title, content, created_at) VALUES
			(1, 'First Post', 'Hello World', 758750400.0),
			(1, 'Second Post', 'More content', 758836800.0),
			(2, 'Bob''s Post', 'Testing', 758923200.0);
	`)
	if err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
		t.Fatalf("creating test data: %v", err)
	}

	_ = db.Close()

	return dbPath, func() {
		_ = os.RemoveAll(tempDir)
	}
}

// createTestDBWithWAL creates a test database with WAL mode enabled.
func createTestDBWithWAL(t *testing.T) (string, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "macfor-sqlite-wal-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("creating test database: %v", err)
	}

	// Enable WAL mode
	_, err = db.ExecContext(context.Background(), "PRAGMA journal_mode=WAL")
	if err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
		t.Fatalf("enabling WAL mode: %v", err)
	}

	// Create test tables and data
	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE items (
			id INTEGER PRIMARY KEY,
			value TEXT
		);

		INSERT INTO items (value) VALUES ('item1'), ('item2'), ('item3');
	`)
	if err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
		t.Fatalf("creating test data: %v", err)
	}

	// Add more data without closing - should create WAL
	_, err = db.ExecContext(context.Background(), `INSERT INTO items (value) VALUES ('item4'), ('item5')`)
	if err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
		t.Fatalf("inserting data: %v", err)
	}

	_ = db.Close()

	return dbPath, func() {
		_ = os.RemoveAll(tempDir)
	}
}

func TestOpenReadOnly(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Verify we can read data
	var count int
	err = db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		t.Fatalf("querying database: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 users, got %d", count)
	}

	// Verify we can't write (should fail with immutable mode)
	_, err = db.ExecContext(context.Background(), "INSERT INTO users (name, email) VALUES ('Test', 'test@test.com')")
	if err == nil {
		t.Error("expected error writing to read-only database")
	}
}

func TestOpenReadOnly_NonExistent(t *testing.T) {
	_, err := OpenReadOnly("/nonexistent/path/db.sqlite")
	if err == nil {
		t.Error("expected error for non-existent database")
	}
}

func TestTableExists(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	tests := []struct {
		table  string
		exists bool
	}{
		{"users", true},
		{"posts", true},
		{"nonexistent", false},
		// Note: sqlite_master is a special system table and may not appear
		// in the sqlite_master query, so we don't test it here
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			got := TableExists(db, tt.table)
			if got != tt.exists {
				t.Errorf("TableExists(%q) = %v, want %v", tt.table, got, tt.exists)
			}
		})
	}
}

func TestGetTableColumns(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	t.Run("users table", func(t *testing.T) {
		columns, err := GetTableColumns(db, "users")
		if err != nil {
			t.Fatalf("GetTableColumns failed: %v", err)
		}

		expected := []string{"id", "name", "email"}
		if len(columns) != len(expected) {
			t.Fatalf("expected %d columns, got %d: %v", len(expected), len(columns), columns)
		}

		for i, col := range expected {
			if columns[i] != col {
				t.Errorf("column %d: expected %q, got %q", i, col, columns[i])
			}
		}
	})

	t.Run("posts table", func(t *testing.T) {
		columns, err := GetTableColumns(db, "posts")
		if err != nil {
			t.Fatalf("GetTableColumns failed: %v", err)
		}

		expected := []string{"id", "user_id", "title", "content", "created_at"}
		if len(columns) != len(expected) {
			t.Fatalf("expected %d columns, got %d: %v", len(expected), len(columns), columns)
		}
	})

	t.Run("nonexistent table", func(t *testing.T) {
		_, err := GetTableColumns(db, "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent table")
		}
	})
}

func TestGetTableRowCount(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	tests := []struct {
		table    string
		expected int64
	}{
		{"users", 3},
		{"posts", 3},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			count, err := GetTableRowCount(db, tt.table)
			if err != nil {
				t.Fatalf("GetTableRowCount failed: %v", err)
			}
			if count != tt.expected {
				t.Errorf("expected %d rows, got %d", tt.expected, count)
			}
		})
	}
}

func TestGetSchemaVersion(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	version, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}

	// Schema version should be positive after creating tables
	if version <= 0 {
		t.Errorf("expected positive schema version, got %d", version)
	}
}

func TestGetUserVersion(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	db, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly failed: %v", err)
	}
	defer func() { _ = db.Close() }()

	version, err := GetUserVersion(db)
	if err != nil {
		t.Fatalf("GetUserVersion failed: %v", err)
	}

	// Default user version is 0
	if version != 0 {
		t.Errorf("expected user version 0, got %d", version)
	}
}

func TestIsSQLiteFile(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	src := newTestSource()

	t.Run("valid sqlite file", func(t *testing.T) {
		valid, err := IsSQLiteFile(src, dbPath)
		if err != nil {
			t.Fatalf("IsSQLiteFile failed: %v", err)
		}
		if !valid {
			t.Error("expected database to be valid SQLite file")
		}
	})

	t.Run("non-sqlite file", func(t *testing.T) {
		// Create a non-SQLite file
		textFile := filepath.Join(filepath.Dir(dbPath), "test.txt")
		if err := os.WriteFile(textFile, []byte("not a database"), 0600); err != nil {
			t.Fatalf("creating test file: %v", err)
		}

		valid, err := IsSQLiteFile(src, textFile)
		if err != nil {
			t.Fatalf("IsSQLiteFile failed: %v", err)
		}
		if valid {
			t.Error("expected text file to not be valid SQLite file")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := IsSQLiteFile(src, "/nonexistent/file.db")
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestCopyForReading(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	src := newTestSource()

	tempPath, copyCleanup, err := CopyForReading(src, dbPath)
	if err != nil {
		t.Fatalf("CopyForReading failed: %v", err)
	}
	defer copyCleanup()

	// Verify the copy exists
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatalf("copy not found: %v", err)
	}

	// Verify we can open and read the copy
	db, err := OpenReadOnly(tempPath)
	if err != nil {
		t.Fatalf("opening copy: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		t.Fatalf("querying copy: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 users, got %d", count)
	}
}

func TestCopyForReading_WithWAL(t *testing.T) {
	dbPath, cleanup := createTestDBWithWAL(t)
	defer cleanup()

	src := newTestSource()

	tempPath, copyCleanup, err := CopyForReading(src, dbPath)
	if err != nil {
		t.Fatalf("CopyForReading failed: %v", err)
	}
	defer copyCleanup()

	// Open the copy and verify all data is accessible
	db, err := OpenReadOnly(tempPath)
	if err != nil {
		t.Fatalf("opening copy: %v", err)
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatalf("querying copy: %v", err)
	}
	// Should have all 5 items including those from WAL
	if count != 5 {
		t.Errorf("expected 5 items (including WAL data), got %d", count)
	}
}

// testSource is a simple Source implementation for testing that reads from the filesystem.
type testSource struct{}

func newTestSource() *testSource {
	return &testSource{}
}

func (s *testSource) Type() macfor.SourceType {
	return macfor.SourceTypeLive
}

func (s *testSource) OSVersion() (string, error) {
	return "14.0", nil
}

func (s *testSource) Hostname() (string, error) {
	return "test-host", nil
}

func (s *testSource) Open(path string) (io.ReadCloser, error) {
	return os.Open(path) //nolint:gosec // Test code
}

func (s *testSource) Stat(path string) (macfor.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &testFileInfo{info}, nil
}

func (s *testSource) ReadDir(path string) ([]macfor.DirEntry, error) {
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

func (s *testSource) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func (s *testSource) Walk(root string, fn macfor.WalkFunc) error {
	return filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		var fi macfor.FileInfo
		if info != nil {
			fi = &testFileInfo{info}
		}
		return fn(path, fi, err)
	})
}

func (s *testSource) Users() ([]macfor.User, error) {
	return nil, nil
}

func (s *testSource) ResolvePath(path string) string {
	return path
}

func (s *testSource) ReadPlist(_ string, _ any) error {
	return nil
}

func (s *testSource) SystemInfo() (*macfor.SystemInfo, error) {
	return &macfor.SystemInfo{
		OSName:       "macOS",
		OSVersion:    "14.0",
		Architecture: "arm64",
		Hostname:     "test-host",
		SourceType:   macfor.SourceTypeLive,
	}, nil
}

// testFileInfo wraps os.FileInfo to implement macfor.FileInfo.
type testFileInfo struct {
	fs.FileInfo
}

func (f *testFileInfo) AccessTime() time.Time {
	return time.Time{}
}

func (f *testFileInfo) ChangeTime() time.Time {
	return time.Time{}
}

func (f *testFileInfo) BirthTime() time.Time {
	return time.Time{}
}

// Verify testSource implements macfor.Source.
var _ macfor.Source = (*testSource)(nil)
