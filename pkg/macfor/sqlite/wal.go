package sqlite

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/macfor-io/macfor-community/pkg/macfor"
)

// WAL file constants.
const (
	// WALMagicLE is the magic number for little-endian WAL files.
	WALMagicLE uint32 = 0x377f0682

	// WALMagicBE is the magic number for big-endian WAL files.
	WALMagicBE uint32 = 0x377f0683

	// WALHeaderSize is the size of the WAL header in bytes.
	WALHeaderSize = 32
)

// WALHeader represents the header of a SQLite WAL file.
type WALHeader struct {
	// Magic is the WAL file magic number (0x377f0682 or 0x377f0683).
	Magic uint32

	// FileFormat is the WAL file format version (3007000 for format 3.7.0).
	FileFormat uint32

	// PageSize is the database page size.
	PageSize uint32

	// CheckpointSeq is the checkpoint sequence number.
	CheckpointSeq uint32

	// Salt1 and Salt2 are random integers used for checksums.
	Salt1 uint32
	Salt2 uint32

	// Checksum1 and Checksum2 are checksum values for the header.
	Checksum1 uint32
	Checksum2 uint32
}

// WALInfo contains information about a WAL file.
type WALInfo struct {
	// Header is the parsed WAL header.
	Header WALHeader

	// Exists indicates whether the WAL file exists.
	Exists bool

	// Size is the size of the WAL file in bytes.
	Size int64

	// EstimatedFrames is an estimate of the number of frames in the WAL.
	// This is calculated as (Size - WALHeaderSize) / (PageSize + 24).
	EstimatedFrames int
}

// WALExists checks if a WAL file exists for the given database path.
func WALExists(src macfor.Source, dbPath string) bool {
	walPath := dbPath + "-wal"
	_, err := src.Stat(walPath)
	return err == nil
}

// SHMExists checks if a SHM (shared memory) file exists for the given database path.
func SHMExists(src macfor.Source, dbPath string) bool {
	shmPath := dbPath + "-shm"
	_, err := src.Stat(shmPath)
	return err == nil
}

// GetWALInfo retrieves information about a WAL file.
// Returns WALInfo with Exists=false if the WAL file does not exist.
func GetWALInfo(src macfor.Source, dbPath string) (WALInfo, error) {
	walPath := dbPath + "-wal"

	stat, err := src.Stat(walPath)
	if err != nil {
		// WAL doesn't exist
		return WALInfo{Exists: false}, nil
	}

	info := WALInfo{
		Exists: true,
		Size:   stat.Size(),
	}

	// Try to read and parse the header
	r, err := src.Open(walPath)
	if err != nil {
		return info, fmt.Errorf("opening WAL file: %w", err)
	}
	defer func() { _ = r.Close() }()

	header, err := readWALHeader(r)
	if err != nil {
		// Return partial info even if header can't be parsed
		return info, fmt.Errorf("reading WAL header: %w", err)
	}

	info.Header = header

	// Calculate estimated frames
	if header.PageSize > 0 {
		frameSize := int64(header.PageSize) + 24 // 24 bytes per frame header
		if info.Size > WALHeaderSize {
			info.EstimatedFrames = int((info.Size - WALHeaderSize) / frameSize)
		}
	}

	return info, nil
}

// readWALHeader reads and parses a WAL file header.
func readWALHeader(r io.Reader) (WALHeader, error) {
	var header WALHeader

	// Read magic number first to determine endianness
	var magic uint32
	if err := binary.Read(r, binary.BigEndian, &magic); err != nil {
		return header, fmt.Errorf("reading magic: %w", err)
	}

	var byteOrder binary.ByteOrder
	switch magic {
	case WALMagicBE:
		byteOrder = binary.BigEndian
	case WALMagicLE:
		byteOrder = binary.LittleEndian
	default:
		return header, fmt.Errorf("invalid WAL magic: 0x%08x", magic)
	}

	header.Magic = magic

	// Read remaining header fields
	if err := binary.Read(r, byteOrder, &header.FileFormat); err != nil {
		return header, fmt.Errorf("reading file format: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.PageSize); err != nil {
		return header, fmt.Errorf("reading page size: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.CheckpointSeq); err != nil {
		return header, fmt.Errorf("reading checkpoint seq: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.Salt1); err != nil {
		return header, fmt.Errorf("reading salt1: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.Salt2); err != nil {
		return header, fmt.Errorf("reading salt2: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.Checksum1); err != nil {
		return header, fmt.Errorf("reading checksum1: %w", err)
	}
	if err := binary.Read(r, byteOrder, &header.Checksum2); err != nil {
		return header, fmt.Errorf("reading checksum2: %w", err)
	}

	return header, nil
}

// IsValidWALMagic checks if a magic number is a valid WAL magic.
func IsValidWALMagic(magic uint32) bool {
	return magic == WALMagicBE || magic == WALMagicLE
}

// DatabaseFilesInfo contains information about a database and its auxiliary files.
type DatabaseFilesInfo struct {
	// DatabasePath is the path to the main database file.
	DatabasePath string

	// DatabaseSize is the size of the main database file.
	DatabaseSize int64

	// HasWAL indicates whether a WAL file exists.
	HasWAL bool

	// WALSize is the size of the WAL file (0 if not present).
	WALSize int64

	// HasSHM indicates whether a SHM file exists.
	HasSHM bool

	// SHMSize is the size of the SHM file (0 if not present).
	SHMSize int64

	// TotalSize is the combined size of all files.
	TotalSize int64
}

// GetDatabaseFilesInfo returns information about a database and its auxiliary files.
func GetDatabaseFilesInfo(src macfor.Source, dbPath string) (DatabaseFilesInfo, error) {
	info := DatabaseFilesInfo{
		DatabasePath: dbPath,
	}

	// Get main database info
	stat, err := src.Stat(dbPath)
	if err != nil {
		return info, fmt.Errorf("stat database: %w", err)
	}
	info.DatabaseSize = stat.Size()
	info.TotalSize = info.DatabaseSize

	// Check WAL
	walPath := dbPath + "-wal"
	if walStat, err := src.Stat(walPath); err == nil {
		info.HasWAL = true
		info.WALSize = walStat.Size()
		info.TotalSize += info.WALSize
	}

	// Check SHM
	shmPath := dbPath + "-shm"
	if shmStat, err := src.Stat(shmPath); err == nil {
		info.HasSHM = true
		info.SHMSize = shmStat.Size()
		info.TotalSize += info.SHMSize
	}

	return info, nil
}
