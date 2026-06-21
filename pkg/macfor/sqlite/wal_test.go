package sqlite

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestWALExists(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "macfor-wal-exist-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "test.db")
	src := newTestSource()

	// Create a dummy database file
	if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0600); err != nil {
		t.Fatalf("creating dummy db: %v", err)
	}

	// WAL should not exist initially
	if WALExists(src, dbPath) {
		t.Error("expected WAL file to not exist initially")
	}

	// Create a WAL file
	createWALFile(t, dbPath)

	// WAL should now exist
	if !WALExists(src, dbPath) {
		t.Error("expected WAL file to exist after creation")
	}
}

func TestSHMExists(t *testing.T) {
	src := newTestSource()

	// For a non-existent database, SHM should not exist
	if SHMExists(src, "/nonexistent/db.sqlite") {
		t.Error("expected SHM file to not exist for nonexistent database")
	}
}

func TestGetWALInfo(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "macfor-wal-info-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "test.db")
	src := newTestSource()

	// Create a dummy database file
	if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0600); err != nil {
		t.Fatalf("creating dummy db: %v", err)
	}

	// Create a WAL file
	createWALFile(t, dbPath)

	t.Run("existing WAL", func(t *testing.T) {
		info, err := GetWALInfo(src, dbPath)
		if err != nil {
			t.Fatalf("GetWALInfo failed: %v", err)
		}

		if !info.Exists {
			t.Error("expected WAL to exist")
		}

		if info.Size <= 0 {
			t.Error("expected positive WAL size")
		}

		// Verify header was parsed
		if !IsValidWALMagic(info.Header.Magic) {
			t.Errorf("invalid WAL magic: 0x%08x", info.Header.Magic)
		}

		if info.Header.PageSize <= 0 {
			t.Error("expected positive page size")
		}
	})

	t.Run("nonexistent WAL", func(t *testing.T) {
		info, err := GetWALInfo(src, "/nonexistent/db.sqlite")
		if err != nil {
			t.Fatalf("GetWALInfo failed: %v", err)
		}

		if info.Exists {
			t.Error("expected WAL to not exist")
		}
	})
}

func TestGetDatabaseFilesInfo(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "macfor-dbinfo-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "test.db")
	src := newTestSource()

	// Create a dummy database file
	dbContent := make([]byte, 4096)
	copy(dbContent, "SQLite format 3\x00")
	if err := os.WriteFile(dbPath, dbContent, 0600); err != nil {
		t.Fatalf("creating dummy db: %v", err)
	}

	// Create a WAL file
	createWALFile(t, dbPath)

	info, err := GetDatabaseFilesInfo(src, dbPath)
	if err != nil {
		t.Fatalf("GetDatabaseFilesInfo failed: %v", err)
	}

	if info.DatabasePath != dbPath {
		t.Errorf("DatabasePath = %q, want %q", info.DatabasePath, dbPath)
	}

	if info.DatabaseSize <= 0 {
		t.Error("expected positive database size")
	}

	// WAL should exist now that we've created it
	if !info.HasWAL {
		t.Error("expected HasWAL to be true")
	}

	if info.WALSize <= 0 {
		t.Error("expected positive WAL size")
	}

	// Total size should be at least database size
	if info.TotalSize < info.DatabaseSize {
		t.Error("TotalSize should be >= DatabaseSize")
	}
}

func TestIsValidWALMagic(t *testing.T) {
	tests := []struct {
		magic uint32
		valid bool
	}{
		{WALMagicBE, true},
		{WALMagicLE, true},
		{0x12345678, false},
		{0, false},
	}

	for _, tt := range tests {
		got := IsValidWALMagic(tt.magic)
		if got != tt.valid {
			t.Errorf("IsValidWALMagic(0x%08x) = %v, want %v", tt.magic, got, tt.valid)
		}
	}
}

func TestReadWALHeader(t *testing.T) {
	testCases := []struct {
		name      string
		magic     uint32
		byteOrder binary.ByteOrder
		salt1     uint32
	}{
		{
			name:      "big-endian header",
			magic:     WALMagicBE,
			byteOrder: binary.BigEndian,
			salt1:     12345,
		},
		{
			name:      "little-endian header",
			magic:     WALMagicLE,
			byteOrder: binary.LittleEndian,
			salt1:     11111,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			_ = binary.Write(buf, binary.BigEndian, tc.magic)    // Magic always big-endian
			_ = binary.Write(buf, tc.byteOrder, uint32(3007000)) // File format
			_ = binary.Write(buf, tc.byteOrder, uint32(4096))    // Page size
			_ = binary.Write(buf, tc.byteOrder, uint32(1))       // Checkpoint seq
			_ = binary.Write(buf, tc.byteOrder, tc.salt1)        // Salt1
			_ = binary.Write(buf, tc.byteOrder, uint32(67890))   // Salt2
			_ = binary.Write(buf, tc.byteOrder, uint32(0))       // Checksum1
			_ = binary.Write(buf, tc.byteOrder, uint32(0))       // Checksum2

			header, err := readWALHeader(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatalf("readWALHeader failed: %v", err)
			}

			if header.Magic != tc.magic {
				t.Errorf("Magic = 0x%08x, want 0x%08x", header.Magic, tc.magic)
			}

			if header.PageSize != 4096 {
				t.Errorf("PageSize = %d, want 4096", header.PageSize)
			}

			if header.Salt1 != tc.salt1 {
				t.Errorf("Salt1 = %d, want %d", header.Salt1, tc.salt1)
			}
		})
	}

	t.Run("invalid magic", func(t *testing.T) {
		buf := new(bytes.Buffer)
		_ = binary.Write(buf, binary.BigEndian, uint32(0x12345678))

		_, err := readWALHeader(bytes.NewReader(buf.Bytes()))
		if err == nil {
			t.Error("expected error for invalid magic")
		}
	})
}

func TestGetDatabaseFilesInfo_NoWAL(t *testing.T) {
	dbPath, cleanup := createTestDB(t)
	defer cleanup()

	src := newTestSource()

	info, err := GetDatabaseFilesInfo(src, dbPath)
	if err != nil {
		t.Fatalf("GetDatabaseFilesInfo failed: %v", err)
	}

	if info.HasWAL {
		t.Error("expected HasWAL to be false for non-WAL database")
	}

	if info.WALSize != 0 {
		t.Errorf("expected WALSize to be 0, got %d", info.WALSize)
	}

	// Total size should equal database size when no WAL
	if info.TotalSize != info.DatabaseSize {
		t.Errorf("TotalSize = %d, want %d (DatabaseSize)", info.TotalSize, info.DatabaseSize)
	}
}

func TestGetDatabaseFilesInfo_Nonexistent(t *testing.T) {
	src := newTestSource()

	_, err := GetDatabaseFilesInfo(src, "/nonexistent/db.sqlite")
	if err == nil {
		t.Error("expected error for nonexistent database")
	}
}

// createWALFile creates a minimal WAL file for testing.
func createWALFile(t *testing.T, dbPath string) {
	t.Helper()

	walPath := dbPath + "-wal"
	f, err := os.Create(walPath) //nolint:gosec // Test code
	if err != nil {
		t.Fatalf("creating WAL file: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Write header - magic is always big-endian
	_ = binary.Write(f, binary.BigEndian, WALMagicBE)
	_ = binary.Write(f, binary.BigEndian, uint32(3007000)) // File format
	_ = binary.Write(f, binary.BigEndian, uint32(4096))    // Page size
	_ = binary.Write(f, binary.BigEndian, uint32(1))       // Checkpoint seq
	_ = binary.Write(f, binary.BigEndian, uint32(12345))   // Salt1
	_ = binary.Write(f, binary.BigEndian, uint32(67890))   // Salt2
	_ = binary.Write(f, binary.BigEndian, uint32(0))       // Checksum1
	_ = binary.Write(f, binary.BigEndian, uint32(0))       // Checksum2
}

func TestGetWALInfo_WithCreatedWAL(t *testing.T) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "macfor-wal-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dbPath := filepath.Join(tempDir, "test.db")

	// Create a dummy database file
	if err := os.WriteFile(dbPath, []byte("SQLite format 3\x00"), 0600); err != nil {
		t.Fatalf("creating dummy db: %v", err)
	}

	// Create a WAL file
	createWALFile(t, dbPath)

	src := newTestSource()
	info, err := GetWALInfo(src, dbPath)
	if err != nil {
		t.Fatalf("GetWALInfo failed: %v", err)
	}

	if !info.Exists {
		t.Error("expected WAL to exist")
	}

	if info.Header.Magic != WALMagicBE {
		t.Errorf("Magic = 0x%08x, want 0x%08x", info.Header.Magic, WALMagicBE)
	}

	if info.Header.PageSize != 4096 {
		t.Errorf("PageSize = %d, want 4096", info.Header.PageSize)
	}
}
