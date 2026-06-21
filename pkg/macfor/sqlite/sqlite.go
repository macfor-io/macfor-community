// Package sqlite provides helper functions for forensically sound SQLite database access.
// It handles read-only database opening, database copying with WAL support, and schema
// introspection utilities.
//
// This package is designed for forensic collection where the original database files
// must never be modified. All operations that could modify the database (like WAL
// checkpoint) are performed on temporary copies.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/macfor-io/macfor-community/pkg/macfor"
	_ "modernc.org/sqlite" // SQLite driver registration
)

// Common errors for SQLite operations.
var (
	// ErrDatabaseLocked indicates the database is locked by another process.
	ErrDatabaseLocked = errors.New("database locked")

	// ErrDatabaseCorrupt indicates the database appears to be corrupt.
	ErrDatabaseCorrupt = errors.New("database corrupt")

	// ErrInvalidMagic indicates the file is not a valid SQLite database.
	ErrInvalidMagic = errors.New("invalid SQLite magic header")
)

// OpenReadOnly opens a SQLite database in read-only mode.
// The returned database connection will not modify the database file.
//
// For databases that may be actively written by other processes (e.g., Safari's
// History.db), use CopyForReading instead to avoid interference and ensure
// WAL data is properly included.
func OpenReadOnly(path string) (*sql.DB, error) {
	// Use modernc.org/sqlite driver with read-only mode and immutable flag
	// The immutable flag ensures no modifications can occur
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Verify the database is accessible
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		// Check for common error conditions
		if isLockError(err) {
			return nil, fmt.Errorf("%w: %s", ErrDatabaseLocked, path)
		}
		if isCorruptError(err) {
			return nil, fmt.Errorf("%w: %s: %w", ErrDatabaseCorrupt, path, err)
		}
		return nil, fmt.Errorf("verifying database connection: %w", err)
	}

	return db, nil
}

// CopyForReading creates a forensically sound copy of a SQLite database including
// its WAL (Write-Ahead Log) and SHM (Shared Memory) files if present.
//
// This function:
//  1. Creates a temporary directory for the copy
//  2. Copies the main database file
//  3. Copies the WAL file if present (contains uncommitted transactions)
//  4. Copies the SHM file if present (shared memory for WAL)
//  5. Opens the copy and performs a WAL checkpoint to merge any WAL data
//  6. Returns the path to the copy and a cleanup function
//
// The cleanup function must be called when done to remove temporary files.
// The returned database path can be safely opened with OpenReadOnly.
//
// Example:
//
//	tempPath, cleanup, err := CopyForReading(src, "/Users/admin/Library/Safari/History.db")
//	if err != nil {
//	    return err
//	}
//	defer cleanup()
//
//	db, err := OpenReadOnly(tempPath)
func CopyForReading(src macfor.Source, dbPath string) (string, func(), error) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "macfor-sqlite-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp directory: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	// Copy main database file
	dbDest := filepath.Join(tempDir, filepath.Base(dbPath))
	if err := copyFile(src, dbPath, dbDest); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copying database: %w", err)
	}

	// Copy WAL file if it exists
	walPath := dbPath + "-wal"
	walDest := dbDest + "-wal"
	walCopied, err := copyFileIfExists(src, walPath, walDest)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copying WAL file: %w", err)
	}

	// Copy SHM file if it exists
	shmPath := dbPath + "-shm"
	shmDest := dbDest + "-shm"
	if _, err := copyFileIfExists(src, shmPath, shmDest); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copying SHM file: %w", err)
	}

	// If WAL file was copied, checkpoint it to merge data into main database
	if walCopied {
		if err := checkpointWAL(dbDest); err != nil {
			// Log warning but continue - data may still be accessible
			// The error is not fatal as we still have the raw WAL file
			_ = err // Intentionally ignored, partial data is better than no data
		}
	}

	return dbDest, cleanup, nil
}

// TableExists checks if a table exists in the database.
func TableExists(db *sql.DB, table string) bool {
	var name string
	err := db.QueryRowContext(
		context.Background(),
		"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
		table,
	).Scan(&name)
	return err == nil
}

// GetTableColumns returns the column names for a table.
// Returns an error if the table does not exist.
func GetTableColumns(db *sql.DB, table string) ([]string, error) {
	// Use PRAGMA table_info to get column information
	rows, err := db.QueryContext(context.Background(), fmt.Sprintf("PRAGMA table_info(%q)", table))
	if err != nil {
		return nil, fmt.Errorf("querying table info: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dfltValue sql.NullString

		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scanning column info: %w", err)
		}
		columns = append(columns, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating columns: %w", err)
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("table %q not found", table)
	}

	return columns, nil
}

// GetTableRowCount returns the number of rows in a table.
func GetTableRowCount(db *sql.DB, table string) (int64, error) {
	var count int64
	err := db.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %q", table)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("counting rows: %w", err)
	}
	return count, nil
}

// GetSchemaVersion returns the schema version of the database.
// This is useful for detecting database format changes between versions.
func GetSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(context.Background(), "PRAGMA schema_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("getting schema version: %w", err)
	}
	return version, nil
}

// GetUserVersion returns the user version of the database.
// Applications often use this to track their own versioning.
func GetUserVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("getting user version: %w", err)
	}
	return version, nil
}

// IsSQLiteFile checks if a file appears to be a valid SQLite database
// by checking for the SQLite magic header.
func IsSQLiteFile(src macfor.Source, path string) (bool, error) {
	r, err := src.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = r.Close() }()

	// SQLite databases start with "SQLite format 3\x00"
	magic := make([]byte, 16)
	n, err := r.Read(magic)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	if n < 16 {
		return false, nil
	}

	return string(magic) == "SQLite format 3\x00", nil
}

// copyFile copies a file from the source to a local destination path.
func copyFile(src macfor.Source, srcPath, destPath string) error {
	r, err := src.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	w, err := os.Create(destPath) //nolint:gosec // Path is from CopyForReading temp dir
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()

	if _, err := io.Copy(w, r); err != nil {
		return err
	}

	return w.Close()
}

// copyFileIfExists copies a file if it exists, returning true if the file was copied.
func copyFileIfExists(src macfor.Source, srcPath, destPath string) (bool, error) {
	_, err := src.Stat(srcPath)
	if err != nil {
		// File doesn't exist, that's fine - check if it's a "not found" error
		if errors.Is(err, macfor.ErrNotFound) || os.IsNotExist(err) {
			return false, nil
		}
		// For other errors (e.g., permission denied), return the error
		return false, err
	}

	if err := copyFile(src, srcPath, destPath); err != nil {
		return false, err
	}

	return true, nil
}

// checkpointWAL performs a WAL checkpoint on the database copy to merge
// WAL contents into the main database file.
func checkpointWAL(dbPath string) error {
	// Open database without immutable flag to allow checkpoint
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("opening for checkpoint: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Perform TRUNCATE checkpoint - merges WAL and truncates the file
	_, err = db.ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return fmt.Errorf("checkpoint failed: %w", err)
	}

	return nil
}

// isLockError checks if an error indicates a database lock.
func isLockError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "locked") || strings.Contains(errStr, "SQLITE_BUSY")
}

// isCorruptError checks if an error indicates database corruption.
func isCorruptError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "corrupt") || strings.Contains(errStr, "malformed") ||
		strings.Contains(errStr, "SQLITE_CORRUPT") || strings.Contains(errStr, "SQLITE_NOTADB")
}
